// Package identity implements auth (JWT + refresh sessions), users, roles and
// the permission catalog (specs/08.1, specs/08.2).
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/middleware"
)

// Permission is one entry of the platform permission catalog.
type Permission struct {
	Code     string `json:"code"`
	Resource string `json:"resource"`
	Action   string `json:"action"`
	Name     string `json:"name"`
}

// Permissions is the single catalog; roles reference codes from here.
var Permissions = buildPermissions()

func buildPermissions() []Permission {
	var out []Permission
	add := func(res string, acts ...string) {
		for _, a := range acts {
			out = append(out, Permission{
				Code: res + ":" + a, Resource: res, Action: a,
				Name: res + " " + a,
			})
		}
	}
	biz := []string{"read", "create", "update", "delete"}
	add("tenant", "read", "update")
	add("user", biz...)
	add("role", biz...)
	add("farm", biz...)
	add("plot", biz...)
	add("crop", biz...)
	add("variety", biz...)
	add("crop-batch", biz...)
	add("task", biz...)
	add("task-record", "read", "create")
	add("input", biz...)
	add("input-batch", biz...)
	add("input-usage", "read", "create")
	add("disease-record", biz...)
	add("harvest", biz...)
	add("raw-material-batch", biz...)
	add("processing-batch", biz...)
	add("product", biz...)
	add("product-batch", biz...)
	add("package", biz...)
	add("warehouse", biz...)
	add("inventory", "read", "create")
	add("logistics", biz...)
	add("trace-event", "read", "create")
	add("trace-code", biz...)
	add("evidence", "read", "create")
	add("blockchain-proof", "read", "create")
	add("device", biz...)
	add("device-model", biz...)
	add("telemetry", "read")
	add("device-gateway", "create")
	add("mission", biz...)
	add("mission", "approve")
	add("geofence", biz...)
	add("observation", "read")
	add("alert", "read", "update")
	add("alert-rule", biz...)
	add("quality-test", biz...)
	add("quality-report", biz...)
	add("platform", biz...)
	add("sync-task", biz...)
	add("ai", "read", "create", "review")
	add("audit", "read")
	add("dashboard", "read")
	add("file", "create", "read")
	return out
}

// Role presets seeded per tenant.
var (
	AdminPerms    = allPermCodes()
	OperatorPerms = []string{
		"farm:read", "farm:update", "plot:read", "plot:update",
		"crop:read", "variety:read", "crop-batch:read", "crop-batch:update",
		"task:read", "task:create", "task:update", "task-record:read", "task-record:create",
		"input:read", "input-batch:read", "input-usage:read", "input-usage:create",
		"disease-record:read", "disease-record:create",
		"harvest:read", "harvest:create", "raw-material-batch:read", "processing-batch:read",
		"product:read", "product-batch:read", "package:read",
		"trace-event:read", "trace-code:read", "evidence:read", "evidence:create",
		"device:read", "device-model:read", "telemetry:read", "mission:read", "mission:create",
		"observation:read", "alert:read", "quality-test:read", "quality-report:read",
		"dashboard:read", "file:create", "file:read", "inventory:read",
	}
	ViewerPerms = []string{"*:read", "dashboard:read", "file:read"}
)

// allPermCodes returns every catalog code (used by the tenant_admin role).
func allPermCodes() []string {
	codes := make([]string, 0, len(Permissions))
	for _, p := range Permissions {
		codes = append(codes, p.Code)
	}
	return codes
}

// Module wires identity routes.
type Module struct {
	pool *pgxpool.Pool
	jwt  *middleware.JWT
}

func NewModule(pool *pgxpool.Pool, jwt *middleware.JWT) *Module {
	return &Module{pool: pool, jwt: jwt}
}

// ---------- shared helpers ----------

type userDTO struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name"`
	Email       string     `json:"email"`
	Phone       string     `json:"phone"`
	Locale      string     `json:"locale"`
	Status      string     `json:"status"`
	Roles       []string   `json:"roles"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

const userSelect = `SELECT u.id, u.tenant_id, u.username, u.display_name, u.email, u.phone,
	u.locale, u.status, u.last_login_at, u.created_at,
	COALESCE(json_agg(r.code) FILTER (WHERE r.code IS NOT NULL), '[]') AS roles
	FROM users u
	LEFT JOIN user_roles ur ON ur.user_id = u.id
	LEFT JOIN roles r ON r.id = ur.role_id`

func (m *Module) scanUser(ctx context.Context, userID, tenantID string) (userDTO, error) {
	var u userDTO
	var roles []byte
	err := m.pool.QueryRow(ctx, userSelect+" WHERE u.tenant_id=$1 AND u.id=$2 GROUP BY u.id",
		tenantID, userID).Scan(&u.ID, &u.TenantID, &u.Username, &u.DisplayName, &u.Email, &u.Phone,
		&u.Locale, &u.Status, &u.LastLoginAt, &u.CreatedAt, &roles)
	if err != nil {
		return u, err
	}
	u.Roles = parseJSONStrings(roles)
	return u, nil
}

func parseJSONStrings(raw []byte) []string {
	var out []string
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = []string{}
	}
	return out
}

// loadPerms returns the union of role codes and permission codes for a user.
func (m *Module) loadPerms(ctx context.Context, userID string) (roles, perms []string, err error) {
	rows, err := m.pool.Query(ctx, `
		SELECT DISTINCT r.code, p.code
		FROM user_roles ur
		JOIN roles r ON r.id = ur.role_id
		JOIN role_permissions rp ON rp.role_id = r.id
		JOIN permissions p ON p.id = rp.permission_id
		WHERE ur.user_id = $1`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	roleSet, permSet := map[string]bool{}, map[string]bool{}
	for rows.Next() {
		var role, perm string
		if err := rows.Scan(&role, &perm); err != nil {
			return nil, nil, err
		}
		roleSet[role], permSet[perm] = true, true
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	for r := range roleSet {
		roles = append(roles, r)
	}
	for p := range permSet {
		perms = append(perms, p)
	}
	if roles == nil {
		roles = []string{}
	}
	if perms == nil {
		perms = []string{}
	}
	return roles, perms, nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Mount registers /auth, /users, /roles, /tenants.
func (m *Module) Mount(r chi.Router) {
	users := NewUsersAPI(m.pool)
	roles := NewRolesAPI(m.pool)
	tenants := NewTenantsAPI(m.pool)

	r.Route("/auth", func(rr chi.Router) {
		rr.Post("/login", m.login)
		rr.Post("/refresh", m.refresh)
		rr.With(middleware.Auth(m.jwt)).Post("/logout", m.logout)
		rr.With(middleware.Auth(m.jwt)).Get("/me", m.me)
	})
	r.Route("/users", func(rr chi.Router) {
		rr.Use(middleware.Auth(m.jwt))
		rr.With(middleware.RequirePerm("user", "read")).Get("/", users.listUsers)
		rr.With(middleware.RequirePerm("user", "create")).Post("/", users.createUser)
		rr.With(middleware.RequirePerm("user", "update")).Patch("/{id}", users.updateUser)
		rr.With(middleware.RequirePerm("user", "delete")).Delete("/{id}", users.deleteUser)
	})
	r.Route("/roles", func(rr chi.Router) {
		rr.Use(middleware.Auth(m.jwt))
		rr.Get("/", roles.listRoles)
		rr.Get("/permissions", roles.listPermissions)
		rr.With(middleware.RequirePerm("role", "create")).Post("/", roles.createRole)
		rr.With(middleware.RequirePerm("role", "update")).Patch("/{id}", roles.updateRole)
		rr.With(middleware.RequirePerm("role", "delete")).Delete("/{id}", roles.deleteRole)
	})
	r.Route("/tenants", func(rr chi.Router) {
		rr.Use(middleware.Auth(m.jwt))
		rr.Get("/current", tenants.currentTenant)
		rr.With(middleware.RequirePerm("tenant", "update")).Patch("/current", tenants.updateTenant)
	})
}
