package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
)

// JWT issues and validates access tokens (specs/08.1).
type JWT struct {
	secret []byte
	ttl    time.Duration
}

func NewJWT(secret string, ttl time.Duration) *JWT {
	return &JWT{secret: []byte(secret), ttl: ttl}
}

func (j *JWT) TTL() time.Duration { return j.ttl }

func (j *JWT) Issue(claims ctxid.Claims) (token string, expiresAt time.Time, err error) {
	expiresAt = time.Now().Add(j.ttl)
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":      claims.UserID,
		"tid":      claims.TenantID,
		"username": claims.Username,
		"name":     claims.DisplayName,
		"roles":    claims.Roles,
		"perms":    claims.Perms,
		"exp":      expiresAt.Unix(),
		"iat":      time.Now().Unix(),
		"iss":      "ctc-api",
	})
	signed, err := t.SignedString(j.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

func (j *JWT) Parse(token string) (ctxid.Claims, error) {
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, apierr.Unauthenticated("unexpected signing method")
		}
		return j.secret, nil
	}, jwt.WithIssuedAt(), jwt.WithIssuer("ctc-api"))
	if err != nil || !parsed.Valid {
		return ctxid.Claims{}, apierr.Unauthenticated("invalid token")
	}
	mc, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return ctxid.Claims{}, apierr.Unauthenticated("invalid token claims")
	}
	sub, _ := mc["sub"].(string)
	tid, _ := mc["tid"].(string)
	username, _ := mc["username"].(string)
	name, _ := mc["name"].(string)
	roles := stringList(mc["roles"])
	perms := stringList(mc["perms"])
	if sub == "" || tid == "" {
		return ctxid.Claims{}, apierr.Unauthenticated("token missing subject or tenant")
	}
	return ctxid.Claims{
		UserID: sub, TenantID: tid, Username: username, DisplayName: name,
		Roles: roles, Perms: perms,
	}, nil
}

func stringList(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Auth resolves the Bearer token into an identity. Public routes opt out by not using this middleware.
func Auth(j *JWT) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				httpxUnauthorized(w, r, "missing Authorization header")
				return
			}
			token := strings.TrimPrefix(header, "Bearer ")
			if token == header {
				httpxUnauthorized(w, r, "Authorization must be a Bearer token")
				return
			}
			claims, err := j.Parse(token)
			if err != nil {
				httpxUnauthorized(w, r, err.Error())
				return
			}
			identity := ctxid.Identity{
				UserID: claims.UserID, TenantID: claims.TenantID,
				Username: claims.Username, DisplayName: claims.DisplayName,
				Roles: claims.Roles, Perms: claims.Perms,
			}
			next.ServeHTTP(w, r.WithContext(ctxid.WithIdentity(r.Context(), identity)))
		})
	}
}

func httpxUnauthorized(w http.ResponseWriter, r *http.Request, msg string) {
	httpx.Respond(w, r, nil, apierr.Unauthenticated(msg))
}

// RequirePerm returns 403 unless the identity holds resource:action (specs/08.2 RBAC).
func RequirePerm(resource, action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := ctxid.From(r.Context())
			if !ok {
				httpx.Respond(w, r, nil, apierr.Unauthenticated("authentication required"))
				return
			}
			if !id.HasPerm(resource, action) {
				httpx.Respond(w, r, nil, apierr.PermissionDenied("missing permission "+resource+":"+action))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CtxWithTenant is used by device-gateway endpoints that authenticate devices (not users).
func CtxWithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantKey{}, tenantID)
}

type tenantKey struct{}

func TenantFromCtx(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(tenantKey{}).(string)
	return v, ok
}
