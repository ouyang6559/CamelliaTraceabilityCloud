package identity

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

// RolesAPI implements role management with permission assignment.
type RolesAPI struct {
	pool *pgxpool.Pool
}

func NewRolesAPI(pool *pgxpool.Pool) *RolesAPI { return &RolesAPI{pool: pool} }

type roleDTO struct {
	ID          string   `json:"id"`
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Builtin     bool     `json:"builtin"`
	Permissions []string `json:"permissions"`
	UserCount   int64    `json:"user_count"`
}

const roleSelect = `SELECT r.id, r.code, r.name, r.description, r.builtin,
	COALESCE(json_agg(p.code) FILTER (WHERE p.code IS NOT NULL), '[]'),
	(SELECT count(*) FROM user_roles ur WHERE ur.role_id = r.id)
	FROM roles r
	LEFT JOIN role_permissions rp ON rp.role_id = r.id
	LEFT JOIN permissions p ON p.id = rp.permission_id`

func (api *RolesAPI) listRoles(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	rows, err := api.pool.Query(r.Context(), roleSelect+`
		WHERE r.tenant_id = $1 GROUP BY r.id ORDER BY r.builtin DESC, r.code`, id.TenantID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	defer rows.Close()
	roles, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (roleDTO, error) {
		var ro roleDTO
		var perms []byte
		err := row.Scan(&ro.ID, &ro.Code, &ro.Name, &ro.Description, &ro.Builtin, &perms, &ro.UserCount)
		ro.Permissions = parseJSONStrings(perms)
		return ro, err
	})
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, httpx.NewPage(roles, int64(len(roles)), httpx.PageParams{Page: 1, PageSize: len(roles)}), nil)
}

func (api *RolesAPI) listPermissions(w http.ResponseWriter, r *http.Request) {
	httpx.Respond(w, r, Permissions, nil)
}

type roleWrite struct {
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

func (api *RolesAPI) createRole(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	var req roleWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.Code == "" || req.Name == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("code and name are required"))
		return
	}
	newID := ulidx.New()
	if _, err := api.pool.Exec(r.Context(),
		`INSERT INTO roles (id, tenant_id, code, name, description, created_by) VALUES ($1,$2,$3,$4,$5,$6)`,
		newID, id.TenantID, req.Code, req.Name, req.Description, id.UserID); err != nil {
		httpx.Respond(w, r, nil, mapUserErr(err))
		return
	}
	if err := api.setPermissions(r.Context(), newID, req.Permissions); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Created(w, r, newID)
}

func (api *RolesAPI) updateRole(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	roleID := chi.URLParam(r, "id")
	var req roleWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	ctx := r.Context()
	var builtin bool
	err := api.pool.QueryRow(ctx, `SELECT builtin FROM roles WHERE tenant_id=$1 AND id=$2`,
		id.TenantID, roleID).Scan(&builtin)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("role"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if builtin {
		httpx.Respond(w, r, nil, apierr.Invalid("builtin roles cannot be modified"))
		return
	}
	if req.Name != "" || req.Description != "" {
		if _, err := api.pool.Exec(ctx, `UPDATE roles SET name = COALESCE(NULLIF($1,''), name),
			description = COALESCE(NULLIF($2,''), description) WHERE id=$3 AND tenant_id=$4`,
			req.Name, req.Description, roleID, id.TenantID); err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
	}
	if req.Permissions != nil {
		if err := api.setPermissions(ctx, roleID, req.Permissions); err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *RolesAPI) deleteRole(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	roleID := chi.URLParam(r, "id")
	ct, err := api.pool.Exec(r.Context(),
		`DELETE FROM roles WHERE tenant_id=$1 AND id=$2 AND builtin = false`, id.TenantID, roleID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.NotFound("role"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *RolesAPI) setPermissions(ctx context.Context, roleID string, codes []string) error {
	if _, err := api.pool.Exec(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, roleID); err != nil {
		return err
	}
	valid := map[string]bool{}
	for _, p := range Permissions {
		valid[p.Code] = true
	}
	for _, code := range codes {
		if code == "*:*" {
			// Admin shortcut: link every catalog permission.
			if _, err := api.pool.Exec(ctx, `
				INSERT INTO role_permissions (role_id, permission_id)
				SELECT $1, id FROM permissions ON CONFLICT DO NOTHING`, roleID); err != nil {
				return err
			}
			continue
		}
		if !valid[code] {
			return apierr.Invalid("unknown permission %q", code)
		}
		if _, err := api.pool.Exec(ctx, `
			INSERT INTO role_permissions (role_id, permission_id)
			SELECT $1, id FROM permissions WHERE code = $2 ON CONFLICT DO NOTHING`, roleID, code); err != nil {
			return err
		}
	}
	return nil
}

// TenantsAPI exposes the caller's own tenant (no cross-tenant listing in MVP).
type TenantsAPI struct {
	pool *pgxpool.Pool
}

func NewTenantsAPI(pool *pgxpool.Pool) *TenantsAPI { return &TenantsAPI{pool: pool} }

func (api *TenantsAPI) currentTenant(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	var t struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Code      string `json:"code"`
		Status    string `json:"status"`
		CreatedAt string `json:"created_at"`
	}
	err = api.pool.QueryRow(r.Context(),
		`SELECT id, name, code, status, created_at FROM tenants WHERE id = $1`, id.TenantID).
		Scan(&t.ID, &t.Name, &t.Code, &t.Status, &t.CreatedAt)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, t, nil)
}

func (api *TenantsAPI) updateTenant(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("name is required"))
		return
	}
	if _, err := api.pool.Exec(r.Context(),
		`UPDATE tenants SET name = $1, metadata = metadata || jsonb_build_object('description', $2) WHERE id = $3`,
		body.Name, body.Description, id.TenantID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
