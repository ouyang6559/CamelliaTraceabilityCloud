package platform

import (
	"log/slog"

	"github.com/nats-io/nats.go"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/events"
)

// EventsPublisher builds the domain event publisher; a nil connection yields
// a no-op publisher so the platform runs without NATS (specs/01.7).
func EventsPublisher(conn *nats.Conn, log *slog.Logger) *events.Publisher {
	return events.NewPublisher(conn, log)
}
