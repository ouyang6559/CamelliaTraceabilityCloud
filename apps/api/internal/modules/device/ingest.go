package device

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/geo"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

// TelemetryMessage is the unified device payload for MQTT and HTTPS ingestion.
type TelemetryMessage struct {
	DeviceCode string             `json:"device_code"`
	Token      string             `json:"token"`
	TS         time.Time          `json:"ts"`
	Status     string             `json:"status"`
	Metrics    map[string]float64 `json:"metrics"`
	Position   *geo.Point         `json:"position"`
	Battery    *int               `json:"battery_level"`
	Payload    map[string]any     `json:"payload"`
}

// authDevice resolves and authenticates a device by code+token (specs/08.3).
func (m *Module) authDevice(ctx context.Context, code, token string) (tenantID, deviceID, deviceType string, err error) {
	err = m.pool.QueryRow(ctx, `
		SELECT tenant_id, id, device_type FROM devices WHERE code = $1 AND token_hash = $2 AND status <> 'maintenance'`,
		code, hashToken(token)).Scan(&tenantID, &deviceID, &deviceType)
	return
}

// Ingest runs the telemetry pipeline: authenticate -> store -> device update ->
// observations -> alert rules -> NATS event. Local storage always succeeds
// before anything else (Local First).
func (m *Module) Ingest(ctx context.Context, msg TelemetryMessage) error {
	if msg.DeviceCode == "" || msg.Token == "" {
		return errAuthFailed
	}
	tenantID, deviceID, deviceType, err := m.authDevice(ctx, msg.DeviceCode, msg.Token)
	if err != nil {
		return errAuthFailed
	}
	ts := msg.TS
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	metricsJSON, _ := json.Marshal(msg.Metrics)
	payloadJSON, _ := json.Marshal(msg.Payload)
	newID := ulidx.New()
	if _, err := m.pool.Exec(ctx, `
		INSERT INTO device_telemetry (id, tenant_id, device_id, ts, topic, metrics, payload)
		VALUES ($1,$2,$3,$4,$5,to_jsonb(COALESCE($6::jsonb,'{}'::jsonb)),to_jsonb(COALESCE($7::jsonb,'{}'::jsonb)))`,
		newID, tenantID, deviceID, ts, "device-gateway", string(metricsJSON), string(payloadJSON)); err != nil {
		return err
	}

	// Device state updates.
	status := msg.Status
	switch status {
	case "online", "offline", "error":
	default:
		status = "online"
	}
	var loc any
	if msg.Position != nil && msg.Position.IsValid() {
		loc = msg.Position.WKT()
	}
	if _, err := m.pool.Exec(ctx, `
		UPDATE devices SET status = $3, last_seen_at = $4,
			location = COALESCE($5::geometry, location),
			battery_level = COALESCE($6, battery_level)
		WHERE tenant_id=$1 AND id=$2`,
		tenantID, deviceID, status, ts, loc, msg.Battery); err != nil {
		m.log.Warn("device state update failed", "device", msg.DeviceCode, "err", err)
	}

	// SensorThings-lite observations: one datastream per metric.
	for metric, value := range msg.Metrics {
		if err := m.recordObservation(ctx, tenantID, deviceID, metric, value, ts); err != nil {
			m.log.Warn("observation insert failed", "device", msg.DeviceCode, "metric", metric, "err", err)
		}
	}

	// Alert rule evaluation (wired by app assembly).
	if m.evalRule != nil && len(msg.Metrics) > 0 {
		m.evalRule(ctx, tenantID, deviceID, deviceType, msg.Metrics)
	}

	m.pub.Publish(tenantID, "device.telemetry", map[string]any{
		"device_id": deviceID, "device_code": msg.DeviceCode,
		"device_type": deviceType, "ts": ts, "metrics": msg.Metrics,
		"position": msg.Position, "status": status,
	})
	m.pub.Publish(tenantID, "device.status", map[string]any{
		"device_id": deviceID, "device_code": msg.DeviceCode, "status": status, "ts": ts,
	})
	return nil
}

// recordObservation upserts sensor + datastream for the metric and appends an observation.
func (m *Module) recordObservation(ctx context.Context, tenantID, deviceID, metric string, value float64, ts time.Time) error {
	var sensorID, streamID string
	err := m.pool.QueryRow(ctx, `
		SELECT ds.id, ds.sensor_id FROM datastreams ds
		WHERE ds.tenant_id=$1 AND ds.device_id=$2 AND ds.observed_property=$3`,
		tenantID, deviceID, metric).Scan(&streamID, &sensorID)
	if err == pgx.ErrNoRows {
		sensorID = ulidx.New()
		if _, err := m.pool.Exec(ctx, `
			INSERT INTO sensors (id, tenant_id, device_id, name, description)
			VALUES ($1,$2,$3,$4,'auto-registered from telemetry')`,
			sensorID, tenantID, deviceID, metric+" sensor"); err != nil {
			return err
		}
		streamID = ulidx.New()
		if _, err := m.pool.Exec(ctx, `
			INSERT INTO datastreams (id, tenant_id, sensor_id, device_id, name, observed_property, unit, description)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'auto-registered from telemetry')`,
			streamID, tenantID, sensorID, deviceID, metric, metric, unitForMetric(metric)); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	obsID := ulidx.New()
	_, err = m.pool.Exec(ctx, `
		INSERT INTO observations (id, tenant_id, datastream_id, device_id, result_number, phenomenon_time, result_time)
		VALUES ($1,$2,$3,$4,$5,$6,$6)`, obsID, tenantID, streamID, deviceID, value, ts)
	return err
}

func unitForMetric(metric string) string {
	units := map[string]string{
		"temperature": "°C", "humidity": "%", "soil_moisture": "%", "soil_temperature": "°C",
		"ph": "pH", "ec": "dS/m", "npk_n": "mg/kg", "npk_p": "mg/kg", "npk_k": "mg/kg",
		"wind_speed": "m/s", "rainfall": "mm", "pressure": "hPa", "light": "lx",
		"battery": "%", "speed": "m/s", "altitude": "m",
	}
	if u, ok := units[strings.ToLower(metric)]; ok {
		return u
	}
	return ""
}

var errAuthFailed = ingestError("device authentication failed")

type ingestError string

func (e ingestError) Error() string { return string(e) }

// MQTTHandler processes MQTT telemetry topics:
// ctc/v1/tenants/{tenant}/devices/{code}/telemetry
func (m *Module) MQTTHandler(topic string, payload []byte) {
	defer func() {
		if rec := recover(); rec != nil {
			m.log.Error("mqtt handler panic", "topic", topic, "err", rec)
		}
	}()
	var msg TelemetryMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		m.log.Warn("mqtt payload not JSON", "topic", topic, "err", err)
		return
	}
	// The device code in the topic is authoritative; accept both placements.
	if msg.DeviceCode == "" {
		msg.DeviceCode = deviceCodeFromTopic(topic)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.Ingest(ctx, msg); err != nil {
		m.log.Warn("mqtt ingest failed", "topic", topic, "err", err)
	}
}

func deviceCodeFromTopic(topic string) string {
	parts := strings.Split(topic, "/")
	for i, p := range parts {
		if p == "devices" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
