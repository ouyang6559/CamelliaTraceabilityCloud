// Package device implements the device registry, telemetry ingest pipeline and
// the device gateway (specs/03, specs/09.11). Vendor logic stays in adapters.
package device

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	sdk "github.com/ouyang6559/CamelliaTraceabilityCloud/adapters/device"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/events"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/tenantref"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

// AlertEvaluator is implemented by the alert module; invoked on every telemetry
// batch so threshold rules fire without coupling the modules.
type AlertEvaluator func(ctx context.Context, tenantID, deviceID, deviceType string, metrics map[string]float64)

// Module wires device routes and the ingest pipeline.
type Module struct {
	pool     *pgxpool.Pool
	pub      *events.Publisher
	log      *slog.Logger
	evalRule AlertEvaluator
	registry *Registry
}

func NewModule(pool *pgxpool.Pool, pub *events.Publisher, log *slog.Logger) *Module {
	return &Module{pool: pool, pub: pub, log: log}
}

// SetRegistry wires the DeviceAdapter registry (app assembly); required for
// the command endpoint. specs/20: vendor logic stays behind the SDK interface.
func (m *Module) SetRegistry(r *Registry) { m.registry = r }

// SetAlertEvaluator wires the alert rule evaluator (called during app assembly).
func (m *Module) SetAlertEvaluator(fn AlertEvaluator) { m.evalRule = fn }

// Mount registers /device-models, /devices, /device-gateway.
func (m *Module) Mount(r chi.Router, auth func(http.Handler) http.Handler,
	authorize func(resource, action string) func(http.Handler) http.Handler) {
	authorizeModels := func(res, act string) func(http.Handler) http.Handler { return authorize("device-model", act) }
	r.Route("/device-models", func(rr chi.Router) {
		rr.Use(auth)
		rr.With(authorizeModels("device-model", "read")).Get("/", m.listModels)
		rr.With(authorizeModels("device-model", "create")).Post("/", m.createModel)
		rr.With(authorizeModels("device-model", "update")).Patch("/{id}", m.updateModel)
		rr.With(authorizeModels("device-model", "delete")).Delete("/{id}", m.deleteModel)
	})

	r.Route("/devices", func(rr chi.Router) {
		rr.Use(auth)
		rr.With(authorize("device", "read")).Get("/", m.listDevices)
		rr.With(authorize("device", "create")).Post("/", m.createDevice)
		rr.With(authorize("device", "read")).Get("/geojson", m.devicesGeoJSON)
		rr.With(authorize("device", "read")).Get("/{id}", m.getDevice)
		rr.With(authorize("device", "update")).Patch("/{id}", m.updateDevice)
		rr.With(authorize("device", "delete")).Delete("/{id}", m.deleteDevice)
		rr.With(authorize("device", "update")).Post("/{id}/rotate-token", m.rotateToken)
		rr.With(authorize("telemetry", "read")).Get("/{id}/telemetry", m.deviceTelemetry)
		rr.With(authorize("telemetry", "read")).Get("/{id}/latest", m.deviceLatest)
		rr.With(authorize("device", "read")).Get("/{id}/events", m.deviceEvents)
		rr.With(authorize("device", "update")).Post("/{id}/commands", m.sendCommand)
	})

}

// MountDeviceGateway registers device-token authenticated endpoints (no user
// JWT; specs/09.11 device side). Must be mounted on a public router.
func (m *Module) MountDeviceGateway(r chi.Router) {
	r.Post("/device-gateway/telemetry", m.gatewayTelemetry)
}

// ---------- device models ----------

type deviceModelDTO struct {
	ID           string         `json:"id"`
	Vendor       string         `json:"vendor"`
	Name         string         `json:"name"`
	DeviceType   string         `json:"device_type"`
	Capabilities []string       `json:"capabilities"`
	Metadata     map[string]any `json:"metadata"`
	CreatedAt    time.Time      `json:"created_at"`
}

func (m *Module) listModels(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	var total int64
	if err := m.pool.QueryRow(r.Context(), `SELECT count(*) FROM device_models WHERE tenant_id=$1`, id.TenantID).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	rows, err := m.pool.Query(r.Context(), `
		SELECT id, vendor, name, device_type, capabilities, metadata, created_at
		FROM device_models WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		id.TenantID, pp.PageSize, pp.Offset())
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

type modelWrite struct {
	Vendor       string         `json:"vendor"`
	Name         string         `json:"name"`
	DeviceType   string         `json:"device_type"`
	Capabilities []string       `json:"capabilities"`
	Metadata     map[string]any `json:"metadata"`
}

func (m *Module) createModel(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	var req modelWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.Name == "" || req.DeviceType == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("name and device_type are required"))
		return
	}
	newID := ulidx.New()
	meta, _ := json.Marshal(req.Metadata)
	if _, err := m.pool.Exec(r.Context(), `
		INSERT INTO device_models (id, tenant_id, vendor, name, device_type, capabilities, metadata, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,to_jsonb(COALESCE($7::jsonb,'{}'::jsonb)),$8)`,
		newID, id.TenantID, req.Vendor, req.Name, req.DeviceType, req.Capabilities, string(meta), id.UserID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Created(w, r, map[string]any{"id": newID})
}

func (m *Module) updateModel(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	modelID := chi.URLParam(r, "id")
	var req modelWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	ct, err := m.pool.Exec(r.Context(), `
		UPDATE device_models SET vendor = COALESCE(NULLIF($3,''), vendor),
			name = COALESCE(NULLIF($4,''), name),
			device_type = COALESCE(NULLIF($5,''), device_type),
			capabilities = COALESCE($6, capabilities)
		WHERE tenant_id=$1 AND id=$2`, id.TenantID, modelID, req.Vendor, req.Name, req.DeviceType, req.Capabilities)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.NotFound("device model"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Module) deleteModel(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	ct, err := m.pool.Exec(r.Context(), `DELETE FROM device_models WHERE tenant_id=$1 AND id=$2`,
		id.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Respond(w, r, nil, apierr.Conflict("model in use by devices"))
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.NotFound("device model"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- devices ----------

type deviceWrite struct {
	ModelID      string         `json:"model_id"`
	Name         string         `json:"name"`
	Code         string         `json:"code"`
	DeviceType   string         `json:"device_type"`
	Capabilities []string       `json:"capabilities"`
	FarmID       string         `json:"farm_id"`
	PlotID       string         `json:"plot_id"`
	Latitude     float64        `json:"latitude"`
	Longitude    float64        `json:"longitude"`
	Firmware     string         `json:"firmware"`
	Metadata     map[string]any `json:"metadata"`
}

func (m *Module) listDevices(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	q := r.URL.Query()
	args := []any{id.TenantID}
	conds := []string{"d.tenant_id = $1"}
	add := func(col string, val any) {
		args = append(args, val)
		conds = append(conds, "d."+col+" = $"+itoa(len(args)))
	}
	for param, col := range map[string]string{
		"status": "status", "device_type": "device_type", "farm_id": "farm_id", "plot_id": "plot_id",
	} {
		if v := q.Get(param); v != "" {
			add(col, v)
		}
	}
	if v := q.Get("search"); v != "" {
		args = append(args, "%"+strings.ToLower(v)+"%")
		conds = append(conds, "(lower(d.name) LIKE $"+itoa(len(args))+" OR lower(d.code) LIKE $"+itoa(len(args))+")")
	}
	var total int64
	if err := m.pool.QueryRow(r.Context(),
		"SELECT count(*) FROM devices d WHERE "+strings.Join(conds, " AND "), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := m.pool.Query(r.Context(), `
		SELECT d.id, d.tenant_id, d.model_id, dm.name AS model_name, d.name, d.code, d.device_type, d.status,
			d.capabilities, d.last_seen_at, d.farm_id, d.plot_id,
			ST_AsGeoJSON(d.location)::jsonb AS location, d.battery_level, d.firmware, d.metadata, d.created_at
		FROM devices d
		LEFT JOIN device_models dm ON dm.id = d.model_id
		WHERE `+strings.Join(conds, " AND ")+` ORDER BY d.created_at DESC
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

// createDevice registers a device and returns its token exactly once (specs/08.3).
func (m *Module) createDevice(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	var req deviceWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.Name == "" || req.DeviceType == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("name and device_type are required"))
		return
	}
	if err := tenantref.MustExistAll(r.Context(), m.pool, id.TenantID,
		[2]string{"device_model", req.ModelID},
		[2]string{"farm", req.FarmID},
		[2]string{"plot", req.PlotID},
	); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	code := req.Code
	if code == "" {
		code = "DEV-" + ulidx.New()[:8]
	}
	token := newDeviceToken()
	tokenHash := hashToken(token)

	newID := ulidx.New()
	var loc any
	if req.Latitude != 0 || req.Longitude != 0 {
		loc = "POINT(" + ftoa(req.Longitude) + " " + ftoa(req.Latitude) + ")"
	}
	meta, _ := json.Marshal(req.Metadata)
	if _, err := m.pool.Exec(r.Context(), `
		INSERT INTO devices (id, tenant_id, model_id, name, code, device_type, capabilities,
			token_hash, farm_id, plot_id, location, firmware, metadata, created_by)
		VALUES ($1,$2,NULLIF($3,''),$4,$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''),COALESCE($11::geometry,NULL),COALESCE(NULLIF($12,''),''),
			to_jsonb(COALESCE($13::jsonb,'{}'::jsonb)),$14)`,
		newID, id.TenantID, req.ModelID, req.Name, code, req.DeviceType, req.Capabilities,
		tokenHash, req.FarmID, req.PlotID, loc, req.Firmware, string(meta), id.UserID); err != nil {
		httpx.Respond(w, r, nil, mapDeviceErr(err))
		return
	}
	httpx.Created(w, r, map[string]any{
		"id": newID, "code": code, "token": token,
		"token_note": "Store this token now; it is not retrievable later. Rotate via POST /devices/{id}/rotate-token.",
	})
}

func (m *Module) getDevice(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	rows, err := m.pool.Query(r.Context(), `
		SELECT d.id, d.tenant_id, d.model_id, dm.name AS model_name, d.name, d.code, d.device_type, d.status,
			d.capabilities, d.last_seen_at, d.farm_id, d.plot_id,
			ST_AsGeoJSON(d.location)::jsonb AS location, d.battery_level, d.firmware, d.metadata, d.created_at
		FROM devices d LEFT JOIN device_models dm ON dm.id = d.model_id
		WHERE d.tenant_id=$1 AND d.id=$2`, id.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	item, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	rows.Close()
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("device"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, item, nil)
}

func (m *Module) updateDevice(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	deviceID := chi.URLParam(r, "id")
	var req deviceWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	sets, args := []string{}, []any{}
	addSet := func(expr string, val any) {
		args = append(args, val)
		sets = append(sets, expr+" = $"+itoa(len(args)))
	}
	if req.Name != "" {
		addSet("name", req.Name)
	}
	if req.ModelID != "" {
		addSet("model_id", req.ModelID)
	}
	if req.DeviceType != "" {
		addSet("device_type", req.DeviceType)
	}
	if req.Capabilities != nil {
		addSet("capabilities", req.Capabilities)
	}
	if req.FarmID != "" {
		addSet("farm_id", req.FarmID)
	}
	if req.PlotID != "" {
		addSet("plot_id", req.PlotID)
	}
	if req.Firmware != "" {
		addSet("firmware", req.Firmware)
	}
	if req.Latitude != 0 || req.Longitude != 0 {
		args = append(args, "POINT("+ftoa(req.Longitude)+" "+ftoa(req.Latitude)+")")
		sets = append(sets, "location = ST_SetSRID(ST_GeomFromText($"+itoa(len(args))+"),4326)")
	}
	if len(sets) > 0 {
		args = append(args, id.TenantID, deviceID)
		tenantIdx, idIdx := len(args)-1, len(args)
		ct, err := m.pool.Exec(r.Context(),
			"UPDATE devices SET "+strings.Join(sets, ", ")+", updated_by = $"+itoa(len(args)+1)+
				" WHERE tenant_id = $"+itoa(tenantIdx)+" AND id = $"+itoa(idIdx),
			append(args, id.UserID)...)
		if err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
		if ct.RowsAffected() == 0 {
			httpx.Respond(w, r, nil, apierr.NotFound("device"))
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Module) deleteDevice(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	ct, err := m.pool.Exec(r.Context(), `DELETE FROM devices WHERE tenant_id=$1 AND id=$2`,
		id.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Respond(w, r, nil, apierr.Conflict("device referenced by missions or telemetry"))
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.NotFound("device"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// rotateToken issues a new device token (shown once) and invalidates the old one.
func (m *Module) rotateToken(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	token := newDeviceToken()
	ct, err := m.pool.Exec(r.Context(),
		`UPDATE devices SET token_hash = $3, updated_by = $4 WHERE tenant_id=$1 AND id=$2`,
		id.TenantID, chi.URLParam(r, "id"), hashToken(token), id.UserID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.NotFound("device"))
		return
	}
	httpx.Respond(w, r, map[string]any{"token": token}, nil)
}

func (m *Module) devicesGeoJSON(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	rows, err := m.pool.Query(r.Context(), `
		SELECT id, name, code, device_type, status,
			COALESCE(ST_AsGeoJSON(location)::jsonb, 'null'::jsonb) AS geometry
		FROM devices WHERE tenant_id=$1 AND location IS NOT NULL`, id.TenantID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	defer rows.Close()
	features := []map[string]any{}
	for rows.Next() {
		var fid, name, code, deviceType, status string
		var geom map[string]any
		if err := rows.Scan(&fid, &name, &code, &deviceType, &status, &geom); err == nil {
			features = append(features, map[string]any{
				"type": "Feature", "id": fid, "geometry": geom,
				"properties": map[string]any{"name": name, "code": code, "device_type": deviceType, "status": status},
			})
		}
	}
	httpx.Respond(w, r, map[string]any{"type": "FeatureCollection", "features": features}, nil)
}

func mapDeviceErr(err error) error {
	if strings.Contains(err.Error(), "duplicate key") {
		return apierr.Conflict("device code already exists")
	}
	return err
}

// commandWhitelist lists the non-high-risk device commands callable directly.
// specs/03.10: takeoff/land/spray/mow/plow/autonomous/remote-control are
// high-risk and MUST go through the mission safety pipeline instead — they are
// deliberately rejected here (ADR-003/ADR-005).
var commandWhitelist = map[string]bool{
	"ping": true, "locate": true, "sync_logs": true,
	"led_on": true, "led_off": true, "charge_start": true, "charge_stop": true,
}

func (m *Module) sendCommand(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	var req struct {
		Name   string            `json:"name"`
		Params map[string]string `json:"params"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if !commandWhitelist[req.Name] {
		httpx.Respond(w, r, nil, apierr.Invalid(
			"command %q is not allowed on this endpoint; high-risk actions must go through the mission pipeline (specs/03.10)", req.Name))
		return
	}
	if m.registry == nil {
		httpx.Respond(w, r, nil, apierr.Unavailable("device adapter registry not configured"))
		return
	}
	if err := m.registry.SendCommand(r.Context(), id.TenantID, chi.URLParam(r, "id"), sdk.Command{
		Name:   req.Name,
		Params: req.Params,
	}); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, map[string]any{"sent": true, "command": req.Name}, nil)
}

func newDeviceToken() string { return "ctc_" + ulidx.New() + ulidx.New()[:12] }

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func itoa(n int) string { return strconv.Itoa(n) }

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', 6, 64) }
