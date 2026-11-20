package mission

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/trace"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/geo"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

// safetyCheck runs the Safety Engine and moves the mission to
// awaiting_confirmation when all checks pass (human approval comes next).
// Checks: keep-out geofence breach, battery level, GPS presence (specs/13.2).
func (m *Module) safetyCheck(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	missionID := chi.URLParam(r, "id")
	row, err := m.fetchMission(r.Context(), id.TenantID, missionID)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("mission"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if row.Status != "safety_check" {
		httpx.Respond(w, r, nil, apierr.Conflict("mission must be in safety_check state"))
		return
	}

	checks := m.runChecks(r, id.TenantID, row)
	passed := true
	for _, c := range checks {
		if !c["passed"].(bool) {
			passed = false
			break
		}
	}
	safetyJSON, _ := json.Marshal(map[string]any{
		"passed":     passed,
		"checks":     checks,
		"checked_at": time.Now().UTC(),
		"checked_by": id.UserID,
	})
	newStatus := "awaiting_confirmation"
	if !passed {
		newStatus = "validation_failed"
	}
	if _, err := m.pool.Exec(r.Context(), `
		UPDATE missions SET status=$3, safety_check = to_jsonb($4::jsonb) WHERE tenant_id=$1 AND id=$2`,
		id.TenantID, missionID, newStatus, string(safetyJSON)); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	_, _ = m.pool.Exec(r.Context(), `
		INSERT INTO mission_events (id, tenant_id, mission_id, type, message, payload, occurred_at)
		VALUES ($1,$2,$3,'safety_checked',$4,to_jsonb($5::jsonb),now())`,
		ulidx.New(), id.TenantID, missionID,
		safetyMessage(passed, checks), string(safetyJSON))
	m.pub.Publish(id.TenantID, "mission.updated", map[string]any{
		"mission_id": missionID, "status": newStatus, "safety_passed": passed,
	})
	httpx.Respond(w, r, map[string]any{"status": newStatus, "passed": passed, "checks": checks}, nil)
}

func safetyMessage(passed bool, checks []map[string]any) string {
	if passed {
		return "safety checks passed, awaiting human confirmation"
	}
	return "safety checks failed"
}

type safetyCheckResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

func (m *Module) runChecks(r *http.Request, tenantID string, row *missionRow) []map[string]any {
	ctx := r.Context()
	results := []map[string]any{}
	add := func(name string, passed bool, detail string) {
		results = append(results, map[string]any{"name": name, "passed": passed, "detail": detail})
	}

	// 1. Keep-out geofence breach: any waypoint inside a keep_out fence fails.
	breaches := 0
	rows, err := m.pool.Query(ctx, `
		SELECT g.name FROM geofences g
		WHERE g.tenant_id = $1 AND g.active = true AND g.fence_type = 'keep_out'
			AND ST_Covers(g.boundary, ST_SetSRID(ST_GeomFromText($2),4326))`,
		tenantID, routeWKTPointList(row.Route))
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			breaches++
		}
	}
	add("geofence_keep_out", breaches == 0, strconv.Itoa(breaches)+" waypoint(s) inside keep-out zones")

	// 2. Battery level >= 20%.
	var battery *int
	_ = m.pool.QueryRow(ctx, `SELECT battery_level FROM devices WHERE tenant_id=$1 AND id=$2`,
		tenantID, row.DeviceID).Scan(&battery)
	if battery == nil {
		add("battery", true, "battery unknown (allowed for static devices)")
	} else if *battery < 20 {
		add("battery", false, "battery "+strconv.Itoa(*battery)+"% below minimum 20%")
	} else {
		add("battery", true, "battery "+strconv.Itoa(*battery)+"%")
	}

	// 3. Route has coordinates (GPS availability proxy).
	add("route_valid", len(row.Route) >= 2, strconv.Itoa(len(row.Route))+" waypoints")

	return results
}

// routeWKTPointList renders waypoints as a LINESTRING text for spatial checks.
func routeWKTPointList(route []waypoint) string {
	if len(route) == 0 {
		return "LINESTRING(0 0, 0 0)"
	}
	points := make([]string, 0, len(route))
	for _, wp := range route {
		points = append(points, ftoa(wp.Lng)+" "+ftoa(wp.Lat))
	}
	if len(points) == 1 {
		points = append(points, points[0])
	}
	out := "LINESTRING("
	for i, p := range points {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out + ")"
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', 6, 64) }

func itoa(n int) string { return strconv.Itoa(n) }

// approveMission: awaiting_confirmation -> approved. Requires the mission:approve
// permission — this is the explicit human confirmation step (human-in-the-loop).
func (m *Module) approveMission(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	missionID := chi.URLParam(r, "id")
	ct, err := m.pool.Exec(r.Context(), `
		UPDATE missions SET status='approved', approved_by=$3, approved_at=now()
		WHERE tenant_id=$1 AND id=$2 AND status='awaiting_confirmation'`,
		id.TenantID, missionID, id.UserID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.Conflict("mission must be awaiting confirmation to approve"))
		return
	}
	_, _ = m.pool.Exec(r.Context(), `
		INSERT INTO mission_events (id, tenant_id, mission_id, type, message, occurred_at)
		VALUES ($1,$2,$3,'approved',$4,now())`,
		ulidx.New(), id.TenantID, missionID, "approved by "+id.DisplayNameOrUsername())
	m.pub.Publish(id.TenantID, "mission.updated", map[string]any{"mission_id": missionID, "status": "approved"})
	httpx.Respond(w, r, map[string]any{"status": "approved"}, nil)
}

func (m *Module) startMission(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	missionID := chi.URLParam(r, "id")
	if err := m.transition(r.Context(), id.TenantID, missionID, "approved", "executing", "mission started"); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	// Publish the execution envelope; the device simulator (and external
	// gateways) pick it up on NATS, while real devices receive the command
	// through their DeviceAdapter below — two channels, one truth (ADR-005).
	if row, err := m.fetchMission(r.Context(), id.TenantID, missionID); err == nil && row != nil {
		routeJSON, _ := json.Marshal(row.Route)
		payload := map[string]any{
			"mission_id": missionID, "device_id": row.DeviceID,
			"mission_type": row.MissionType, "route": json.RawMessage(routeJSON),
		}
		m.pub.Publish(id.TenantID, "mission.started", payload)
		m.dispatchDevice(r.Context(), id.TenantID, row.DeviceID, missionID, "start", payload)
	}
	httpx.Respond(w, r, map[string]any{"status": "executing"}, nil)
}

func (m *Module) pauseMission(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	missionID := chi.URLParam(r, "id")
	if err := m.transition(r.Context(), id.TenantID, missionID, "executing", "paused", "mission paused"); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if row, err := m.fetchMission(r.Context(), id.TenantID, missionID); err == nil {
		m.dispatchDevice(r.Context(), id.TenantID, row.DeviceID, missionID, "pause", map[string]any{})
	}
	httpx.Respond(w, r, map[string]any{"status": "paused"}, nil)
}

func (m *Module) resumeMission(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	missionID := chi.URLParam(r, "id")
	if err := m.transition(r.Context(), id.TenantID, missionID, "paused", "executing", "mission resumed"); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if row, err := m.fetchMission(r.Context(), id.TenantID, missionID); err == nil {
		m.dispatchDevice(r.Context(), id.TenantID, row.DeviceID, missionID, "resume", map[string]any{})
	}
	httpx.Respond(w, r, map[string]any{"status": "executing"}, nil)
}

func (m *Module) stopMission(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	missionID := chi.URLParam(r, "id")
	row, err := m.fetchMission(r.Context(), id.TenantID, missionID)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("mission"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if row.Status != "executing" && row.Status != "paused" {
		httpx.Respond(w, r, nil, apierr.Conflict("only executing missions can be stopped"))
		return
	}
	if err := m.transition(r.Context(), id.TenantID, missionID, row.Status, "completed", "mission stopped early"); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	m.dispatchDevice(r.Context(), id.TenantID, row.DeviceID, missionID, "stop", map[string]any{})
	m.recordMissionTrace(r.Context(), id.TenantID, missionID, "mission", "Mission stopped")
	httpx.Respond(w, r, map[string]any{"status": "completed"}, nil)
}

func (m *Module) cancelMission(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	missionID := chi.URLParam(r, "id")
	ct, err := m.pool.Exec(r.Context(), `
		UPDATE missions SET status='cancelled', finished_at=now()
		WHERE tenant_id=$1 AND id=$2 AND status IN ('draft','validation_failed','safety_check','awaiting_confirmation','approved','executing','paused')`,
		id.TenantID, missionID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.Conflict("mission cannot be cancelled"))
		return
	}
	_, _ = m.pool.Exec(r.Context(), `
		INSERT INTO mission_events (id, tenant_id, mission_id, type, message, occurred_at)
		VALUES ($1,$2,$3,'cancelled','mission cancelled',now())`,
		ulidx.New(), id.TenantID, missionID)
	m.pub.Publish(id.TenantID, "mission.updated", map[string]any{"mission_id": missionID, "status": "cancelled"})
	httpx.Respond(w, r, map[string]any{"status": "cancelled"}, nil)
}

func (m *Module) failMission(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	missionID := chi.URLParam(r, "id")
	var req struct {
		Reason string `json:"reason"`
	}
	_ = httpx.DecodeJSON(r, &req)
	if req.Reason == "" {
		req.Reason = "mission failed"
	}
	ct, err := m.pool.Exec(r.Context(), `
		UPDATE missions SET status='failed', finished_at=now(),
			result = to_jsonb(jsonb_build_object('reason', $3))
		WHERE tenant_id=$1 AND id=$2 AND status IN ('executing','paused')`,
		id.TenantID, missionID, req.Reason)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.Conflict("only executing missions can fail"))
		return
	}
	_, _ = m.pool.Exec(r.Context(), `
		INSERT INTO mission_events (id, tenant_id, mission_id, type, message, occurred_at)
		VALUES ($1,$2,$3,'failed',$4,now())`,
		ulidx.New(), id.TenantID, missionID, req.Reason)
	m.pub.Publish(id.TenantID, "mission.updated", map[string]any{"mission_id": missionID, "status": "failed"})
	httpx.Respond(w, r, map[string]any{"status": "failed"}, nil)
}

type progressWrite struct {
	Progress  float64    `json:"progress"`
	Latitude  float64    `json:"latitude"`
	Longitude float64    `json:"longitude"`
	Note      string     `json:"note"`
	Position  *geo.Point `json:"position"`
}

// progress records execution updates; at 100% the mission completes and a
// trace event is appended (specs/02.4: Device -> Mission -> TraceEvent).
func (m *Module) progress(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	missionID := chi.URLParam(r, "id")
	var req progressWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.Progress < 0 || req.Progress > 100 {
		httpx.Respond(w, r, nil, apierr.Invalid("progress must be 0-100"))
		return
	}
	row, err := m.fetchMission(r.Context(), id.TenantID, missionID)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("mission"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if row.Status != "executing" {
		httpx.Respond(w, r, nil, apierr.Conflict("mission is not executing"))
		return
	}
	if _, err := m.pool.Exec(r.Context(),
		`UPDATE missions SET progress=$3 WHERE tenant_id=$1 AND id=$2`, id.TenantID, missionID, req.Progress); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	payload := map[string]any{"progress": req.Progress, "note": req.Note}
	note := req.Note
	if note == "" {
		note = "progress " + strconv.FormatFloat(req.Progress, 'f', 1, 64) + "%"
	}
	_, _ = m.pool.Exec(r.Context(), `
		INSERT INTO mission_events (id, tenant_id, mission_id, type, message, payload, occurred_at)
		VALUES ($1,$2,$3,'progress',$4,to_jsonb($5::jsonb),now())`,
		ulidx.New(), id.TenantID, missionID, note, marshalExtra([]any{payload}))
	m.pub.Publish(id.TenantID, "mission.updated", map[string]any{
		"mission_id": missionID, "status": "executing", "progress": req.Progress,
	})

	if req.Progress >= 100 {
		if err := m.transition(r.Context(), id.TenantID, missionID, "executing", "completed", "mission completed"); err != nil {
			// The mission may already be completed by a concurrent update; ignore.
			_ = err
		} else {
			m.recordMissionTrace(r.Context(), id.TenantID, missionID, "mission", "Mission completed")
		}
	}
	httpx.Respond(w, r, map[string]any{"progress": req.Progress}, nil)
}

// recordMissionTrace appends a trace event for a finished mission.
func (m *Module) recordMissionTrace(ctx context.Context, tenantID, missionID, refType, title string) {
	var deviceID, missionType string
	var progress float64
	err := m.pool.QueryRow(ctx, `
		SELECT device_id, mission_type, progress::float8 FROM missions WHERE tenant_id=$1 AND id=$2`,
		tenantID, missionID).Scan(&deviceID, &missionType, &progress)
	if err != nil {
		return
	}
	eventType := "uav_mission"
	if missionType == "mow" || missionType == "plow" || missionType == "transport" {
		eventType = "robot_mission"
	}
	_, _ = m.traceSvc.Append(ctx, tenantID, trace.EventInput{
		EventType: eventType,
		Title:     title + " (" + missionType + ")",
		RefType:   refType,
		RefID:     missionID,
		MissionID: missionID,
		DeviceID:  deviceID,
		Metadata:  map[string]any{"mission_type": missionType, "progress": progress},
	})
}

func (m *Module) listMissionEvents(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	rows, err := m.pool.Query(r.Context(), `
		SELECT id, mission_id, type, message, payload, occurred_at
		FROM mission_events WHERE tenant_id=$1 AND mission_id=$2 ORDER BY occurred_at`,
		id.TenantID, chi.URLParam(r, "id"))
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
	httpx.Respond(w, r, items, nil)
}
