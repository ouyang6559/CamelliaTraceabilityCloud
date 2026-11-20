// Package stream exposes Server-Sent Events fed by NATS domain events
// (specs/09.10 Realtime). Tenant isolation: each connection subscribes only to
// its own tenant subject.
package stream

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
)

// Module wires the SSE endpoint.
type Module struct {
	conn *nats.Conn
	log  *slog.Logger
	auth func(http.Handler) http.Handler
}

func NewModule(conn *nats.Conn, log *slog.Logger, auth func(http.Handler) http.Handler) *Module {
	return &Module{conn: conn, log: log, auth: auth}
}

// Mount registers /stream (SSE).
func (m *Module) Mount(r chi.Router) {
	r.With(m.auth).Get("/stream", m.serve)
}

func (m *Module) serve(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	subject := "ctc." + id.TenantID + ".>"
	msgCh := make(chan *nats.Msg, 64)
	sub, err := m.conn.ChanSubscribe(subject, msgCh)
	if err != nil {
		m.log.Warn("sse subscribe failed", "err", err)
		return
	}
	defer func() {
		if sub != nil {
			_ = sub.Unsubscribe()
		}
	}()

	// Initial ping so clients know the stream is live.
	fmt.Fprint(w, "event: connected\ndata: {\"ok\":true}\n\n")
	flusher.Flush()

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case msg := <-msgCh:
			event := "message"
			prefix := "ctc." + id.TenantID + "."
			if len(msg.Subject) > len(prefix) {
				event = msg.Subject[len(prefix):]
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, sanitizeJSON(msg.Data))
			flusher.Flush()
		}
	}
}

// sanitizeJSON guarantees valid single-line JSON payloads.
func sanitizeJSON(data []byte) string {
	var probe map[string]any
	if err := json.Unmarshal(data, &probe); err != nil {
		return "{}"
	}
	out, _ := json.Marshal(probe)
	return string(out)
}
