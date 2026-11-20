package identity

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/passhash"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

type loginRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	TenantCode string `json:"tenant_code"` // optional; defaults to the single matching tenant
}

type tokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (m *Module) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.Username == "" || req.Password == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("username and password are required"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Resolve tenant: explicit code first, otherwise the unique tenant of that username.
	var tenantID string
	if req.TenantCode != "" {
		err := m.pool.QueryRow(ctx, `SELECT id FROM tenants WHERE code = $1`, req.TenantCode).Scan(&tenantID)
		if err == pgx.ErrNoRows {
			httpx.Respond(w, r, nil, apierr.Unauthenticated("invalid credentials"))
			return
		}
		if err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
		userID, err := m.authenticate(ctx, tenantID, req.Username, req.Password)
		if err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
		pair, u, err := m.issueTokens(ctx, r, tenantID, userID)
		if err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
		httpx.Respond(w, r, map[string]any{"user": u, "tokens": pair}, nil)
		return
	}

	rows, err := m.pool.Query(ctx, `SELECT u.tenant_id, u.id, u.password_hash FROM users u
		JOIN tenants t ON t.id = u.tenant_id AND t.status = 'active'
		WHERE u.username = $1 AND u.status = 'active'`, req.Username)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	type candidate struct {
		tenantID, userID, hash string
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.tenantID, &c.userID, &c.hash); err == nil {
			cands = append(cands, c)
		}
	}
	rows.Close()

	for _, c := range cands {
		if passhash.Verify(c.hash, req.Password) {
			pair, u, err := m.issueTokens(ctx, r, c.tenantID, c.userID)
			if err != nil {
				httpx.Respond(w, r, nil, err)
				return
			}
			httpx.Respond(w, r, map[string]any{"user": u, "tokens": pair}, nil)
			return
		}
	}
	httpx.Respond(w, r, nil, apierr.Unauthenticated("invalid credentials"))
}

func (m *Module) authenticate(ctx context.Context, tenantID, username, password string) (string, error) {
	var userID, hash string
	err := m.pool.QueryRow(ctx,
		`SELECT id, password_hash FROM users WHERE tenant_id=$1 AND username=$2 AND status='active'`,
		tenantID, username).Scan(&userID, &hash)
	if err == pgx.ErrNoRows {
		return "", apierr.Unauthenticated("invalid credentials")
	}
	if err != nil {
		return "", err
	}
	if !passhash.Verify(hash, password) {
		return "", apierr.Unauthenticated("invalid credentials")
	}
	return userID, nil
}

// issueTokens creates a refresh session, returns tokens plus the user profile.
func (m *Module) issueTokens(ctx context.Context, r *http.Request, tenantID, userID string) (tokenPair, userDTO, error) {
	u, err := m.scanUser(ctx, userID, tenantID)
	if err != nil {
		return tokenPair{}, userDTO{}, err
	}
	roles, perms, err := m.loadPerms(ctx, userID)
	if err != nil {
		return tokenPair{}, userDTO{}, err
	}
	access, expiresAt, err := m.jwt.Issue(ctxid.Claims{
		UserID: userID, TenantID: tenantID, Username: u.Username,
		DisplayName: u.DisplayName, Roles: roles, Perms: perms,
	})
	if err != nil {
		return tokenPair{}, userDTO{}, err
	}
	refresh := randomToken()
	if _, err := m.pool.Exec(ctx, `INSERT INTO sessions (id, tenant_id, user_id, refresh_hash, user_agent, ip, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		ulidx.New(), tenantID, userID, hashToken(refresh), r.UserAgent(), clientIP(r),
		time.Now().Add(7*24*time.Hour)); err != nil {
		return tokenPair{}, userDTO{}, err
	}
	if _, err := m.pool.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, userID); err != nil {
		return tokenPair{}, userDTO{}, err
	}
	return tokenPair{AccessToken: access, RefreshToken: refresh, TokenType: "Bearer", ExpiresAt: expiresAt}, u, nil
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		for i := 0; i < len(fwd); i++ {
			if fwd[i] == ',' {
				return fwd[:i]
			}
		}
		return fwd
	}
	return r.RemoteAddr
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (m *Module) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := httpx.DecodeJSON(r, &req); err != nil || req.RefreshToken == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("refresh_token is required"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var sessID, tenantID, userID string
	err := m.pool.QueryRow(ctx,
		`SELECT id, tenant_id, user_id FROM sessions
		 WHERE refresh_hash = $1 AND revoked = false AND expires_at > now()`,
		hashToken(req.RefreshToken)).Scan(&sessID, &tenantID, &userID)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.Unauthenticated("invalid refresh token"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	// Rotate: revoke the used token and issue a fresh pair.
	if _, err := m.pool.Exec(ctx, `UPDATE sessions SET revoked = true WHERE id = $1`, sessID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	pair, u, err := m.issueTokens(ctx, r, tenantID, userID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, map[string]any{"user": u, "tokens": pair}, nil)
}

func (m *Module) logout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	_ = httpx.DecodeJSON(r, &req)
	if req.RefreshToken != "" {
		_, _ = m.pool.Exec(r.Context(), `UPDATE sessions SET revoked = true WHERE refresh_hash = $1`,
			hashToken(req.RefreshToken))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Module) me(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	u, err := m.scanUser(r.Context(), id.UserID, id.TenantID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	var tenant struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Code string `json:"code"`
	}
	_ = m.pool.QueryRow(r.Context(),
		`SELECT id, name, code FROM tenants WHERE id = $1`, id.TenantID).
		Scan(&tenant.ID, &tenant.Name, &tenant.Code)
	httpx.Respond(w, r, map[string]any{"user": u, "tenant": tenant, "permissions": id.Perms}, nil)
}
