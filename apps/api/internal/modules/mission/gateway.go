package mission

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

// MountDeviceGateway registers device-authenticated mission endpoints
// (specs/09.11 device side). Authentication = device code + token.
func (m *Module) MountDeviceGateway(r chi.Router) {
	r.Post("/device-gateway/missions/{id}/progress", m.deviceProgress)
}

type deviceProgressWrite struct {
	DeviceCode string  `json:"device_code"`
	Token      string  `json:"token"`
	Progress   float64 `json:"progress"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	Note       string  `json:"note"`
}

// deviceProgress lets the executing device (or simulator) report progress.
func (m *Module) deviceProgress(w http.ResponseWriter, r *http.Request) {
	var req deviceProgressWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	ctx := r.Context()
	missionID := chi.URLParam(r, "id")

	var tenantID, deviceID, status string
	err := m.pool.QueryRow(ctx, `
		SELECT mi.tenant_id, mi.device_id, mi.status
		FROM missions mi
		JOIN devices d ON d.id = mi.device_id AND d.tenant_id = mi.tenant_id
		WHERE mi.id = $1 AND d.code = $2 AND d.token_hash = $3`,
		missionID, req.DeviceCode, deviceHashToken(req.Token)).
		Scan(&tenantID, &deviceID, &status)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.Unauthenticated("invalid device credentials or mission"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if status != "executing" && status != "paused" {
		httpx.Respond(w, r, nil, apierr.Conflict("mission is not executing"))
		return
	}
	if req.Progress < 0 || req.Progress > 100 {
		httpx.Respond(w, r, nil, apierr.Invalid("progress must be 0-100"))
		return
	}

	if _, err := m.pool.Exec(ctx,
		`UPDATE missions SET progress=$3 WHERE tenant_id=$1 AND id=$2`,
		tenantID, missionID, req.Progress); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	note := req.Note
	if note == "" {
		note = "device progress " + ftoa(req.Progress) + "%"
	}
	_, _ = m.pool.Exec(ctx, `
		INSERT INTO mission_events (id, tenant_id, mission_id, type, message, payload, occurred_at)
		VALUES ($1,$2,$3,'progress',$4,to_jsonb($5::jsonb),now())`,
		ulidx.New(), tenantID, missionID, note,
		marshalExtra([]any{map[string]any{
			"progress": req.Progress,
			"position": map[string]float64{"lat": req.Latitude, "lng": req.Longitude},
		}}))
	m.pub.Publish(tenantID, "mission.updated", map[string]any{
		"mission_id": missionID, "status": status, "progress": req.Progress,
	})

	if req.Progress >= 100 && status == "executing" {
		if err := m.transition(ctx, tenantID, missionID, "executing", "completed", "mission completed"); err == nil {
			m.recordMissionTrace(ctx, tenantID, missionID, "mission", "Mission completed")
		}
	}
	httpx.Respond(w, r, map[string]any{"accepted": true, "progress": req.Progress}, nil)
}

func deviceHashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
