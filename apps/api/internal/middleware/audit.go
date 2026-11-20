package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

// Audit records every mutating request (specs/08.5) at the transport level:
// actor, tenant, IP, UA, route action and payload summary. Domain services add
// richer old/new values for critical transitions.
func Audit(pool *pgxpool.Pool, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			var body []byte
			if r.Body != nil {
				raw, err := io.ReadAll(io.LimitReader(r.Body, 16<<10))
				if err == nil {
					body = raw
					r.Body = io.NopCloser(bytes.NewReader(raw))
				}
			}
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)

			id, _ := ctxid.From(r.Context())
			actor := id.DisplayName
			if actor == "" {
				actor = id.Username
			}
			summary := json.RawMessage("{}")
			if len(body) > 0 {
				var probe map[string]any
				if err := json.Unmarshal(body, &probe); err == nil {
					redactSecrets(probe)
					if b, err := json.Marshal(probe); err == nil && len(b) <= 4096 {
						summary = json.RawMessage(b)
					}
				}
			}
			if pool != nil {
				go func() {
					_, err := pool.Exec(context.Background(),
						`INSERT INTO audit_logs (id, tenant_id, actor_id, actor_name, action, resource_type, resource_id, new_value, ip, user_agent, created_at)
						 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now())`,
						ulidx.New(), id.TenantID, id.UserID, actor,
						methodAction(r.Method), routeResource(r), pathID(r),
						summary, clientIP(r), r.UserAgent())
					if err != nil {
						log.Warn("audit insert failed", "err", err)
					}
				}()
			}
			_ = sw
		})
	}
}

// routeResource maps /api/v1/farms/123/records -> "farms".
func routeResource(r *http.Request) string {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	for i, p := range parts {
		if p == "api" && i+2 < len(parts) {
			return parts[i+2]
		}
	}
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

// pathID returns the trailing path segment when it looks like an ID.
func pathID(r *http.Request) string {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	last := parts[len(parts)-1]
	if last == "" || len(last) < 10 {
		return ""
	}
	return last
}

func methodAction(method string) string {
	switch method {
	case http.MethodPost:
		return "create"
	case http.MethodPut, http.MethodPatch:
		return "update"
	case http.MethodDelete:
		return "delete"
	default:
		return strings.ToLower(method)
	}
}

var sensitiveKeys = map[string]struct{}{
	"password": {}, "new_password": {}, "old_password": {}, "token": {}, "secret": {}, "api_key": {},
}

func redactSecrets(m map[string]any) {
	for k := range m {
		if _, hit := sensitiveKeys[k]; hit {
			m[k] = "***"
		}
	}
}
