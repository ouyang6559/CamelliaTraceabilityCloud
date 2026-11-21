// Package mission implements the mission state machine with the safety engine:
// Validation -> Safety Check -> Authorization (human) -> Execution -> Audit
// (specs/03.10, specs/19 第六原则). High-risk actions never bypass this pipeline.
package mission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/crud"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/trace"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/events"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/geo"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/tenantref"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

// Module wires mission and geofence routes.
type Module struct {
	pool       *pgxpool.Pool
	traceSvc   *trace.Service
	pub        *events.Publisher
	devices    DeviceEventRecorder
	dispatcher MissionDispatcher
}

// MissionDispatcher sends mission lifecycle commands to the device through its
// DeviceAdapter (implemented by the device module Registry; specs/20). Local
// First: dispatch failures never roll back the committed mission state.
type MissionDispatcher interface {
	DispatchMission(ctx context.Context, tenantID, deviceID, missionID, action string, payload map[string]any) error
}

// SetDeviceDispatcher wires the adapter registry (app assembly).
func (m *Module) SetDeviceDispatcher(d MissionDispatcher) { m.dispatcher = d }

// dispatchDevice best-effort dispatches a lifecycle command; a failure is
// recorded as a mission_event for operator retry but never fails the request
// (the local state transition already committed, specs/01.7).
func (m *Module) dispatchDevice(ctx context.Context, tenantID, deviceID, missionID, action string, payload map[string]any) {
	if m.dispatcher == nil || deviceID == "" {
		return
	}
	if err := m.dispatcher.DispatchMission(ctx, tenantID, deviceID, missionID, action, payload); err != nil {
		_, _ = m.pool.Exec(ctx, `
			INSERT INTO mission_events (id, tenant_id, mission_id, type, message, payload, occurred_at)
			VALUES ($1,$2,$3,'dispatch_failed',$4,to_jsonb($5::jsonb),now())`,
			ulidx.New(), tenantID, missionID,
			"device dispatch failed: "+err.Error(),
			marshalExtra([]any{map[string]any{"action": action}}))
		m.pub.Publish(tenantID, "mission.updated", map[string]any{
			"mission_id": missionID, "dispatch_error": err.Error(),
		})
	}
}

// DeviceEventRecorder is the subset of the device module used by missions.
type DeviceEventRecorder interface {
	RecordDeviceEvent(ctx context.Context, tenantID, deviceID, eventType, severity, message string, payload map[string]any)
}

func NewModule(pool *pgxpool.Pool, traceSvc *trace.Service, pub *events.Publisher) *Module {
	return &Module{pool: pool, traceSvc: traceSvc, pub: pub}
}

// SetDeviceRecorder wires the device module (for device_events).
func (m *Module) SetDeviceRecorder(d DeviceEventRecorder) { m.devices = d }

// Mount registers /missions and /geofences.
func (m *Module) Mount(r chi.Router, authorize func(resource, action string) func(http.Handler) http.Handler) {
	geofences := crud.NewStore(m.pool, crud.Resource{
		Table: "geofences", Perm: "geofence",
		Columns: []crud.Col{
			{Name: "farm_id"},
			{Name: "name", Required: true},
			{Name: "fence_type"},
			{Name: "boundary", Geometry: true, Required: true},
			{Name: "active"},
			{Name: "note"},
		},
		Search:  []string{"name", "note"},
		Filters: []string{"farm_id", "fence_type"},
	})
	geofences.Mount(r, authorize)

	r.Route("/missions", func(rr chi.Router) {
		rr.Use(authorize("mission", "read"))
		rr.Get("/", m.listMissions)
		rr.With(authorize("mission", "create")).Post("/", m.createMission)
		rr.Get("/{id}", m.getMission)
		rr.Get("/{id}/events", m.listMissionEvents)
		rr.With(authorize("mission", "update")).Patch("/{id}", m.updateMission)
		rr.With(authorize("mission", "update")).Post("/{id}/validate", m.validateMission)
		rr.With(authorize("mission", "update")).Post("/{id}/safety-check", m.safetyCheck)
		rr.With(authorize("mission", "approve")).Post("/{id}/approve", m.approveMission)
		rr.With(authorize("mission", "update")).Post("/{id}/start", m.startMission)
		rr.With(authorize("mission", "update")).Post("/{id}/pause", m.pauseMission)
		rr.With(authorize("mission", "update")).Post("/{id}/resume", m.resumeMission)
		rr.With(authorize("mission", "update")).Post("/{id}/stop", m.stopMission)
		rr.With(authorize("mission", "update")).Post("/{id}/cancel", m.cancelMission)
		rr.With(authorize("mission", "update")).Post("/{id}/progress", m.progress)
		rr.With(authorize("mission", "update")).Post("/{id}/fail", m.failMission)
	})
}

// ---------- DTOs ----------

type waypoint struct {
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
	Alt    float64 `json:"alt"`
	Action string  `json:"action"` // takeoff | wayline | spray_on | spray_off | photo | land ...
	Speed  float64 `json:"speed"`
}

type missionWrite struct {
	DeviceID       string         `json:"device_id"`
	Name           string         `json:"name"`
	MissionType    string         `json:"mission_type"` // spray | mow | plow | seed | fertilize | map | inspect | transport | harvest
	Route          []waypoint     `json:"route"`
	PlannedStartAt *time.Time     `json:"planned_start_at"`
	PlannedEndAt   *time.Time     `json:"planned_end_at"`
	Metadata       map[string]any `json:"metadata"`
}

const missionSelect = `SELECT mi.id, mi.tenant_id, mi.device_id, d.name AS device_name, d.code AS device_code,
	mi.name, mi.mission_type, mi.status, mi.risk_level, mi.route, mi.planned_start_at, mi.planned_end_at,
	mi.started_at, mi.finished_at, mi.progress::float8 AS progress, mi.result, mi.safety_check,
	mi.approved_by, mi.approved_at, mi.created_by, mi.metadata, mi.created_at
	FROM missions mi LEFT JOIN devices d ON d.id = mi.device_id`

// capabilityFor maps a mission type to the device capability it requires (specs/03.3).
func capabilityFor(missionType string) string {
	switch missionType {
	case "spray":
		return "SPRAY"
	case "mow":
		return "MOW"
	case "plow":
		return "PLOW"
	case "seed":
		return "SEED"
	case "fertilize":
		return "FERTILIZE"
	case "map":
		return "MAPPING"
	case "inspect":
		return "INSPECTION"
	case "transport":
		return "AUTONOMOUS"
	default:
		return "MISSION"
	}
}

// riskLevelFor derives the risk tier; high-risk types require explicit approval.
func riskLevelFor(missionType string) string {
	switch missionType {
	case "spray", "mow", "plow", "seed":
		return "high"
	case "fertilize", "harvest":
		return "medium"
	default:
		return "low"
	}
}

type missionRow struct {
	ID          string
	TenantID    string
	DeviceID    string
	Name        string
	MissionType string
	Status      string
	Route       []waypoint
}

func (m *Module) fetchMission(ctx context.Context, tenantID, id string) (*missionRow, error) {
	var row missionRow
	var route []byte
	err := m.pool.QueryRow(ctx, `
		SELECT id, tenant_id, device_id, name, mission_type, status, route
		FROM missions WHERE tenant_id=$1 AND id=$2`, tenantID, id).
		Scan(&row.ID, &row.TenantID, &row.DeviceID, &row.Name, &row.MissionType, &row.Status, &route)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(route, &row.Route)
	return &row, nil
}

// transition moves the mission status and records a mission_event.
func (m *Module) transition(ctx context.Context, tenantID, missionID, from, to, message string, extra ...any) error {
	sets := []string{"status = $3"}
	args := []any{tenantID, missionID, to}
	if to == "executing" {
		sets = append(sets, "started_at = COALESCE(started_at, now())")
	}
	if to == "completed" || to == "failed" || to == "cancelled" {
		sets = append(sets, "finished_at = COALESCE(finished_at, now())")
	}
	query := fmt.Sprintf("UPDATE missions SET %s WHERE tenant_id=$1 AND id=$2 AND ($4 = '' OR status = $4)", strings.Join(sets, ", "))
	args = append(args, from)
	ct, err := m.pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return apierr.Conflict("mission cannot transition from %s to %s", from, to)
	}
	evID := ulidx.New()
	if _, err := m.pool.Exec(ctx, `
		INSERT INTO mission_events (id, tenant_id, mission_id, type, message, payload, occurred_at)
		VALUES ($1,$2,$3,$4,$5,to_jsonb(COALESCE($6::jsonb,'{}'::jsonb)),now())`,
		evID, tenantID, missionID, to, message, marshalExtra(extra)); err != nil {
		return err
	}
	m.pub.Publish(tenantID, "mission.updated", map[string]any{
		"mission_id": missionID, "status": to, "message": message,
	})
	return nil
}

func marshalExtra(extra []any) string {
	if len(extra) == 0 {
		return "{}"
	}
	b, _ := json.Marshal(extra[0])
	return string(b)
}

// ---------- handlers ----------

func (m *Module) listMissions(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	q := r.URL.Query()
	args := []any{id.TenantID}
	conds := []string{"mi.tenant_id = $1"}
	add := func(col string, val any) {
		args = append(args, val)
		conds = append(conds, "mi."+col+" = $"+itoa(len(args)))
	}
	for param, col := range map[string]string{
		"status": "status", "device_id": "device_id", "mission_type": "mission_type",
	} {
		if v := q.Get(param); v != "" {
			add(col, v)
		}
	}
	if v := q.Get("search"); v != "" {
		args = append(args, "%"+strings.ToLower(v)+"%")
		conds = append(conds, "lower(mi.name) LIKE $"+itoa(len(args)))
	}
	var total int64
	if err := m.pool.QueryRow(r.Context(),
		"SELECT count(*) FROM missions mi WHERE "+strings.Join(conds, " AND "), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := m.pool.Query(r.Context(), missionSelect+" WHERE "+strings.Join(conds, " AND ")+
		" ORDER BY mi.created_at DESC LIMIT $"+itoa(len(args)-1)+" OFFSET $"+itoa(len(args)), args...)
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

func (m *Module) createMission(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	var req missionWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.DeviceID == "" || req.Name == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("device_id and name are required"))
		return
	}
	if req.MissionType == "" {
		req.MissionType = "inspect"
	}
	if len(req.Route) < 2 {
		httpx.Respond(w, r, nil, apierr.Invalid("route requires at least 2 waypoints"))
		return
	}
	for _, wp := range req.Route {
		wpPoint := geo.Point{Lat: wp.Lat, Lng: wp.Lng}
		if !wpPoint.IsValid() {
			httpx.Respond(w, r, nil, apierr.Invalid("waypoint out of WGS84 bounds"))
			return
		}
	}
	// The mission device must belong to the caller's tenant (ADR-006).
	if err := tenantref.MustExist(r.Context(), m.pool, id.TenantID, "device", req.DeviceID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}

	newID := ulidx.New()
	routeJSON, _ := json.Marshal(req.Route)
	line := routeWKT(req.Route)
	meta, _ := json.Marshal(req.Metadata)

	if _, err := m.pool.Exec(r.Context(), `
		INSERT INTO missions (id, tenant_id, device_id, name, mission_type, status, risk_level,
			route, route_geometry, planned_start_at, planned_end_at, created_by, metadata)
		VALUES ($1,$2,$3,$4,$5,'draft',$6,to_jsonb(COALESCE($7::jsonb,'[]'::jsonb)),
			COALESCE($8::geometry,NULL),$9,$10,$11,to_jsonb(COALESCE($12::jsonb,'{}'::jsonb)))`,
		newID, id.TenantID, req.DeviceID, req.Name, req.MissionType, riskLevelFor(req.MissionType),
		string(routeJSON), line, req.PlannedStartAt, req.PlannedEndAt, id.UserID, string(meta)); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if _, err := m.pool.Exec(r.Context(), `
		INSERT INTO mission_events (id, tenant_id, mission_id, type, message, occurred_at)
		VALUES ($1,$2,$3,'created',$4,now())`, ulidx.New(), id.TenantID, newID, "mission created"); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Created(w, r, m.getMissionMap(r, id.TenantID, newID))
}

// routeWKT renders the waypoint list as a LineString WKT.
func routeWKT(route []waypoint) string {
	if len(route) < 2 {
		return ""
	}
	points := make([]string, 0, len(route))
	for _, wp := range route {
		points = append(points, fmt.Sprintf("%f %f", wp.Lng, wp.Lat))
	}
	return "LINESTRING(" + strings.Join(points, ", ") + ")"
}

func (m *Module) getMission(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	item := m.getMissionMap(r, id.TenantID, chi.URLParam(r, "id"))
	if item == nil {
		httpx.Respond(w, r, nil, apierr.NotFound("mission"))
		return
	}
	httpx.Respond(w, r, item, nil)
}

func (m *Module) getMissionMap(r *http.Request, tenantID, missionID string) map[string]any {
	rows, err := m.pool.Query(r.Context(), missionSelect+" WHERE mi.tenant_id=$1 AND mi.id=$2", tenantID, missionID)
	if err != nil {
		return nil
	}
	item, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	rows.Close()
	if err != nil {
		return nil
	}
	return item
}

func (m *Module) updateMission(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	missionID := chi.URLParam(r, "id")
	var req missionWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
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
	if row.Status != "draft" && row.Status != "validation_failed" {
		httpx.Respond(w, r, nil, apierr.Conflict("only draft missions can be edited"))
		return
	}
	if req.Name != "" {
		if _, err := m.pool.Exec(r.Context(),
			`UPDATE missions SET name=$3 WHERE tenant_id=$1 AND id=$2`, id.TenantID, missionID, req.Name); err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
	}
	if len(req.Route) >= 2 {
		routeJSON, _ := json.Marshal(req.Route)
		if _, err := m.pool.Exec(r.Context(),
			`UPDATE missions SET route = to_jsonb($3::jsonb), route_geometry = ST_SetSRID(ST_GeomFromText($4),4326)
			 WHERE tenant_id=$1 AND id=$2`,
			id.TenantID, missionID, string(routeJSON), routeWKT(req.Route)); err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// validateMission: draft -> safety_check | validation_failed.
// Checks device existence, capability (specs/03.3) and route integrity.
func (m *Module) validateMission(w http.ResponseWriter, r *http.Request) {
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
	if row.Status != "draft" && row.Status != "validation_failed" {
		httpx.Respond(w, r, nil, apierr.Conflict("mission must be in draft to validate"))
		return
	}
	var deviceType string
	var capabilities []string
	err = m.pool.QueryRow(r.Context(), `SELECT device_type, capabilities FROM devices WHERE tenant_id=$1 AND id=$2`,
		id.TenantID, row.DeviceID).Scan(&deviceType, &capabilities)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.Invalid("mission device does not exist"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	required := capabilityFor(row.MissionType)
	hasCap := false
	for _, c := range capabilities {
		if strings.EqualFold(c, required) || strings.EqualFold(c, "MISSION") {
			hasCap = true
			break
		}
	}
	if !hasCap {
		_, _ = m.pool.Exec(r.Context(),
			`UPDATE missions SET status='validation_failed' WHERE tenant_id=$1 AND id=$2`, id.TenantID, missionID)
		_, _ = m.pool.Exec(r.Context(), `
			INSERT INTO mission_events (id, tenant_id, mission_id, type, message, occurred_at)
			VALUES ($1,$2,$3,'validation_failed',$4,now())`,
			ulidx.New(), id.TenantID, missionID,
			"device lacks required capability "+required)
		httpx.Respond(w, r, nil, apierr.Invalid("device lacks required capability %s", required))
		return
	}
	if _, err := m.pool.Exec(r.Context(),
		`UPDATE missions SET status='safety_check' WHERE tenant_id=$1 AND id=$2`, id.TenantID, missionID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	_, _ = m.pool.Exec(r.Context(), `
		INSERT INTO mission_events (id, tenant_id, mission_id, type, message, occurred_at)
		VALUES ($1,$2,$3,'validated',$4,now())`,
		ulidx.New(), id.TenantID, missionID, "validation passed (capability "+required+")")
	m.pub.Publish(id.TenantID, "mission.updated", map[string]any{"mission_id": missionID, "status": "safety_check"})
	httpx.Respond(w, r, map[string]any{"status": "safety_check", "capability": required}, nil)
}
