// Package events publishes domain events on NATS (specs/01.6).
//
// Subject layout: ctc.<tenant_id>.<domain>.<event>
// e.g. ctc.01J...Z.device.telemetry
package events

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/nats-io/nats.go"
)

type Publisher struct {
	conn *nats.Conn
	log  *slog.Logger
}

func NewPublisher(conn *nats.Conn, log *slog.Logger) *Publisher {
	if log == nil {
		log = slog.Default()
	}
	return &Publisher{conn: conn, log: log}
}

// Publish serializes payload and publishes it under the tenant-scoped subject.
// Publishing must never fail the local business transaction (specs/01.7, Local First).
func (p *Publisher) Publish(tenantID, subject string, payload any) {
	if p == nil || p.conn == nil {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		p.log.Warn("events: marshal failed", "subject", subject, "err", err)
		return
	}
	full := "ctc." + tenantID + "." + subject
	if err := p.conn.Publish(full, data); err != nil {
		p.log.Warn("events: publish failed", "subject", full, "err", err)
	}
}

// Subscribe wraps a NATS subscription filtered to a single tenant.
func (p *Publisher) Subscribe(tenantID, pattern string, handler func(subject string, payload []byte)) error {
	sub, err := p.conn.Subscribe("ctc."+tenantID+"."+pattern, func(m *nats.Msg) {
		sub := strings.TrimPrefix(m.Subject, "ctc."+tenantID+".")
		handler(sub, m.Data)
	})
	if err != nil {
		return err
	}
	_ = sub
	return nil
}
