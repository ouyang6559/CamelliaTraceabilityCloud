package device

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	sdk "github.com/ouyang6559/CamelliaTraceabilityCloud/adapters/device"
)

// Registry routes vendor device integration through DeviceAdapter
// implementations registered by adapter_type (specs/20, specs/03.2).
// Core business code (mission engine, command API) never references vendor
// packages directly — it dispatches by adapter_type, and each adapter lives in
// adapters/device/<type>/. Adding a vendor = implement sdk.Adapter + Register
// one entry (docs/adr/adr-005).
//
// Dispatch is best-effort after the local state transition (Local First,
// specs/01.7): a failed dispatch never rolls back the mission record; it is
// logged as a mission_event so operators can retry.
type Registry struct {
	pool     *pgxpool.Pool
	log      *slog.Logger
	adapters map[string]sdk.Adapter
}

func NewRegistry(pool *pgxpool.Pool, log *slog.Logger) *Registry {
	return &Registry{pool: pool, log: log, adapters: map[string]sdk.Adapter{}}
}

// Register makes an adapter available under its adapter_type.
func (r *Registry) Register(adapterType string, a sdk.Adapter) {
	if r.adapters == nil {
		r.adapters = map[string]sdk.Adapter{}
	}
	r.adapters[adapterType] = a
}

// Registered lists registered adapter types (diagnostics).
func (r *Registry) Registered() []string {
	out := make([]string, 0, len(r.adapters))
	for k := range r.adapters {
		out = append(out, k)
	}
	return out
}

type deviceRow struct {
	ID           string
	Code         string
	Name         string
	DeviceType   string
	Capabilities []string
	AdapterType  string
	Metadata     map[string]any
}

// adapterForDevice loads the device and resolves its adapter implementation.
func (r *Registry) adapterForDevice(ctx context.Context, tenantID, deviceID string) (sdk.Device, sdk.Adapter, error) {
	var row deviceRow
	err := r.pool.QueryRow(ctx, `
		SELECT id, code, name, device_type, capabilities, adapter_type, metadata
		FROM devices WHERE tenant_id=$1 AND id=$2`, tenantID, deviceID).
		Scan(&row.ID, &row.Code, &row.Name, &row.DeviceType, &row.Capabilities, &row.AdapterType, &row.Metadata)
	if err != nil {
		return sdk.Device{}, nil, fmt.Errorf("device lookup: %w", err)
	}
	if row.AdapterType == "" {
		return sdk.Device{}, nil, fmt.Errorf("device %s has no adapter_type (ingest-only device)", row.Code)
	}
	a, ok := r.adapters[row.AdapterType]
	if !ok {
		return sdk.Device{}, nil, fmt.Errorf("no adapter registered for adapter_type %q", row.AdapterType)
	}
	meta := map[string]string{}
	for k, v := range row.Metadata {
		if s, ok := v.(string); ok {
			meta[k] = s
		}
	}
	// The dispatching tenant is authoritative — adapters route their topics by
	// it (generic-mqtt: ctc/v1/tenants/{tenant}/devices/{code}/...).
	meta["tenant"] = tenantID
	return sdk.Device{
		ID:           row.ID,
		Code:         row.Code,
		Name:         row.Name,
		DeviceType:   row.DeviceType,
		Capabilities: row.Capabilities,
		Endpoint:     meta["endpoint"],
		Metadata:     meta,
	}, a, nil
}

// DispatchMission sends mission lifecycle commands to the device through its
// adapter. action ∈ create|start|pause|resume|stop. payload carries
// mission_type/route for start (converted to the SDK Mission shape here so
// adapters stay vendor-neutral).
func (r *Registry) DispatchMission(ctx context.Context, tenantID, deviceID, missionID, action string, payload map[string]any) error {
	dev, a, err := r.adapterForDevice(ctx, tenantID, deviceID)
	if err != nil {
		return err
	}
	mission := missionFromPayload(missionID, payload)
	switch action {
	case "create":
		return a.CreateMission(ctx, dev, mission)
	case "start":
		if err := a.CreateMission(ctx, dev, mission); err != nil {
			return err
		}
		return a.StartMission(ctx, dev, missionID)
	case "pause":
		return a.PauseMission(ctx, dev, missionID)
	case "resume":
		return a.ResumeMission(ctx, dev, missionID)
	case "stop":
		return a.StopMission(ctx, dev, missionID)
	default:
		return fmt.Errorf("unknown dispatch action %q", action)
	}
}

// missionFromPayload converts the NATS/DB envelope into the SDK Mission shape.
// route may arrive as []any (caller-decoded), json.RawMessage (raw JSON from
// the mission service) or a JSON string — all are normalized here so adapters
// stay vendor-neutral regardless of caller.
func missionFromPayload(missionID string, payload map[string]any) sdk.Mission {
	m := sdk.Mission{ID: missionID}
	if v, ok := payload["mission_type"].(string); ok {
		m.Type = v
	}
	if v, ok := payload["metadata"].(map[string]any); ok {
		m.Params = map[string]string{}
		for k, pv := range v {
			if s, ok := pv.(string); ok {
				m.Params[k] = s
			}
		}
	}
	route := normalizeRoute(payload["route"])
	for _, wp := range route {
		wpm, ok := wp.(map[string]any)
		if !ok {
			continue
		}
		w := sdk.Waypoint{}
		if v, ok := wpm["lat"].(float64); ok {
			w.Lat = v
		}
		if v, ok := wpm["lng"].(float64); ok {
			w.Lng = v
		}
		if v, ok := wpm["alt"].(float64); ok {
			w.Alt = v
		}
		if v, ok := wpm["speed"].(float64); ok {
			w.Speed = v
		}
		if v, ok := wpm["action"].(string); ok {
			w.Action = v
		}
		m.Waypoints = append(m.Waypoints, w)
	}
	return m
}

func normalizeRoute(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case json.RawMessage:
		var arr []any
		if json.Unmarshal(t, &arr) == nil {
			return arr
		}
	case string:
		var arr []any
		if json.Unmarshal([]byte(t), &arr) == nil {
			return arr
		}
	}
	return nil
}

// SendCommand routes a non-high-risk command to the device adapter
// (specs/03.10: high-risk actions must go through the mission pipeline).
func (r *Registry) SendCommand(ctx context.Context, tenantID, deviceID string, cmd sdk.Command) error {
	dev, a, err := r.adapterForDevice(ctx, tenantID, deviceID)
	if err != nil {
		return err
	}
	return a.SendCommand(ctx, dev, cmd)
}
