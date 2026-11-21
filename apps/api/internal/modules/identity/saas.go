// SaaS tenant lifecycle: self-registration, platform-managed provisioning and
// the platform administrator endpoints (docs/adr/adr-006).
package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/config"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/passhash"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

// SaaSAPI implements tenant registration and platform administration.
type SaaSAPI struct {
	pool   *pgxpool.Pool
	cfg    *config.Config
	log    *slog.Logger
	module *Module // reuses issueTokens for the auto-login response
}

func NewSaaSAPI(pool *pgxpool.Pool, cfg *config.Config, log *slog.Logger, mod *Module) *SaaSAPI {
	return &SaaSAPI{pool: pool, cfg: cfg, log: log, module: mod}
}

var tenantCodeRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,31}$`)

// ---------- provisioning core ----------

// ProvisionInput is one tenant onboarding request.
type ProvisionInput struct {
	TenantName    string
	TenantCode    string // optional; generated when empty
	AdminUsername string
	AdminPassword string
	AdminEmail    string
	DisplayName   string
}

// ProvisionTenant creates the tenant, its builtin roles and the admin user in
// one transaction (SaaS onboarding core, reused by self-registration and the
// platform console). Returns the new tenant id.
func (s *SaaSAPI) ProvisionTenant(ctx context.Context, in ProvisionInput) (string, error) {
	if strings.TrimSpace(in.TenantName) == "" {
		return "", apierr.Invalid("tenant_name is required")
	}
	if in.AdminUsername == "" || len(in.AdminPassword) < 6 {
		return "", apierr.Invalid("admin_username required, admin_password must be >= 6 chars")
	}
	code := strings.ToLower(strings.TrimSpace(in.TenantCode))
	if code == "" {
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		code = "t-" + hex.EncodeToString(b)
	}
	if !tenantCodeRe.MatchString(code) {
		return "", apierr.Invalid("tenant_code must match %s", tenantCodeRe.String())
	}
	switch code {
	case "platform":
		return "", apierr.Conflict("tenant code %q is reserved", code)
	}

	hash, err := passhash.Hash(in.AdminPassword)
	if err != nil {
		return "", err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	tenantID := ulidx.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenants (id, name, code, status, metadata, created_by)
		VALUES ($1,$2,$3,'active','{}','saas-register')`, tenantID, strings.TrimSpace(in.TenantName), code); err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return "", apierr.Conflict("tenant code %q already exists", code)
		}
		return "", err
	}
	if err := ensureBuiltinRolesTx(ctx, tx, tenantID); err != nil {
		return "", err
	}
	adminID := ulidx.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO users (id, tenant_id, username, password_hash, display_name, email, locale, status, created_by)
		VALUES ($1,$2,$3,$4,COALESCE(NULLIF($5,''),$3),COALESCE(NULLIF($6,''),''),'zh-CN','active','saas-register')`,
		adminID, tenantID, in.AdminUsername, hash, in.DisplayName, in.AdminEmail); err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return "", apierr.Conflict("username %q already exists in the new tenant", in.AdminUsername)
		}
		return "", err
	}
	var adminRoleID string
	if err := tx.QueryRow(ctx,
		`SELECT id FROM roles WHERE tenant_id=$1 AND code='tenant_admin'`, tenantID).Scan(&adminRoleID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO user_roles (user_id, role_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		adminID, adminRoleID); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	s.log.Info("tenant provisioned", "code", code, "tenant", tenantID)
	return tenantID, nil
}

// ensureBuiltinRolesTx creates tenant_admin/operator/viewer inside a transaction.
func ensureBuiltinRolesTx(ctx context.Context, tx pgx.Tx, tenantID string) error {
	type roleDef struct {
		code, name, desc string
		perms            []string
	}
	roles := []roleDef{
		{"tenant_admin", "租户管理员", "Full management of the tenant", AdminPerms},
		{"operator", "作业人员", "Field operations (tasks, devices, records)", OperatorPerms},
		{"viewer", "只读用户", "Read-only access", ViewerPerms},
	}
	for _, r := range roles {
		roleID := ulidx.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO roles (id, tenant_id, code, name, description, builtin, created_by)
			VALUES ($1,$2,$3,$4,$5,true,'saas-register')`,
			roleID, tenantID, r.code, r.name, r.desc); err != nil {
			return err
		}
		for _, code := range r.perms {
			if code == "*:*" {
				if _, err := tx.Exec(ctx, `
					INSERT INTO role_permissions (role_id, permission_id)
					SELECT $1, id FROM permissions ON CONFLICT DO NOTHING`, roleID); err != nil {
					return err
				}
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO role_permissions (role_id, permission_id)
				SELECT $1, id FROM permissions WHERE code=$2 ON CONFLICT DO NOTHING`, roleID, code); err != nil {
				return err
			}
		}
	}
	return nil
}

// EnsurePermissionCatalog upserts the global permission catalog at startup so
// newly provisioned tenants always have permissions to link.
func EnsurePermissionCatalog(ctx context.Context, pool *pgxpool.Pool) error {
	for _, p := range Permissions {
		if _, err := pool.Exec(ctx, `
			INSERT INTO permissions (id, code, resource, action, name)
			VALUES ($1,$2,$3,$4,$5) ON CONFLICT (code) DO NOTHING`,
			"perm-"+p.Code, p.Code, p.Resource, p.Action, p.Name); err != nil {
			return err
		}
	}
	return nil
}

// EnsurePlatformAdmin guarantees the platform operator account exists
// (system tenant code='platform', users.is_platform_admin=true, ADR-006).
func EnsurePlatformAdmin(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, log *slog.Logger) error {
	var tenantID string
	err := pool.QueryRow(ctx, `SELECT id FROM tenants WHERE code='platform'`).Scan(&tenantID)
	if err == pgx.ErrNoRows {
		tenantID = "platform-system-tenant"
		if _, err := pool.Exec(ctx, `
			INSERT INTO tenants (id, name, code, status, metadata, created_by)
			VALUES ($1,'平台运营（系统）','platform','active','{"system": true}','bootstrap')
			ON CONFLICT (code) DO NOTHING`, tenantID); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE tenant_id=$1 AND is_platform_admin=true`, tenantID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := passhash.Hash(cfg.PlatformAdminPassword)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, tenant_id, username, password_hash, display_name, locale, status, is_platform_admin, created_by)
		VALUES ($1,$2,'platform',$3,'平台管理员','zh-CN','active',true,'bootstrap')
		ON CONFLICT (tenant_id, username) DO NOTHING`,
		ulidx.New(), tenantID, hash)
	if err == nil {
		log.Info("platform administrator ensured", "username", "platform")
	}
	return err
}

// ---------- HTTP handlers ----------

type registerRequest struct {
	TenantName  string `json:"tenant_name"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

// register is the public SaaS self-signup endpoint (POST /auth/register).
// On success the caller receives a token pair — no separate login needed.
func (s *SaaSAPI) register(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil || !s.cfg.RegistrationEnabled {
		httpx.Respond(w, r, nil, apierr.PermissionDenied("self-registration is disabled on this deployment"))
		return
	}
	var req registerRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if strings.TrimSpace(req.TenantName) == "" || req.Username == "" || len(req.Password) < 6 {
		httpx.Respond(w, r, nil, apierr.Invalid("tenant_name, username and a password of >= 6 chars are required"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	tenantID, err := s.ProvisionTenant(ctx, ProvisionInput{
		TenantName:    req.TenantName,
		AdminUsername: req.Username,
		AdminPassword: req.Password,
		AdminEmail:    req.Email,
		DisplayName:   req.DisplayName,
	})
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	var code, userID string
	if err := s.pool.QueryRow(ctx, `SELECT code FROM tenants WHERE id=$1`, tenantID).Scan(&code); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT id FROM users WHERE tenant_id=$1 AND username=$2`,
		tenantID, req.Username).Scan(&userID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	// Auto-login: the freshly created administrator receives a token pair.
	pair, u, err := s.module.issueTokens(ctx, r, tenantID, userID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	s.log.Info("tenant self-registered", "code", code)
	httpx.Created(w, r, map[string]any{
		"tenant_code": code, "tokens": pair, "user": u,
		"message": "tenant created; you are signed in as its administrator",
	})
}

// ---------- platform console ----------

func (s *SaaSAPI) listTenants(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	search := r.URL.Query().Get("search")
	rows, err := s.pool.Query(r.Context(), `
		SELECT t.id, t.name, t.code, t.status, t.created_at,
			(SELECT count(*) FROM users u WHERE u.tenant_id = t.id) AS users,
			(SELECT count(*) FROM farms f WHERE f.tenant_id = t.id) AS farms,
			(SELECT count(*) FROM devices d WHERE d.tenant_id = t.id) AS devices
		FROM tenants t
		WHERE ($1 = '' OR t.status = $1)
		  AND ($2 = '' OR t.name ILIKE '%'||$2||'%' OR t.code ILIKE '%'||$2||'%')
		ORDER BY t.created_at DESC`, status, search)
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
	httpx.Respond(w, r, httpx.NewPage(items, int64(len(items)), httpx.PageParams{Page: 1, PageSize: len(items)}), nil)
}

type platformTenantWrite struct {
	Name          string `json:"name"`
	Status        string `json:"status"` // active | suspended
	Code          string `json:"code"`
	AdminUsername string `json:"admin_username"`
	AdminPassword string `json:"admin_password"`
	AdminEmail    string `json:"admin_email"`
	NewPassword   string `json:"new_password"`
}

func (s *SaaSAPI) createTenant(w http.ResponseWriter, r *http.Request) {
	var req platformTenantWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.AdminUsername == "" || len(req.AdminPassword) < 6 {
		httpx.Respond(w, r, nil, apierr.Invalid("admin_username and admin_password (>= 6 chars) are required"))
		return
	}
	tenantID, err := s.ProvisionTenant(r.Context(), ProvisionInput{
		TenantName:    req.Name,
		TenantCode:    req.Code,
		AdminUsername: req.AdminUsername,
		AdminPassword: req.AdminPassword,
		AdminEmail:    req.AdminEmail,
	})
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	var code string
	_ = s.pool.QueryRow(r.Context(), `SELECT code FROM tenants WHERE id=$1`, tenantID).Scan(&code)
	httpx.Created(w, r, map[string]any{"id": tenantID, "code": code})
}

func (s *SaaSAPI) patchTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	if tenantID == "platform-system-tenant" {
		httpx.Respond(w, r, nil, apierr.Invalid("the platform system tenant cannot be modified"))
		return
	}
	var req platformTenantWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.Name == "" && req.Status == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("nothing to update"))
		return
	}
	if req.Status != "" && req.Status != "active" && req.Status != "suspended" {
		httpx.Respond(w, r, nil, apierr.Invalid("status must be active or suspended"))
		return
	}
	_, err := s.pool.Exec(r.Context(), `
		UPDATE tenants SET
			name = COALESCE(NULLIF($2,''), name),
			status = COALESCE(NULLIF($3,''), status)
		WHERE id=$1 AND code <> 'platform'`, tenantID, req.Name, req.Status)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resetAdminPassword resets the oldest tenant_admin user's password of a tenant.
func (s *SaaSAPI) resetAdminPassword(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	var req platformTenantWrite
	if err := httpx.DecodeJSON(r, &req); err != nil || len(req.NewPassword) < 6 {
		httpx.Respond(w, r, nil, apierr.Invalid("new_password (>= 6 chars) is required"))
		return
	}
	var userID, username string
	err := s.pool.QueryRow(r.Context(), `
		SELECT u.id, u.username FROM users u
		JOIN user_roles ur ON ur.user_id = u.id
		JOIN roles r ON r.id = ur.role_id AND r.tenant_id = u.tenant_id AND r.code = 'tenant_admin'
		WHERE u.tenant_id = $1
		ORDER BY u.created_at LIMIT 1`, tenantID).Scan(&userID, &username)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("tenant admin user"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	hash, err := passhash.Hash(req.NewPassword)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if _, err := s.pool.Exec(r.Context(),
		`UPDATE users SET password_hash=$3 WHERE id=$1 AND tenant_id=$2`, userID, tenantID, hash); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	// Force re-login everywhere.
	if _, err := s.pool.Exec(r.Context(),
		`UPDATE sessions SET revoked=true WHERE user_id=$1`, userID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	s.log.Info("tenant admin password reset", "tenant", tenantID, "username", username)
	httpx.Respond(w, r, map[string]any{"reset": true, "username": username}, nil)
}
