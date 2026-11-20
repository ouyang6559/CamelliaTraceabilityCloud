// Package sync implements Local-First external platform synchronization
// (specs/06, specs/21): platform registry, TraceProvider interface, sync task
// queue with retries/dead-letter and a run-once worker.
//
// 第三方平台接口必须以官方文档为准；未取得文档的平台只提供 mock Provider +
// Integration TODO，绝不伪造 endpoint（specs/06.11）。
package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/events"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

// ProviderRegistry resolves a provider by platform provider_type.
type ProviderRegistry map[string]Provider

// Module wires sync routes and the worker.
type Module struct {
	pool      *pgxpool.Pool
	providers ProviderRegistry
	pub       *events.Publisher
	log       *slog.Logger
	interval  time.Duration
}

func NewModule(pool *pgxpool.Pool, providers ProviderRegistry, pub *events.Publisher, log *slog.Logger, interval time.Duration) *Module {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	return &Module{pool: pool, providers: providers, pub: pub, log: log, interval: interval}
}

// Start runs the background sync worker until ctx is cancelled.
func (m *Module) Start(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.RunOnce(ctx)
			}
		}
	}()
}

// RunOnce processes due sync tasks (PENDING / RETRYING / FAILED with backoff elapsed).
func (m *Module) RunOnce(ctx context.Context) {
	rows, err := m.pool.Query(ctx, `
		SELECT st.id, st.tenant_id, st.ref_type, st.ref_id, st.action, st.idempotency_key,
			st.payload, st.attempts, st.max_attempts, ep.provider_type
		FROM sync_tasks st
		JOIN external_platforms ep ON ep.id = st.platform_id
		WHERE st.status IN ($1,$2,$3) AND st.scheduled_at <= now()
		LIMIT 20`, StatusPending, StatusRetrying, StatusFailed)
	if err != nil {
		return
	}
	type taskRow struct {
		id, tenantID, refType, refID, action, idem, providerType string
		payload                                                  []byte
		attempts, maxAttempts                                    int
	}
	var tasks []taskRow
	for rows.Next() {
		var t taskRow
		if err := rows.Scan(&t.id, &t.tenantID, &t.refType, &t.refID, &t.action, &t.idem,
			&t.payload, &t.attempts, &t.maxAttempts, &t.providerType); err == nil {
			tasks = append(tasks, t)
		}
	}
	rows.Close()

	for _, t := range tasks {
		m.processTask(ctx, t.id, t.tenantID, t.refType, t.refID, t.action, t.idem, t.payload, t.attempts, t.maxAttempts, t.providerType)
	}
}

func (m *Module) processTask(ctx context.Context, taskID, tenantID, refType, refID, action, idem string, payload []byte, attempts, maxAttempts int, providerType string) {
	provider, ok := m.providers[providerType]
	if !ok {
		m.log.Warn("no provider registered", "provider_type", providerType)
		return
	}
	// Mark SYNCING (guarded transition).
	ct, err := m.pool.Exec(ctx, `
		UPDATE sync_tasks SET status=$3, attempts = attempts + 1, updated_at = now()
		WHERE id=$1 AND tenant_id=$2 AND status IN ($4,$5,$6)`,
		taskID, tenantID, StatusSyncing, StatusPending, StatusRetrying, StatusFailed)
	if err != nil || ct.RowsAffected() == 0 {
		return
	}
	m.logSync(ctx, tenantID, taskID, "info", "sync attempt started (provider "+provider.Name()+")")

	var payloadMap map[string]any
	_ = json.Unmarshal(payload, &payloadMap)
	externalID, perr := provider.Push(ctx, PushTask{
		TaskID: taskID, TenantID: tenantID, RefType: refType, RefID: refID,
		Action: action, IdempotencyKey: idem, Payload: payloadMap,
	})

	if perr != nil {
		status := StatusRetrying
		if attempts+1 >= maxAttempts {
			status = StatusFailed // dead-letter after max attempts
		}
		backoff := time.Duration(1<<uint(min(attempts, 5))) * time.Minute
		_, _ = m.pool.Exec(ctx, `
			UPDATE sync_tasks SET status=$3, last_error=$4, scheduled_at = now() + $5::interval, updated_at=now()
			WHERE id=$1 AND tenant_id=$2`,
			taskID, tenantID, status, perr.Error(), fmt.Sprintf("%dm", int(backoff.Minutes())))
		m.logSync(ctx, tenantID, taskID, "error", "sync failed: "+perr.Error())
		m.pub.Publish(tenantID, "sync.updated", map[string]any{"task_id": taskID, "status": status, "error": perr.Error()})
		return
	}
	_, _ = m.pool.Exec(ctx, `
		UPDATE sync_tasks SET status=$3, external_id=$4, synced_at=now(), last_error='', updated_at=now()
		WHERE id=$1 AND tenant_id=$2`, taskID, tenantID, StatusSynced, externalID)
	m.logSync(ctx, tenantID, taskID, "info", "synced, external_id="+externalID)
	m.pub.Publish(tenantID, "sync.updated", map[string]any{"task_id": taskID, "status": StatusSynced})
}

func (m *Module) logSync(ctx context.Context, tenantID, taskID, level, message string) {
	_, _ = m.pool.Exec(ctx, `
		INSERT INTO sync_logs (id, tenant_id, sync_task_id, level, message, created_at)
		VALUES ($1,$2,$3,$4,$5,now())`, ulidx.New(), tenantID, taskID, level, message)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---------- HTTP ----------

// Mount registers /external-platforms and /sync-tasks.
func (m *Module) Mount(r chi.Router, authorize func(resource, action string) func(http.Handler) http.Handler) {
	r.Route("/external-platforms", func(rr chi.Router) {
		rr.Use(authorize("platform", "read"))
		rr.Get("/", m.listPlatforms)
		rr.With(authorize("platform", "create")).Post("/", m.createPlatform)
		rr.With(authorize("platform", "update")).Patch("/{id}", m.updatePlatform)
		rr.With(authorize("platform", "delete")).Delete("/{id}", m.deletePlatform)
		rr.With(authorize("platform", "update")).Post("/{id}/test", m.testPlatform)
	})
	r.Route("/sync-tasks", func(rr chi.Router) {
		rr.Use(authorize("sync-task", "read"))
		rr.Get("/", m.listTasks)
		rr.Get("/{id}/logs", m.taskLogs)
		rr.With(authorize("sync-task", "create")).Post("/", m.createTask)
		rr.With(authorize("sync-task", "update")).Post("/{id}/retry", m.retryTask)
		rr.With(authorize("sync-task", "update")).Post("/run", m.runNow)
	})
}

type platformWrite struct {
	Name         string         `json:"name"`
	ProviderType string         `json:"provider_type"` // mock | national | hubei | himarking | antchain | tencent_zxin | nongzhan | lifei
	Status       string         `json:"status"`
	Capabilities []string       `json:"capabilities"`
	Config       map[string]any `json:"config"`
}

func (m *Module) listPlatforms(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	rows, err := m.pool.Query(r.Context(), `
		SELECT id, tenant_id, name, provider_type, status, capabilities, created_at
		FROM external_platforms WHERE tenant_id=$1 ORDER BY created_at DESC`, id.TenantID)
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

func (m *Module) createPlatform(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	var req platformWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.Name == "" || req.ProviderType == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("name and provider_type are required"))
		return
	}
	status := req.Status
	if status == "" {
		status = "CONNECTED"
	}
	if req.Capabilities == nil {
		req.Capabilities = []string{"BATCH", "TRACE_EVENT", "QUERY", "VERIFY"}
	}
	newID := ulidx.New()
	configJSON, _ := json.Marshal(req.Config)
	if _, err := m.pool.Exec(r.Context(), `
		INSERT INTO external_platforms (id, tenant_id, name, provider_type, status, capabilities, config, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,to_jsonb(COALESCE($7::jsonb,'{}'::jsonb)),$8)`,
		newID, id.TenantID, req.Name, req.ProviderType, status, req.Capabilities, string(configJSON), id.UserID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Created(w, r, map[string]any{"id": newID})
}

func (m *Module) updatePlatform(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	platformID := chi.URLParam(r, "id")
	var req platformWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	sets, args := []string{}, []any{}
	add := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, col+" = $"+itoa(len(args)))
	}
	if req.Name != "" {
		add("name", req.Name)
	}
	if req.Status != "" {
		add("status", req.Status)
	}
	if req.Capabilities != nil {
		add("capabilities", req.Capabilities)
	}
	if req.Config != nil {
		args = append(args, jsonOf(req.Config))
		sets = append(sets, "config = to_jsonb($"+itoa(len(args))+"::jsonb)")
	}
	if len(sets) > 0 {
		args = append(args, id.TenantID, platformID)
		ct, err := m.pool.Exec(r.Context(), "UPDATE external_platforms SET "+joinComma(sets)+
			" WHERE tenant_id = $"+itoa(len(args)-1)+" AND id = $"+itoa(len(args)), args...)
		if err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
		if ct.RowsAffected() == 0 {
			httpx.Respond(w, r, nil, apierr.NotFound("platform"))
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Module) deletePlatform(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	ct, err := m.pool.Exec(r.Context(), `DELETE FROM external_platforms WHERE tenant_id=$1 AND id=$2`,
		id.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Respond(w, r, nil, apierr.Conflict("platform referenced by sync tasks"))
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.NotFound("platform"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// testPlatform probes a platform connection via its provider (mock always succeeds).
func (m *Module) testPlatform(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	platformID := chi.URLParam(r, "id")
	var providerType string
	err := m.pool.QueryRow(r.Context(),
		`SELECT provider_type FROM external_platforms WHERE tenant_id=$1 AND id=$2`,
		id.TenantID, platformID).Scan(&providerType)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("platform"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	provider, ok := m.providers[providerType]
	if !ok {
		httpx.Respond(w, r, nil, apierr.Unavailable("no provider implementation for %s (integration TODO)", providerType))
		return
	}
	verified, err := provider.Verify(r.Context(), "ping-"+platformID)
	httpx.Respond(w, r, map[string]any{
		"provider":  provider.Name(),
		"reachable": err == nil && verified,
		"note":      "mock providers always succeed; real integrations require official credentials",
	}, nil)
}

type syncTaskWrite struct {
	PlatformID string         `json:"platform_id"`
	RefType    string         `json:"ref_type"`
	RefID      string         `json:"ref_id"`
	Action     string         `json:"action"`
	Payload    map[string]any `json:"payload"`
}

func (m *Module) createTask(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	var req syncTaskWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.PlatformID == "" || req.RefType == "" || req.RefID == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("platform_id, ref_type and ref_id are required"))
		return
	}
	if req.Action == "" {
		req.Action = "push"
	}
	newID := ulidx.New()
	payloadJSON, _ := json.Marshal(req.Payload)
	_, err := m.pool.Exec(r.Context(), `
		INSERT INTO sync_tasks (id, tenant_id, platform_id, ref_type, ref_id, action, status,
			idempotency_key, payload, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,to_jsonb(COALESCE($9::jsonb,'{}'::jsonb)),$10)`,
		newID, id.TenantID, req.PlatformID, req.RefType, req.RefID, req.Action,
		StatusPending, newID, string(payloadJSON), id.UserID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	m.logSync(r.Context(), id.TenantID, newID, "info", "task enqueued")
	httpx.Created(w, r, map[string]any{"id": newID, "status": StatusPending})
}

func (m *Module) listTasks(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	q := r.URL.Query()
	args := []any{id.TenantID}
	conds := []string{"st.tenant_id = $1"}
	for param, col := range map[string]string{
		"status": "st.status", "platform_id": "st.platform_id", "ref_type": "st.ref_type", "ref_id": "st.ref_id",
	} {
		if v := q.Get(param); v != "" {
			args = append(args, v)
			conds = append(conds, col+" = $"+itoa(len(args)))
		}
	}
	var total int64
	if err := m.pool.QueryRow(r.Context(),
		"SELECT count(*) FROM sync_tasks st WHERE "+joinAnd(conds), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := m.pool.Query(r.Context(), `
		SELECT st.id, st.tenant_id, st.platform_id, ep.name AS platform_name, ep.provider_type,
			st.ref_type, st.ref_id, st.action, st.status, st.attempts, st.max_attempts,
			st.idempotency_key, st.external_id, st.last_error, st.scheduled_at, st.synced_at, st.created_at
		FROM sync_tasks st
		JOIN external_platforms ep ON ep.id = st.platform_id
		WHERE `+joinAnd(conds)+` ORDER BY st.created_at DESC
		LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)), args...)
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

func (m *Module) taskLogs(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	rows, err := m.pool.Query(r.Context(), `
		SELECT id, sync_task_id, level, message, detail, created_at
		FROM sync_logs WHERE tenant_id=$1 AND sync_task_id=$2 ORDER BY created_at`,
		id.TenantID, chi.URLParam(r, "id"))
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
	httpx.Respond(w, r, items, nil)
}

func (m *Module) retryTask(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	ct, err := m.pool.Exec(r.Context(), `
		UPDATE sync_tasks SET status=$3, scheduled_at=now(), updated_at=now()
		WHERE tenant_id=$1 AND id=$2 AND status IN ($4,$5)`,
		id.TenantID, chi.URLParam(r, "id"), StatusPending, StatusFailed, StatusRetrying)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.Conflict("task not retryable"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Module) runNow(w http.ResponseWriter, r *http.Request) {
	m.RunOnce(r.Context())
	httpx.Respond(w, r, map[string]any{"ok": true}, nil)
}
