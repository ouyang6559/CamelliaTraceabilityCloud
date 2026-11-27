// Package trace implements the TraceEvent hash chain, trace codes (one-code-per-item)
// and the trace graph (specs/05: TraceEvent First).
package trace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/events"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/geo"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

const hashVersion = "ctc-trace-v1"

// Service appends events to the per-tenant hash chain.
type Service struct {
	pool *pgxpool.Pool
	pub  *events.Publisher
	log  *slog.Logger
}

func NewService(pool *pgxpool.Pool, pub *events.Publisher, log *slog.Logger) *Service {
	return &Service{pool: pool, pub: pub, log: log}
}

// EventInput is the normalized payload for one trace event.
type EventInput struct {
	EventType   string         `json:"event_type"`
	EventTime   time.Time      `json:"event_time"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	FarmID      string         `json:"farm_id"`
	PlotID      string         `json:"plot_id"`
	CropBatchID string         `json:"crop_batch_id"`
	RefType     string         `json:"ref_type"`
	RefID       string         `json:"ref_id"`
	TaskID      string         `json:"task_id"`
	DeviceID    string         `json:"device_id"`
	MissionID   string         `json:"mission_id"`
	OperatorID  string         `json:"operator_id"`
	Location    *geo.Point     `json:"location"`
	Quantity    *float64       `json:"quantity"`
	Unit        string         `json:"unit"`
	Inputs      any            `json:"inputs"`
	Media       any            `json:"media"`
	Weather     map[string]any `json:"weather"`
	Metadata    map[string]any `json:"metadata"`
}

// Append inserts a trace event inside the tenant's hash chain and publishes
// the trace.created domain event. Local commit never depends on event delivery.
func (s *Service) Append(ctx context.Context, tenantID string, in EventInput) (map[string]any, error) {
	if in.EventType == "" {
		return nil, fmt.Errorf("event_type is required")
	}
	if in.EventTime.IsZero() {
		in.EventTime = time.Now().UTC()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Serialize chain appends per tenant.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, tenantID); err != nil {
		return nil, err
	}
	var seq int64
	var prevHash string
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(sequence),0),
			COALESCE((SELECT hash FROM trace_events WHERE tenant_id = $1 ORDER BY sequence DESC LIMIT 1), '')
		 FROM trace_events WHERE tenant_id = $1`,
		tenantID).Scan(&seq, &prevHash); err != nil {
		return nil, err
	}
	seq++

	payload := canonicalJSON(in)
	hash := computeHash(seq, prevHash, tenantID, payload)

	newID := ulidx.New()
	var locVal any
	if in.Location != nil && in.Location.IsValid() {
		locVal = fmt.Sprintf("POINT(%f %f)", in.Location.Lng, in.Location.Lat)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO trace_events
			(id, tenant_id, event_type, event_time, title, description,
			 farm_id, plot_id, crop_batch_id, ref_type, ref_id,
			 task_id, device_id, mission_id, operator_id,
			 location, quantity, unit, inputs, media, weather, metadata,
			 sequence, prev_hash, hash, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,
			COALESCE($16::geometry, NULL), $17, $18,
			to_jsonb(COALESCE($19::jsonb, '[]'::jsonb)),
			to_jsonb(COALESCE($20::jsonb, '[]'::jsonb)),
			to_jsonb(COALESCE($21::jsonb, '{}'::jsonb)),
			to_jsonb(COALESCE($22::jsonb, '{}'::jsonb)),
			$23, $24, $25, $26)`,
		newID, tenantID, in.EventType, in.EventTime, in.Title, in.Description,
		nullStr(in.FarmID), nullStr(in.PlotID), nullStr(in.CropBatchID), in.RefType, in.RefID,
		nullStr(in.TaskID), nullStr(in.DeviceID), nullStr(in.MissionID), nullStr(in.OperatorID),
		locVal, in.Quantity, in.Unit,
		jsonOrNil(in.Inputs), jsonOrNil(in.Media), jsonOrNil(in.Weather), jsonOrNil(in.Metadata),
		seq, prevHash, hash, nullStr(in.OperatorID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	s.pub.Publish(tenantID, "trace.created", map[string]any{
		"event_id": newID, "event_type": in.EventType,
		"ref_type": in.RefType, "ref_id": in.RefID,
	})
	s.log.Debug("trace event appended", "tenant", tenantID, "seq", seq, "type", in.EventType)

	return map[string]any{
		"id": newID, "sequence": seq, "prev_hash": prevHash, "hash": hash,
		"event_type": in.EventType, "event_time": in.EventTime,
		"ref_type": in.RefType, "ref_id": in.RefID,
	}, nil
}

// canonicalJSON renders the hashed payload; verification rebuilds the exact
// same structure from stored rows, so every field must round-trip stably.
// nil collections are normalized to empty ones because the database stores
// them as '[]'/'{}' (COALESCE) and they scan back as non-nil empties.
func canonicalJSON(in EventInput) string {
	var loc any
	if in.Location != nil {
		loc = map[string]float64{"lat": in.Location.Lat, "lng": in.Location.Lng}
	}
	b, _ := json.Marshal(map[string]any{
		"type": in.EventType, "title": in.Title, "description": in.Description,
		"farm_id": in.FarmID, "plot_id": in.PlotID, "crop_batch_id": in.CropBatchID,
		"ref_type": in.RefType, "ref_id": in.RefID,
		"task_id": in.TaskID, "device_id": in.DeviceID, "mission_id": in.MissionID,
		"operator_id": in.OperatorID, "location": loc,
		"quantity": in.Quantity, "unit": in.Unit,
		"inputs":   normalizeCollection(in.Inputs, emptySlice),
		"media":    normalizeCollection(in.Media, emptySlice),
		"weather":  normalizeCollection(in.Weather, emptyObject),
		"metadata": normalizeCollection(in.Metadata, emptyObject),
	})
	return string(b)
}

// sentinel empties used when a JSON value round-trips to null.
var (
	emptySlice  = []any{}
	emptyObject = map[string]any{}
)

// normalizeCollection round-trips any JSON value through encoding/json so both
// append and verification see identical shapes (e.g. json.RawMessage("null")
// and a SQL NULL jsonb column must both normalize to the same empty value).
func normalizeCollection(v any, empty any) any {
	if v == nil {
		return empty
	}
	b, err := json.Marshal(v)
	if err != nil {
		return empty
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil || out == nil {
		return empty
	}
	return out
}

// computeHash = SHA-256(hashVersion|seq|prev_hash|tenant|payload).
func computeHash(seq int64, prevHash, tenantID, payload string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s|%s|%s", hashVersion, seq, prevHash, tenantID, payload)))
	return hex.EncodeToString(sum[:])
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func jsonOrNil(v any) any {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case map[string]any, []any:
		b, err := json.Marshal(t)
		if err != nil {
			return nil
		}
		return string(b)
	case string:
		return t
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return nil
		}
		return string(b)
	}
}
