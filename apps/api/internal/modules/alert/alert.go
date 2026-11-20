// Package alert implements threshold-based alert rules evaluated on telemetry
// and the alert lifecycle (open -> acknowledged -> resolved).
package alert

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/events"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

// Module wires alert routes and implements the device.AlertEvaluator contract.
type Module struct {
	pool *pgxpool.Pool
	pub  *events.Publisher
	log  *slog.Logger
}

func NewModule(pool *pgxpool.Pool, pub *events.Publisher, log *slog.Logger) *Module {
	return &Module{pool: pool, pub: pub, log: log}
}

// Mount registers /alert-rules and /alerts.
func (m *Module) Mount(r chi.Router, authorize func(resource, action string) func(http.Handler) http.Handler) {
	r.Route("/alert-rules", func(rr chi.Router) {
		rr.Use(authorize("alert-rule", "read"))
		rr.Get("/", m.listRules)
		rr.With(authorize("alert-rule", "create")).Post("/", m.createRule)
		rr.With(authorize("alert-rule", "update")).Patch("/{id}", m.updateRule)
		rr.With(authorize("alert-rule", "delete")).Delete("/{id}", m.deleteRule)
	})
	r.Route("/alerts", func(rr chi.Router) {
		rr.Use(authorize("alert", "read"))
		rr.Get("/", m.listAlerts)
		rr.With(authorize("alert", "update")).Post("/{id}/acknowledge", m.ackAlert)
		rr.With(authorize("alert", "update")).Post("/{id}/resolve", m.resolveAlert)
	})
}

// EvaluateTelemetry implements device.AlertEvaluator: matches open rules against
// incoming metrics and creates alerts (deduplicated per open rule+device).
func (m *Module) EvaluateTelemetry(ctx context.Context, tenantID, deviceID, deviceType string, metrics map[string]float64) {
	rows, err := m.pool.Query(ctx, `
		SELECT id, metric, operator, threshold::float8, severity, name
		FROM alert_rules WHERE tenant_id=$1 AND enabled=true AND (device_type='' OR device_type=$2)`,
		tenantID, deviceType)
	if err != nil {
		m.log.Warn("alert rule query failed", "err", err)
		return
	}
	type rule struct {
		id, metric, op, severity, name string
		threshold                      float64
	}
	var rules []rule
	for rows.Next() {
		var rl rule
		if err := rows.Scan(&rl.id, &rl.metric, &rl.op, &rl.threshold, &rl.severity, &rl.name); err == nil {
			rules = append(rules, rl)
		}
	}
	rows.Close()

	for _, rl := range rules {
		value, ok := metrics[rl.metric]
		if !ok {
			continue
		}
		if !compare(rl.op, value, rl.threshold) {
			continue
		}
		// Deduplicate: one open alert per rule+device.
		var openCount int
		if err := m.pool.QueryRow(ctx, `
			SELECT count(*) FROM alerts WHERE tenant_id=$1 AND rule_id=$2 AND device_id=$3 AND status='open'`,
			tenantID, rl.id, deviceID).Scan(&openCount); err != nil || openCount > 0 {
			continue
		}
		alertID := ulidx.New()
		if _, err := m.pool.Exec(ctx, `
			INSERT INTO alerts (id, tenant_id, rule_id, device_id, severity, status, title, message, payload, triggered_at)
			VALUES ($1,$2,$3,$4,$5,'open',$6,$7,to_jsonb($8::jsonb),now())`,
			alertID, tenantID, rl.id, deviceID, rl.severity,
			rl.name,
			rl.metric+" "+rl.op+" "+strconv.FormatFloat(rl.threshold, 'f', -1, 64)+
				" (current "+strconv.FormatFloat(value, 'f', -1, 64)+")",
			jsonOf(map[string]any{"metric": rl.metric, "value": value, "threshold": rl.threshold})); err != nil {
			m.log.Warn("alert insert failed", "err", err)
			continue
		}
		m.pub.Publish(tenantID, "alert.created", map[string]any{
			"alert_id": alertID, "rule": rl.name, "metric": rl.metric,
			"value": value, "threshold": rl.threshold, "severity": rl.severity, "device_id": deviceID,
		})
	}
}

func compare(op string, value, threshold float64) bool {
	switch op {
	case ">":
		return value > threshold
	case ">=":
		return value >= threshold
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	case "==", "=":
		return value == threshold
	default:
		return false
	}
}

func jsonOf(v any) string {
	b, _ := jsonMarshal(v)
	return string(b)
}

// ---------- rule handlers ----------

type ruleWrite struct {
	Name       string  `json:"name"`
	DeviceType string  `json:"device_type"`
	Metric     string  `json:"metric"`
	Operator   string  `json:"operator"`
	Threshold  float64 `json:"threshold"`
	Severity   string  `json:"severity"`
	Enabled    *bool   `json:"enabled"`
}

func (m *Module) listRules(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	rows, err := m.pool.Query(r.Context(), `
		SELECT id, tenant_id, name, device_type, metric, operator, threshold::float8 AS threshold,
			severity, enabled, created_at
		FROM alert_rules WHERE tenant_id=$1 ORDER BY created_at DESC`, id.TenantID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	defer rows.Close()
	items, err := collect(rows)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, httpx.NewPage(items, int64(len(items)), httpx.PageParams{Page: 1, PageSize: len(items)}), nil)
}

func (m *Module) createRule(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	var req ruleWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.Name == "" || req.Metric == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("name and metric are required"))
		return
	}
	if req.Operator == "" {
		req.Operator = ">"
	}
	if !isValidOperator(req.Operator) {
		httpx.Respond(w, r, nil, apierr.Invalid("operator must be one of > >= < <= =="))
		return
	}
	severity := req.Severity
	if severity == "" {
		severity = "warning"
	}
	newID := ulidx.New()
	if _, err := m.pool.Exec(r.Context(), `
		INSERT INTO alert_rules (id, tenant_id, name, device_type, metric, operator, threshold, severity, enabled, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,COALESCE($9,true),$10)`,
		newID, id.TenantID, req.Name, req.DeviceType, req.Metric, req.Operator,
		req.Threshold, severity, req.Enabled, id.UserID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Created(w, r, map[string]any{"id": newID})
}

func isValidOperator(op string) bool {
	for _, v := range []string{">", ">=", "<", "<=", "==", "="} {
		if v == op {
			return true
		}
	}
	return false
}

func (m *Module) updateRule(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	ruleID := chi.URLParam(r, "id")
	var req ruleWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	sets, args := []string{}, []any{}
	add := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, col+" = $"+strconv.Itoa(len(args)))
	}
	if req.Name != "" {
		add("name", req.Name)
	}
	if req.Metric != "" {
		add("metric", req.Metric)
	}
	if req.Operator != "" {
		add("operator", req.Operator)
	}
	if req.DeviceType != "" {
		add("device_type", req.DeviceType)
	}
	if req.Severity != "" {
		add("severity", req.Severity)
	}
	if req.Enabled != nil {
		add("enabled", *req.Enabled)
	}
	args = append(args, req.Threshold)
	sets = append(sets, "threshold = $"+strconv.Itoa(len(args)))
	if len(sets) > 0 {
		args = append(args, id.TenantID, ruleID)
		ct, err := m.pool.Exec(r.Context(), "UPDATE alert_rules SET "+strings.Join(sets, ", ")+
			" WHERE tenant_id = $"+strconv.Itoa(len(args)-1)+" AND id = $"+strconv.Itoa(len(args)), args...)
		if err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
		if ct.RowsAffected() == 0 {
			httpx.Respond(w, r, nil, apierr.NotFound("alert rule"))
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Module) deleteRule(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	ct, err := m.pool.Exec(r.Context(), `DELETE FROM alert_rules WHERE tenant_id=$1 AND id=$2`,
		id.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.NotFound("alert rule"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- alert handlers ----------

func (m *Module) listAlerts(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	q := r.URL.Query()
	args := []any{id.TenantID}
	conds := []string{"a.tenant_id = $1"}
	for param, col := range map[string]string{
		"status": "a.status", "severity": "a.severity", "device_id": "a.device_id",
	} {
		if v := q.Get(param); v != "" {
			args = append(args, v)
			conds = append(conds, col+" = $"+strconv.Itoa(len(args)))
		}
	}
	var total int64
	if err := m.pool.QueryRow(r.Context(),
		"SELECT count(*) FROM alerts a WHERE "+strings.Join(conds, " AND "), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := m.pool.Query(r.Context(), `
		SELECT a.id, a.tenant_id, a.rule_id, ar.name AS rule_name, a.device_id, d.name AS device_name,
			a.severity, a.status, a.title, a.message, a.payload, a.triggered_at,
			a.acknowledged_by, a.acknowledged_at, a.resolved_at
		FROM alerts a
		LEFT JOIN alert_rules ar ON ar.id = a.rule_id
		LEFT JOIN devices d ON d.id = a.device_id
		WHERE `+strings.Join(conds, " AND ")+` ORDER BY a.triggered_at DESC
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	defer rows.Close()
	items, err := collect(rows)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, httpx.NewPage(items, total, pp), nil)
}

func (m *Module) ackAlert(w http.ResponseWriter, r *http.Request) {
	m.updateAlertStatus(w, r, "acknowledged")
}

func (m *Module) resolveAlert(w http.ResponseWriter, r *http.Request) {
	m.updateAlertStatus(w, r, "resolved")
}

func (m *Module) updateAlertStatus(w http.ResponseWriter, r *http.Request, status string) {
	id, _ := ctxid.Must(r.Context())
	sets := []string{"status = '" + status + "'"}
	if status == "acknowledged" {
		sets = append(sets, "acknowledged_by = $3", "acknowledged_at = now()")
	}
	if status == "resolved" {
		sets = append(sets, "resolved_by = $3", "resolved_at = now()")
	}
	ct, err := m.pool.Exec(r.Context(), "UPDATE alerts SET "+strings.Join(sets, ", ")+
		" WHERE tenant_id=$1 AND id=$2 AND status <> 'resolved'",
		id.TenantID, chi.URLParam(r, "id"), id.UserID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.NotFound("alert"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var _ = time.Now
