package identity

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/passhash"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

// UsersAPI implements user management on top of the identity module.
type UsersAPI struct {
	pool *pgxpool.Pool
	mod  *Module
}

func NewUsersAPI(pool *pgxpool.Pool) *UsersAPI {
	return &UsersAPI{pool: pool, mod: NewModule(pool, nil)}
}

type userWrite struct {
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	DisplayName string   `json:"display_name"`
	Email       string   `json:"email"`
	Phone       string   `json:"phone"`
	Locale      string   `json:"locale"`
	Status      string   `json:"status"`
	Roles       []string `json:"roles"`
}

func (api *UsersAPI) listUsers(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	search := "%" + strings.ToLower(r.URL.Query().Get("search")) + "%"
	var total int64
	if err := api.pool.QueryRow(r.Context(),
		`SELECT count(*) FROM users WHERE tenant_id=$1 AND (lower(username) LIKE $2 OR lower(display_name) LIKE $2)`,
		id.TenantID, search).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	rows, err := api.pool.Query(r.Context(), userSelect+`
		WHERE u.tenant_id=$1 AND (lower(u.username) LIKE $2 OR lower(u.display_name) LIKE $2)
		GROUP BY u.id ORDER BY u.created_at DESC LIMIT $3 OFFSET $4`,
		id.TenantID, search, pp.PageSize, pp.Offset())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	defer rows.Close()
	users, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (userDTO, error) {
		var u userDTO
		var roles []byte
		err := row.Scan(&u.ID, &u.TenantID, &u.Username, &u.DisplayName, &u.Email, &u.Phone,
			&u.Locale, &u.Status, &u.LastLoginAt, &u.CreatedAt, &u.IsPlatformAdmin, &roles)
		u.Roles = parseJSONStrings(roles)
		return u, err
	})
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, httpx.NewPage(users, total, pp), nil)
}

func (api *UsersAPI) createUser(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	var req userWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.Username == "" || len(req.Password) < 6 {
		httpx.Respond(w, r, nil, apierr.Invalid("username required, password must be >= 6 chars"))
		return
	}
	hash, err := passhash.Hash(req.Password)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	newID := ulidx.New()
	status := req.Status
	if status == "" {
		status = "active"
	}
	locale := req.Locale
	if locale == "" {
		locale = "zh-CN"
	}
	if _, err := api.pool.Exec(r.Context(), `
		INSERT INTO users (id, tenant_id, username, password_hash, display_name, email, phone, locale, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		newID, id.TenantID, req.Username, hash, req.DisplayName, req.Email, req.Phone, locale, status, id.UserID); err != nil {
		httpx.Respond(w, r, nil, mapUserErr(err))
		return
	}
	if req.Roles != nil {
		if err := api.setRoles(r.Context(), id.TenantID, newID, req.Roles); err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
	}
	u, err := api.mod.scanUser(r.Context(), newID, id.TenantID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Created(w, r, u)
}

func (api *UsersAPI) updateUser(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	target := chi.URLParam(r, "id")
	var req userWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	ctx := r.Context()
	sets, args := []string{}, []any{}
	addSet := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, col+" = $"+strconv.Itoa(len(args)))
	}
	if req.DisplayName != "" {
		addSet("display_name", req.DisplayName)
	}
	if req.Email != "" {
		addSet("email", req.Email)
	}
	if req.Phone != "" {
		addSet("phone", req.Phone)
	}
	if req.Locale != "" {
		addSet("locale", req.Locale)
	}
	if req.Status != "" {
		addSet("status", req.Status)
	}
	if req.Password != "" {
		if len(req.Password) < 6 {
			httpx.Respond(w, r, nil, apierr.Invalid("password must be >= 6 chars"))
			return
		}
		hash, err := passhash.Hash(req.Password)
		if err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
		addSet("password_hash", hash)
	}
	if len(sets) > 0 {
		args = append(args, id.UserID, id.TenantID, target)
		byIdx, tenantIdx, idIdx := len(args)-2, len(args)-1, len(args)
		if _, err := api.pool.Exec(ctx, `UPDATE users SET `+strings.Join(sets, ", ")+
			`, updated_by = $`+strconv.Itoa(byIdx)+` WHERE id = $`+strconv.Itoa(idIdx)+
			` AND tenant_id = $`+strconv.Itoa(tenantIdx), args...); err != nil {
			httpx.Respond(w, r, nil, mapUserErr(err))
			return
		}
	}
	if req.Roles != nil {
		if err := api.setRoles(ctx, id.TenantID, target, req.Roles); err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
	}
	u, err := api.mod.scanUser(ctx, target, id.TenantID)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("user"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, u, nil)
}

func (api *UsersAPI) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	target := chi.URLParam(r, "id")
	if target == id.UserID {
		httpx.Respond(w, r, nil, apierr.Invalid("cannot delete yourself"))
		return
	}
	ct, err := api.pool.Exec(r.Context(), `DELETE FROM users WHERE tenant_id=$1 AND id=$2`, id.TenantID, target)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.NotFound("user"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setRoles replaces the user's role links by role code. The target user MUST
// belong to the caller's tenant — the historical unscoped DELETE allowed a
// tenant admin to wipe another tenant's user_roles (fixed, ADR-006).
func (api *UsersAPI) setRoles(ctx context.Context, tenantID, userID string, roleCodes []string) error {
	var targetTenant string
	err := api.pool.QueryRow(ctx,
		`SELECT tenant_id FROM users WHERE id = $1`, userID).Scan(&targetTenant)
	if err == pgx.ErrNoRows {
		return apierr.NotFound("user")
	}
	if err != nil {
		return err
	}
	if targetTenant != tenantID {
		return apierr.PermissionDenied("user belongs to another tenant")
	}
	if _, err := api.pool.Exec(ctx,
		`DELETE FROM user_roles ur WHERE ur.user_id = $1
		 AND EXISTS (SELECT 1 FROM users u WHERE u.id = ur.user_id AND u.tenant_id = $2)`,
		userID, tenantID); err != nil {
		return err
	}
	for _, code := range roleCodes {
		var roleID string
		err := api.pool.QueryRow(ctx, `SELECT id FROM roles WHERE tenant_id=$1 AND code=$2`, tenantID, code).Scan(&roleID)
		if err == pgx.ErrNoRows {
			return apierr.Invalid("unknown role %q", code)
		}
		if err != nil {
			return err
		}
		if _, err := api.pool.Exec(ctx,
			`INSERT INTO user_roles (user_id, role_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, userID, roleID); err != nil {
			return err
		}
	}
	return nil
}

func mapUserErr(err error) error {
	if strings.Contains(err.Error(), "duplicate key") {
		return apierr.Conflict("username already exists")
	}
	return err
}
