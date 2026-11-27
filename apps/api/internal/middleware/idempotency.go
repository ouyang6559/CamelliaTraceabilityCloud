// Package middleware — Idempotency-Key support (specs/09.8): replay-safe
// writes for create mission / trace event / sync / blockchain proof.
//
// When a request carries an Idempotency-Key header, the middleware scopes it
// to (tenant, method, path) and either replays the previously stored 2xx
// response or executes the handler once and stores the response in Redis
// (TTL 24h). Only 2xx responses are stored — a failed attempt can be retried
// with the same key. Requests without the header pass through untouched.
package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
)

const idemTTL = 24 * time.Hour

type idemRecord struct {
	Status int    `json:"status"`
	Body   []byte `json:"body"`
}

// bufferedWriter captures the response for idempotent storage.
type bufferedWriter struct {
	http.ResponseWriter
	buf    bytes.Buffer
	status int
}

func (w *bufferedWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *bufferedWriter) Write(b []byte) (int, error) {
	w.buf.Write(b)
	return w.ResponseWriter.Write(b)
}

// Idempotency returns the middleware; attach it to routes that must be
// replay-safe. Requests without an Idempotency-Key header are untouched.
func Idempotency(rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Idempotency-Key")
			if key == "" || rdb == nil {
				next.ServeHTTP(w, r)
				return
			}
			var tenantID string
			if id, ok := ctxid.From(r.Context()); ok {
				tenantID = id.TenantID
			}
			redisKey := "ctc:idem:" + tenantID + ":" + r.Method + ":" + r.URL.Path + ":" + key

			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()

			// Replay path: stored 2xx response for this (tenant, method, path, key).
			if raw, err := rdb.Get(ctx, redisKey).Bytes(); err == nil {
				var rec idemRecord
				if json.Unmarshal(raw, &rec) == nil && rec.Status >= 200 && rec.Status < 300 {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.Header().Set("Idempotent-Replayed", "true")
					w.WriteHeader(rec.Status)
					_, _ = w.Write(rec.Body)
					return
				}
			}
			// Miss/expired: execute normally. Concurrent duplicates with the
			// same key each execute; the last stored response wins (SETNX with
			// an "in-flight" marker is deliberately avoided so a crashed
			// handler never poisons the key).

			bw := &bufferedWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(bw, r)

			if bw.status >= 200 && bw.status < 300 {
				if rec, err := json.Marshal(idemRecord{Status: bw.status, Body: bw.buf.Bytes()}); err == nil {
					go func() {
						sctx, scancel := context.WithTimeout(context.Background(), 2*time.Second)
						defer scancel()
						_ = rdb.Set(sctx, redisKey, rec, idemTTL).Err()
					}()
				}
			}
			// Non-2xx responses are not stored — retriable with the same key.
		})
	}
}
