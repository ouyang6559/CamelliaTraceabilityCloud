package device

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

// gatewayTelemetry accepts telemetry over HTTPS with device credentials
// (mirror of the MQTT path; specs/09.11).
func (m *Module) gatewayTelemetry(w http.ResponseWriter, r *http.Request) {
	var msg TelemetryMessage
	if err := httpx.DecodeJSON(r, &msg); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if err := m.Ingest(r.Context(), msg); err != nil {
		httpx.Respond(w, r, nil, apierr.Unauthenticated(err.Error()))
		return
	}
	httpx.Respond(w, r, map[string]any{"accepted": true}, nil)
}

// deviceTelemetry returns raw telemetry rows for a device in a time window.
func (m *Module) deviceTelemetry(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	pp := httpx.GetPageParams(r)
	from, to := httpx.GetTime(r, "from"), httpx.GetTime(r, "to")
	args := []any{id.TenantID, chi.URLParam(r, "id")}
	conds := []string{"tenant_id = $1", "device_id = $2"}
	if !from.IsZero() {
		args = append(args, from)
		conds = append(conds, "ts >= $"+itoa(len(args)))
	}
	if !to.IsZero() {
		args = append(args, to)
		conds = append(conds, "ts <= $"+itoa(len(args)))
	}
	var total int64
	if err := m.pool.QueryRow(r.Context(),
		"SELECT count(*) FROM device_telemetry WHERE "+joinConds(conds), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := m.pool.Query(r.Context(), `
		SELECT id, device_id, ts, metrics, payload
		FROM device_telemetry WHERE `+joinConds(conds)+` ORDER BY ts DESC
		LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)), args...)
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
	httpx.Respond(w, r, httpx.NewPage(items, total, pp), nil)
}

// deviceLatest returns the most recent telemetry row plus current device state.
func (m *Module) deviceLatest(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	deviceID := chi.URLParam(r, "id")
	var status string
	var battery *int
	var lastSeen *time.Time
	var loc *map[string]any
	err = m.pool.QueryRow(r.Context(), `
		SELECT status, battery_level, last_seen_at, ST_AsGeoJSON(location)::jsonb
		FROM devices WHERE tenant_id=$1 AND id=$2`, id.TenantID, deviceID).
		Scan(&status, &battery, &lastSeen, &loc)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("device"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	latest := map[string]any{}
	rows, err := m.pool.Query(r.Context(), `
		SELECT ts, metrics FROM device_telemetry
		WHERE tenant_id=$1 AND device_id=$2 ORDER BY ts DESC LIMIT 1`, id.TenantID, deviceID)
	if err == nil {
		items, _ := pgx.CollectRows(rows, pgx.RowToMap)
		rows.Close()
		if len(items) > 0 {
			latest = items[0]
		}
	}
	httpx.Respond(w, r, map[string]any{
		"device_id": deviceID, "status": status, "battery_level": battery,
		"location": loc, "latest": latest,
	}, nil)
}

// deviceEvents lists device events (status changes, errors, geofence breaches).
func (m *Module) deviceEvents(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	pp := httpx.GetPageParams(r)
	var total int64
	if err := m.pool.QueryRow(r.Context(),
		`SELECT count(*) FROM device_events WHERE tenant_id=$1 AND device_id=$2`,
		id.TenantID, chi.URLParam(r, "id")).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	rows, err := m.pool.Query(r.Context(), `
		SELECT id, device_id, type, severity, message, payload, occurred_at
		FROM device_events WHERE tenant_id=$1 AND device_id=$2
		ORDER BY occurred_at DESC LIMIT $3 OFFSET $4`,
		id.TenantID, chi.URLParam(r, "id"), pp.PageSize, pp.Offset())
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
	httpx.Respond(w, r, httpx.NewPage(items, total, pp), nil)
}

func joinConds(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}

// RecordDeviceEvent stores a device event (used by mission engine and status flows).
func (m *Module) RecordDeviceEvent(ctx context.Context, tenantID, deviceID, eventType, severity, message string, payload map[string]any) {
	payloadJSON, _ := json.Marshal(payload)
	_, err := m.pool.Exec(ctx,
		`INSERT INTO device_events (id, tenant_id, device_id, type, severity, message, payload, occurred_at)
		 VALUES ($1,$2,$3,$4,$5,$6,to_jsonb(COALESCE($7::jsonb,'{}'::jsonb)),now())`,
		ulidx.New(), tenantID, deviceID, eventType, severity, message, string(payloadJSON))
	if err != nil {
		m.log.Warn("device event insert failed", "device", deviceID, "err", err)
	}
}
