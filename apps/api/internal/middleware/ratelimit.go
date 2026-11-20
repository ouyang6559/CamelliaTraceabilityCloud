package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
)

// RateLimit implements a fixed-window limiter per client IP (specs/08.6).
func RateLimit(rdb *redis.Client, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rdb == nil {
				next.ServeHTTP(w, r)
				return
			}
			ip := clientIP(r)
			key := "ctc:rl:" + ip + ":" + r.URL.Path
			ctx, cancel := context.WithTimeout(r.Context(), 500*time.Millisecond)
			defer cancel()
			pipe := rdb.TxPipeline()
			count := pipe.Incr(ctx, key)
			pipe.Expire(ctx, key, window)
			if _, err := pipe.Exec(ctx); err == nil {
				if count.Val() > int64(limit) {
					w.Header().Set("Retry-After", "1")
					httpx.Respond(w, r, nil, apierr.RateLimited())
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
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
