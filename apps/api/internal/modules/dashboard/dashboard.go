// Package dashboard aggregates workbench statistics (specs/11.2 工作台).
package dashboard

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
)

// Module wires dashboard routes.
type Module struct {
	pool *pgxpool.Pool
}

func NewModule(pool *pgxpool.Pool) *Module { return &Module{pool: pool} }

// Mount registers /dashboard.
func (m *Module) Mount(r chi.Router, authorize func(resource, action string) func(http.Handler) http.Handler) {
	r.With(authorize("dashboard", "read")).Get("/dashboard/summary", m.summary)
}

func (m *Module) summary(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	ctx := r.Context()
	count := func(table string) int64 {
		var n int64
		_ = m.pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", id.TenantID).Scan(&n)
		return n
	}
	openAlerts := func() int64 {
		var n int64
		_ = m.pool.QueryRow(ctx, `SELECT count(*) FROM alerts WHERE tenant_id=$1 AND status='open'`, id.TenantID).Scan(&n)
		return n
	}
	onlineDevices := func() int64 {
		var n int64
		_ = m.pool.QueryRow(ctx, `SELECT count(*) FROM devices WHERE tenant_id=$1 AND status='online'`, id.TenantID).Scan(&n)
		return n
	}
	openTasks := func() int64 {
		var n int64
		_ = m.pool.QueryRow(ctx, `SELECT count(*) FROM agricultural_tasks WHERE tenant_id=$1 AND status IN ('planned','in_progress')`, id.TenantID).Scan(&n)
		return n
	}

	// Last 14 days of trace event counts.
	trendRows, _ := m.pool.Query(ctx, `
		SELECT d::date AS day, (SELECT count(*) FROM trace_events te
			WHERE te.tenant_id=$1 AND te.event_time::date = d::date) AS events
		FROM generate_series(now()::date - interval '13 days', now()::date, interval '1 day') d`, id.TenantID)
	var trend []map[string]any
	if trendRows != nil {
		trend, _ = pgx.CollectRows(trendRows, pgx.RowToMap)
		trendRows.Close()
	}

	// Recent alerts (5).
	alertRows, _ := m.pool.Query(ctx, `
		SELECT a.id, a.severity, a.status, a.title, a.triggered_at, COALESCE(d.name,'') AS device_name
		FROM alerts a LEFT JOIN devices d ON d.id = a.device_id
		WHERE a.tenant_id=$1 ORDER BY a.triggered_at DESC LIMIT 5`, id.TenantID)
	var recentAlerts []map[string]any
	if alertRows != nil {
		recentAlerts, _ = pgx.CollectRows(alertRows, pgx.RowToMap)
		alertRows.Close()
	}

	// Upcoming tasks (5).
	taskRows, _ := m.pool.Query(ctx, `
		SELECT t.id, t.title, t.status, t.priority, t.planned_start_at, COALESCE(p.name,'') AS plot_name
		FROM agricultural_tasks t LEFT JOIN plots p ON p.id = t.plot_id
		WHERE t.tenant_id=$1 AND t.status IN ('planned','in_progress')
		ORDER BY t.planned_start_at ASC NULLS LAST LIMIT 5`, id.TenantID)
	var upcomingTasks []map[string]any
	if taskRows != nil {
		upcomingTasks, _ = pgx.CollectRows(taskRows, pgx.RowToMap)
		taskRows.Close()
	}

	httpx.Respond(w, r, map[string]any{
		"farms":          count("farms"),
		"plots":          count("plots"),
		"crop_batches":   count("crop_batches"),
		"tasks_open":     openTasks(),
		"devices":        count("devices"),
		"devices_online": onlineDevices(),
		"missions":       count("missions"),
		"trace_events":   count("trace_events"),
		"packages":       count("packages"),
		"trace_codes":    count("trace_codes"),
		"alerts_open":    openAlerts(),
		"event_trend":    trend,
		"recent_alerts":  recentAlerts,
		"upcoming_tasks": upcomingTasks,
	}, nil)
}
