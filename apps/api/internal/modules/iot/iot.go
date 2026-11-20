// Package iot exposes the SensorThings-lite data model: Things(devices),
// Sensors, Datastreams, Observations (specs/04.3, specs/04.4).
package iot

import (
	"net/http"

	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
)

// Module exposes read-oriented IoT endpoints (writes happen via telemetry ingest).
type Module struct {
	pool *pgxpool.Pool
}

func NewModule(pool *pgxpool.Pool) *Module { return &Module{pool: pool} }

// Mount registers /sensors, /datastreams, /observations.
func (m *Module) Mount(r chi.Router) {
	r.Route("/sensors", func(rr chi.Router) {
		rr.Get("/", m.listSensors)
		rr.Get("/{id}", m.getSensor)
	})
	r.Route("/datastreams", func(rr chi.Router) {
		rr.Get("/", m.listDatastreams)
		rr.Get("/{id}", m.getDatastream)
		rr.Get("/{id}/observations", m.observationsForStream)
	})
	r.Route("/observations", func(rr chi.Router) {
		rr.Get("/", m.listObservations)
	})
}

func (m *Module) listSensors(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	var total int64
	if err := m.pool.QueryRow(r.Context(), `SELECT count(*) FROM sensors WHERE tenant_id=$1`, id.TenantID).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	rows, err := m.pool.Query(r.Context(), `
		SELECT s.id, s.tenant_id, s.device_id, d.name AS device_name, s.name, s.description, s.created_at
		FROM sensors s LEFT JOIN devices d ON d.id = s.device_id
		WHERE s.tenant_id=$1 ORDER BY s.created_at DESC LIMIT $2 OFFSET $3`,
		id.TenantID, pp.PageSize, pp.Offset())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	defer rows.Close()
	items, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, httpx.NewPage(items, total, pp), nil)
}

func (m *Module) getSensor(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	rows, err := m.pool.Query(r.Context(), `
		SELECT s.id, s.tenant_id, s.device_id, d.name AS device_name, s.name, s.description, s.created_at
		FROM sensors s LEFT JOIN devices d ON d.id = s.device_id
		WHERE s.tenant_id=$1 AND s.id=$2`, id.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	item, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	rows.Close()
	if err != nil {
		httpx.Respond(w, r, nil, apierr.NotFound("sensor"))
		return
	}
	httpx.Respond(w, r, item, nil)
}

func (m *Module) listDatastreams(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	args := []any{id.TenantID}
	conds := []string{"ds.tenant_id = $1"}
	if v := r.URL.Query().Get("device_id"); v != "" {
		args = append(args, v)
		conds = append(conds, "ds.device_id = $"+itoa(len(args)))
	}
	if v := r.URL.Query().Get("observed_property"); v != "" {
		args = append(args, v)
		conds = append(conds, "ds.observed_property = $"+itoa(len(args)))
	}
	var total int64
	if err := m.pool.QueryRow(r.Context(),
		"SELECT count(*) FROM datastreams ds WHERE "+joinAnd(conds), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := m.pool.Query(r.Context(), `
		SELECT ds.id, ds.tenant_id, ds.sensor_id, ds.device_id, d.name AS device_name,
			ds.name, ds.observed_property, ds.unit, ds.description, ds.created_at
		FROM datastreams ds LEFT JOIN devices d ON d.id = ds.device_id
		WHERE `+joinAnd(conds)+` ORDER BY ds.created_at DESC
		LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)), args...)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	defer rows.Close()
	items, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, httpx.NewPage(items, total, pp), nil)
}

func (m *Module) getDatastream(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	rows, err := m.pool.Query(r.Context(), `
		SELECT ds.id, ds.tenant_id, ds.sensor_id, ds.device_id, d.name AS device_name,
			ds.name, ds.observed_property, ds.unit, ds.description, ds.created_at
		FROM datastreams ds LEFT JOIN devices d ON d.id = ds.device_id
		WHERE ds.tenant_id=$1 AND ds.id=$2`, id.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	item, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	rows.Close()
	if err != nil {
		httpx.Respond(w, r, nil, apierr.NotFound("datastream"))
		return
	}
	httpx.Respond(w, r, item, nil)
}

// observationsForStream returns the time series for one datastream.
func (m *Module) observationsForStream(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	from, to := httpx.GetTime(r, "from"), httpx.GetTime(r, "to")
	args := []any{id.TenantID, chi.URLParam(r, "id")}
	conds := []string{"o.tenant_id = $1", "o.datastream_id = $2"}
	if !from.IsZero() {
		args = append(args, from)
		conds = append(conds, "o.phenomenon_time >= $"+itoa(len(args)))
	}
	if !to.IsZero() {
		args = append(args, to)
		conds = append(conds, "o.phenomenon_time <= $"+itoa(len(args)))
	}
	var total int64
	if err := m.pool.QueryRow(r.Context(),
		"SELECT count(*) FROM observations o WHERE "+joinAnd(conds), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := m.pool.Query(r.Context(), `
		SELECT o.id, o.datastream_id, o.device_id, o.result_number, o.result_string, o.result_json,
			o.phenomenon_time, o.result_time
		FROM observations o WHERE `+joinAnd(conds)+` ORDER BY o.phenomenon_time DESC
		LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)), args...)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	defer rows.Close()
	items, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, httpx.NewPage(items, total, pp), nil)
}

func (m *Module) listObservations(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	q := r.URL.Query()
	args := []any{id.TenantID}
	conds := []string{"o.tenant_id = $1"}
	if v := q.Get("datastream_id"); v != "" {
		args = append(args, v)
		conds = append(conds, "o.datastream_id = $"+itoa(len(args)))
	}
	if v := q.Get("device_id"); v != "" {
		args = append(args, v)
		conds = append(conds, "o.device_id = $"+itoa(len(args)))
	}
	if v := q.Get("observed_property"); v != "" {
		args = append(args, v)
		conds = append(conds, "ds.observed_property = $"+itoa(len(args)))
	}
	if !httpx.GetTime(r, "from").IsZero() {
		args = append(args, httpx.GetTime(r, "from"))
		conds = append(conds, "o.phenomenon_time >= $"+itoa(len(args)))
	}
	if !httpx.GetTime(r, "to").IsZero() {
		args = append(args, httpx.GetTime(r, "to"))
		conds = append(conds, "o.phenomenon_time <= $"+itoa(len(args)))
	}
	join := "FROM observations o LEFT JOIN datastreams ds ON ds.id = o.datastream_id WHERE "
	var total int64
	if err := m.pool.QueryRow(r.Context(),
		"SELECT count(*) "+join+joinAnd(conds), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := m.pool.Query(r.Context(), `
		SELECT o.id, o.datastream_id, ds.observed_property, ds.unit, o.device_id,
			o.result_number, o.result_string, o.result_json, o.phenomenon_time, o.result_time
		`+join+joinAnd(conds)+` ORDER BY o.phenomenon_time DESC
		LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)), args...)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	defer rows.Close()
	items, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, httpx.NewPage(items, total, pp), nil)
}

func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}

func itoa(n int) string { return strconv.Itoa(n) }
