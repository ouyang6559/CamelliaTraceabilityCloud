// Package crud implements the generic tenant-scoped REST store shared by most
// domain resources: filtering, search, sorting, pagination, GeoJSON geometry
// round-tripping and permission wiring, following specs/09 API conventions.
package crud

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

// Col describes one writable/selectable column.
type Col struct {
	Name     string
	JSON     string // json key; defaults to Name
	Geometry bool   // GeoJSON <-> PostGIS geometry
	Numeric  bool   // NUMERIC columns are cast to float8 for JSON friendliness
	Required bool
	Hidden   bool // excluded from SELECT results (secrets)
}

// Resource describes one tenant-scoped table exposed via /api/v1/<alias>.
type Resource struct {
	Table   string
	Alias   string // URL path; defaults to Table
	Perm    string // permission resource code; defaults to Table
	Columns []Col
	Search  []string
	Filters []string
	Order   string // default ORDER BY
}

func (res Resource) perm() string {
	if res.Perm != "" {
		return res.Perm
	}
	return res.Table
}

func (res Resource) col(name string) (Col, bool) {
	for _, c := range res.Columns {
		if c.Name == name {
			return c, true
		}
	}
	return Col{}, false
}

func (res Resource) jsonKey(c Col) string {
	if c.JSON != "" {
		return c.JSON
	}
	return c.Name
}

// Store is a generic pgx-backed store for a Resource.
type Store struct {
	pool *pgxpool.Pool
	res  Resource
}

func NewStore(pool *pgxpool.Pool, res Resource) *Store {
	if res.Order == "" {
		res.Order = "created_at DESC"
	}
	return &Store{pool: pool, res: res}
}

var reservedParams = map[string]bool{
	"page": true, "page_size": true, "search": true, "sort": true,
}

// Mount registers GET list, POST create, GET/PATCH/DELETE one under the
// resource URL path (Alias or table name); permission codes use Perm.
func (s *Store) Mount(r chi.Router, authorize func(resource, action string) func(http.Handler) http.Handler) {
	path := s.res.Alias
	if path == "" {
		path = s.res.Table
	}
	perm := s.res.perm()
	r.Route("/"+path, func(rr chi.Router) {
		rr.With(authorize(perm, "read")).Get("/", s.List)
		rr.With(authorize(perm, "create")).Post("/", s.Create)
		rr.With(authorize(perm, "read")).Get("/{id}", s.Get)
		rr.With(authorize(perm, "update")).Patch("/{id}", s.Update)
		rr.With(authorize(perm, "delete")).Delete("/{id}", s.Delete)
	})
}

// selectList builds the SELECT expressions; id and tenant_id are always included.
func (s *Store) selectList() string {
	var parts []string
	parts = append(parts, "id")
	for _, c := range s.res.Columns {
		if c.Hidden {
			continue
		}
		switch {
		case c.Geometry:
			parts = append(parts, fmt.Sprintf("ST_AsGeoJSON(%s)::jsonb AS %s", c.Name, c.Name))
		case c.Numeric:
			parts = append(parts, fmt.Sprintf("%s::float8 AS %s", c.Name, c.Name))
		default:
			parts = append(parts, c.Name)
		}
	}
	return strings.Join(parts, ", ")
}

func (s *Store) buildWhere(tenantID string, r *http.Request, args []any) (string, []any, error) {
	conds := []string{"tenant_id = $1"}
	args = append(args, tenantID)

	q := r.URL.Query()
	for _, f := range s.res.Filters {
		if strings.HasSuffix(f, "_after") || strings.HasSuffix(f, "_before") {
			col := strings.TrimSuffix(strings.TrimSuffix(f, "_after"), "_before")
			if _, ok := s.res.col(col); !ok {
				continue
			}
			val := q.Get(f)
			if val == "" {
				continue
			}
			t, ok := parseTime(val)
			if !ok {
				return "", nil, apierr.Invalid("invalid time filter %s=%s", f, val)
			}
			args = append(args, t)
			op := ">="
			if strings.HasSuffix(f, "_before") {
				op = "<="
			}
			conds = append(conds, fmt.Sprintf("%s %s $%d", col, op, len(args)))
			continue
		}
		val := q.Get(f)
		if val == "" {
			continue
		}
		if val == "null" {
			conds = append(conds, fmt.Sprintf("%s IS NULL", f))
			continue
		}
		if strings.Contains(val, ",") {
			ids := strings.Split(val, ",")
			args = append(args, ids)
			conds = append(conds, fmt.Sprintf("%s = ANY($%d)", f, len(args)))
			continue
		}
		args = append(args, val)
		conds = append(conds, fmt.Sprintf("%s = $%d", f, len(args)))
	}

	if search := q.Get("search"); search != "" && len(s.res.Search) > 0 {
		like := "%" + strings.ToLower(search) + "%"
		var parts []string
		for _, c := range s.res.Search {
			args = append(args, like)
			parts = append(parts, fmt.Sprintf("lower(%s) LIKE $%d", c, len(args)))
		}
		conds = append(conds, "("+strings.Join(parts, " OR ")+")")
	}
	return strings.Join(conds, " AND "), args, nil
}

func parseTime(v string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func (s *Store) orderClause(r *http.Request) (string, error) {
	sort := r.URL.Query().Get("sort")
	if sort == "" {
		return s.res.Order, nil
	}
	allowed := map[string]bool{"id": true, "created_at": true, "updated_at": true, "name": true, "event_time": true, "triggered_at": true, "status": true, "code": true}
	var parts []string
	for _, term := range strings.Split(sort, ",") {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		desc := strings.HasPrefix(term, "-")
		col := strings.TrimPrefix(term, "-")
		if !allowed[col] {
			if _, ok := s.res.col(col); ok {
				allowed[col] = true
			} else {
				return "", apierr.Invalid("cannot sort by %q", col)
			}
		}
		if desc {
			parts = append(parts, col+" DESC")
		} else {
			parts = append(parts, col+" ASC")
		}
	}
	if len(parts) == 0 {
		parts = append(parts, s.res.Order)
	}
	return strings.Join(parts, ", "), nil
}

// List handles GET /.
func (s *Store) List(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	where, args, err := s.buildWhere(id.TenantID, r, nil)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	order, err := s.orderClause(r)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	pp := httpx.GetPageParams(r)

	var total int64
	countSQL := "SELECT count(*) FROM " + s.res.Table + " WHERE " + where
	if err := s.pool.QueryRow(r.Context(), countSQL, args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}

	args = append(args, pp.PageSize, pp.Offset())
	limitIdx, offsetIdx := len(args)-1, len(args)
	sql := fmt.Sprintf("SELECT %s FROM %s WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d",
		s.selectList(), s.res.Table, where, order, limitIdx, offsetIdx)
	rows, err := s.pool.Query(r.Context(), sql, args...)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	items, err := pgx.CollectRows(rows, pgx.RowToMap)
	rows.Close()
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, httpx.NewPage(items, total, pp), nil)
}

// Get handles GET /{id}.
func (s *Store) Get(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	sql := "SELECT " + s.selectList() + " FROM " + s.res.Table + " WHERE tenant_id = $1 AND id = $2"
	rows, qerr := s.pool.Query(r.Context(), sql, id.TenantID, chi.URLParam(r, "id"))
	if qerr != nil {
		httpx.Respond(w, r, nil, qerr)
		return
	}
	row, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound(s.res.Table))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, row, nil)
}

// Create handles POST /.
func (s *Store) Create(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.Respond(w, r, nil, apierr.Invalid("invalid JSON body"))
		return
	}
	fields, args, geoms, err := s.prepareWrite(body, true)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	newID := ulidx.New()
	cols := append([]string{"id", "tenant_id"}, fields...)
	ph := []string{"$1", "$2"}
	for i := range fields {
		ph = append(ph, placeholder(i+3, geoms[i]))
	}
	args = append([]any{newID, id.TenantID}, args...)

	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING %s",
		s.res.Table, strings.Join(cols, ", "), strings.Join(ph, ", "), s.selectList())
	rows, qerr := s.pool.Query(r.Context(), sql, args...)
	if qerr != nil {
		httpx.Respond(w, r, nil, mapWriteErr(s.res.Table, qerr))
		return
	}
	row, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	if err != nil {
		httpx.Respond(w, r, nil, mapWriteErr(s.res.Table, err))
		return
	}
	httpx.Created(w, r, row)
}

// Update handles PATCH /{id}.
func (s *Store) Update(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.Respond(w, r, nil, apierr.Invalid("invalid JSON body"))
		return
	}
	fields, args, geoms, err := s.prepareWrite(body, false)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if len(fields) == 0 {
		httpx.Respond(w, r, nil, apierr.Invalid("no updatable fields provided"))
		return
	}
	sets := make([]string, 0, len(fields))
	for i, c := range fields {
		sets = append(sets, fmt.Sprintf("%s = %s", c, placeholder(i+1, geoms[i])))
	}
	targetID := chi.URLParam(r, "id")
	args = append(args, id.TenantID, targetID)
	tenantIdx, idIdx := len(args)-1, len(args)

	sql := fmt.Sprintf("UPDATE %s SET %s, updated_by = $%d WHERE tenant_id = $%d AND id = $%d RETURNING %s",
		s.res.Table, strings.Join(sets, ", "), len(args)+1, tenantIdx, idIdx, s.selectList())
	args = append(args, id.UserID)
	rows, qerr := s.pool.Query(r.Context(), sql, args...)
	if qerr != nil {
		httpx.Respond(w, r, nil, mapWriteErr(s.res.Table, qerr))
		return
	}
	row, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound(s.res.Table))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, mapWriteErr(s.res.Table, err))
		return
	}
	httpx.Respond(w, r, row, nil)
}

// Delete handles DELETE /{id}.
func (s *Store) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	ct, err := s.pool.Exec(r.Context(),
		"DELETE FROM "+s.res.Table+" WHERE tenant_id = $1 AND id = $2", id.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Respond(w, r, nil, mapWriteErr(s.res.Table, err))
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.NotFound(s.res.Table))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// prepareWrite validates the JSON body against column specs and produces
// (columnNames, bindValues, geometryFlags). Geometry values are bound as raw
// GeoJSON text and wrapped by placeholder() at statement build time.
func (s *Store) prepareWrite(body map[string]any, creating bool) ([]string, []any, []bool, error) {
	var cols []string
	var args []any
	var geoms []bool
	for _, c := range s.res.Columns {
		key := s.res.jsonKey(c)
		raw, provided := body[key]
		if creating && c.Required && (!provided || raw == nil || fmt.Sprintf("%v", raw) == "") {
			return nil, nil, nil, apierr.Invalid("field %q is required", key)
		}
		if !provided {
			continue
		}
		if raw == nil {
			cols = append(cols, c.Name)
			args = append(args, nil)
			geoms = append(geoms, false)
			continue
		}
		if c.Geometry {
			b, err := json.Marshal(raw)
			if err != nil {
				return nil, nil, nil, apierr.Invalid("invalid GeoJSON for %q", key)
			}
			cols = append(cols, c.Name)
			args = append(args, string(b))
			geoms = append(geoms, true)
			continue
		}
		cols = append(cols, c.Name)
		args = append(args, raw)
		geoms = append(geoms, false)
	}
	delete(body, "id")
	delete(body, "tenant_id")
	return cols, args, geoms, nil
}

// placeholder renders $n, wrapped for PostGIS when the column holds GeoJSON.
func placeholder(n int, geometry bool) string {
	if geometry {
		return fmt.Sprintf("ST_SetSRID(ST_GeomFromGeoJSON($%d), 4326)", n)
	}
	return fmt.Sprintf("$%d", n)
}

func mapWriteErr(table string, err error) error {
	msg := err.Error()
	if strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint") {
		return apierr.Conflict("duplicate %s (code already exists)", table)
	}
	if strings.Contains(msg, "violates foreign key") {
		return apierr.Invalid("referenced entity does not exist")
	}
	return err
}
