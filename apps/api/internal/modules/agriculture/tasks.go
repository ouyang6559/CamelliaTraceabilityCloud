package agriculture

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/trace"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/tenantref"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

const taskSelect = `SELECT t.id, t.tenant_id, t.farm_id, t.plot_id, t.crop_batch_id, t.mission_id,
	t.type, t.title, t.description, t.priority, t.status, t.planned_start_at, t.planned_end_at,
	t.started_at, t.completed_at, t.assignee_id, t.source, t.metadata, t.created_at, t.updated_at
	FROM agricultural_tasks t`

type taskWrite struct {
	FarmID         string         `json:"farm_id"`
	PlotID         string         `json:"plot_id"`
	CropBatchID    string         `json:"crop_batch_id"`
	Type           string         `json:"type"`
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	Priority       string         `json:"priority"`
	PlannedStartAt *time.Time     `json:"planned_start_at"`
	PlannedEndAt   *time.Time     `json:"planned_end_at"`
	AssigneeID     string         `json:"assignee_id"`
	Metadata       map[string]any `json:"metadata"`
}

func (m *Module) listTasks(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	q := r.URL.Query()
	args := []any{id.TenantID}
	conds := []string{"t.tenant_id = $1"}
	add := func(col, op string, val any) {
		args = append(args, val)
		conds = append(conds, "t."+col+" "+op+" $"+strconv.Itoa(len(args)))
	}
	for param, col := range map[string]string{
		"status": "status", "type": "type", "plot_id": "plot_id",
		"crop_batch_id": "crop_batch_id", "farm_id": "farm_id", "assignee_id": "assignee_id",
	} {
		if v := q.Get(param); v != "" {
			add(col, "=", v)
		}
	}
	if v := q.Get("from"); v != "" {
		if t, ok := parseTimeParam(v); ok {
			add("planned_start_at", ">=", t)
		}
	}
	if v := q.Get("to"); v != "" {
		if t, ok := parseTimeParam(v); ok {
			add("planned_start_at", "<=", t)
		}
	}
	if v := q.Get("search"); v != "" {
		args = append(args, "%"+strings.ToLower(v)+"%")
		conds = append(conds, "lower(t.title) LIKE $"+strconv.Itoa(len(args)))
	}

	var total int64
	if err := m.pool.QueryRow(r.Context(),
		"SELECT count(*) FROM agricultural_tasks t WHERE "+strings.Join(conds, " AND "), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := m.pool.Query(r.Context(), taskSelect+" WHERE "+strings.Join(conds, " AND ")+
		" ORDER BY t.planned_start_at DESC NULLS LAST, t.created_at DESC LIMIT $"+
		strconv.Itoa(len(args)-1)+" OFFSET $"+strconv.Itoa(len(args)), args...)
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

func (m *Module) createTask(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	var req taskWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.Title == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("title is required"))
		return
	}
	if req.Type == "" {
		req.Type = "other"
	}
	// Reference ownership (ADR-006 hardening).
	if err := tenantref.MustExistAll(r.Context(), m.pool, id.TenantID,
		[2]string{"farm", req.FarmID},
		[2]string{"plot", req.PlotID},
		[2]string{"crop_batch", req.CropBatchID},
		[2]string{"user", req.AssigneeID},
	); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	newID := ulidx.New()
	meta, _ := json.Marshal(req.Metadata)
	_, err := m.pool.Exec(r.Context(), `
		INSERT INTO agricultural_tasks
			(id, tenant_id, farm_id, plot_id, crop_batch_id, type, title, description,
			 priority, status, planned_start_at, planned_end_at, assignee_id, source, metadata, created_by)
		VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),$6,$7,$8,
			COALESCE(NULLIF($9,''),'normal'),'planned',$10,$11,NULLIF($12,''),'manual',to_jsonb(COALESCE($13::jsonb,'{}'::jsonb)),$14)`,
		newID, id.TenantID, req.FarmID, req.PlotID, req.CropBatchID, req.Type, req.Title, req.Description,
		req.Priority, req.PlannedStartAt, req.PlannedEndAt, req.AssigneeID, string(meta), id.UserID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	m.pub.Publish(id.TenantID, "agri.task.updated", map[string]any{"task_id": newID, "status": "planned"})
	httpx.Created(w, r, m.getTaskMap(r, id.TenantID, newID))
}

func (m *Module) getTask(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	item := m.getTaskMap(r, id.TenantID, chi.URLParam(r, "id"))
	if item == nil {
		httpx.Respond(w, r, nil, apierr.NotFound("task"))
		return
	}
	httpx.Respond(w, r, item, nil)
}

func (m *Module) getTaskMap(r *http.Request, tenantID, taskID string) map[string]any {
	row, err := m.pool.Query(r.Context(), taskSelect+" WHERE t.tenant_id=$1 AND t.id=$2", tenantID, taskID)
	if err != nil {
		return nil
	}
	item, err := pgx.CollectOneRow(row, pgx.RowToMap)
	row.Close()
	if err != nil {
		return nil
	}
	return item
}

func (m *Module) updateTask(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	taskID := chi.URLParam(r, "id")
	var req taskWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	sets, args := []string{}, []any{}
	addSet := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, col+" = $"+strconv.Itoa(len(args)))
	}
	if req.Title != "" {
		addSet("title", req.Title)
	}
	if req.Type != "" {
		addSet("type", req.Type)
	}
	if req.Description != "" {
		addSet("description", req.Description)
	}
	if req.Priority != "" {
		addSet("priority", req.Priority)
	}
	if req.PlotID != "" {
		addSet("plot_id", req.PlotID)
	}
	if req.CropBatchID != "" {
		addSet("crop_batch_id", req.CropBatchID)
	}
	if req.AssigneeID != "" {
		addSet("assignee_id", req.AssigneeID)
	}
	if req.PlannedStartAt != nil {
		addSet("planned_start_at", *req.PlannedStartAt)
	}
	if req.PlannedEndAt != nil {
		addSet("planned_end_at", *req.PlannedEndAt)
	}
	if len(sets) > 0 {
		args = append(args, id.TenantID, taskID)
		tenantIdx, idIdx := len(args)-1, len(args)
		ct, err := m.pool.Exec(r.Context(), "UPDATE agricultural_tasks SET "+strings.Join(sets, ", ")+
			", updated_by = $"+strconv.Itoa(len(args)+1)+
			" WHERE tenant_id = $"+strconv.Itoa(tenantIdx)+" AND id = $"+strconv.Itoa(idIdx),
			append(args, id.UserID)...)
		if err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
		if ct.RowsAffected() == 0 {
			httpx.Respond(w, r, nil, apierr.NotFound("task"))
			return
		}
	}
	m.pub.Publish(id.TenantID, "agri.task.updated", map[string]any{"task_id": taskID})
	httpx.Respond(w, r, m.getTaskMap(r, id.TenantID, taskID), nil)
}

func (m *Module) deleteTask(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	ct, err := m.pool.Exec(r.Context(),
		`DELETE FROM agricultural_tasks WHERE tenant_id=$1 AND id=$2 AND status NOT IN ('completed')`,
		id.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.NotFound("task (or already completed)"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Module) startTask(w http.ResponseWriter, r *http.Request) {
	m.transitionTask(w, r, "in_progress", `status = 'planned' OR status = 'in_progress'`)
}

func (m *Module) transitionTask(w http.ResponseWriter, r *http.Request, status, allowed string) {
	id, _ := ctxid.Must(r.Context())
	taskID := chi.URLParam(r, "id")
	updates := []string{"status = '" + status + "'"}
	if status == "in_progress" {
		updates = append(updates, "started_at = COALESCE(started_at, now())")
	}
	if status == "completed" {
		updates = append(updates, "completed_at = now()")
	}
	ct, err := m.pool.Exec(r.Context(),
		`UPDATE agricultural_tasks SET `+strings.Join(updates, ", ")+` WHERE tenant_id=$1 AND id=$2 AND (`+allowed+`)`,
		id.TenantID, taskID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.Conflict("task cannot transition to %s", status))
		return
	}
	m.pub.Publish(id.TenantID, "agri.task.updated", map[string]any{"task_id": taskID, "status": status})
	httpx.Respond(w, r, m.getTaskMap(r, id.TenantID, taskID), nil)
}

type taskRecordWrite struct {
	Note      string  `json:"note"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Media     []any   `json:"media"`
}

// completeTask closes the task, writes a task record and appends a TraceEvent
// (specs/02.3: all production activities become trace events).
func (m *Module) completeTask(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	taskID := chi.URLParam(r, "id")
	var req taskRecordWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}

	var taskType, title, plotID, cropBatchID, farmID string
	err := m.pool.QueryRow(r.Context(), `
		SELECT type, title, COALESCE(plot_id::text,''), COALESCE(crop_batch_id::text,''), COALESCE(farm_id::text,'')
		FROM agricultural_tasks WHERE tenant_id=$1 AND id=$2`, id.TenantID, taskID).
		Scan(&taskType, &title, &plotID, &cropBatchID, &farmID)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("task"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}

	var loc any
	if req.Latitude != 0 || req.Longitude != 0 {
		loc = "POINT(" + strconv.FormatFloat(req.Longitude, 'f', 6, 64) + " " +
			strconv.FormatFloat(req.Latitude, 'f', 6, 64) + ")"
	}
	media, _ := json.Marshal(req.Media)
	recordID := ulidx.New()
	if _, err := m.pool.Exec(r.Context(), `
		INSERT INTO agricultural_task_records (id, tenant_id, task_id, operator_id, note, location, finished_at, media)
		VALUES ($1,$2,$3,$4,$5,COALESCE($6::geometry,NULL),now(),to_jsonb(COALESCE($7::jsonb,'[]'::jsonb)))`,
		recordID, id.TenantID, taskID, id.UserID, req.Note, loc, string(media)); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}

	ct, err := m.pool.Exec(r.Context(),
		`UPDATE agricultural_tasks SET status='completed', completed_at=now(), updated_by=$3
		 WHERE tenant_id=$1 AND id=$2 AND status IN ('planned','in_progress')`, id.TenantID, taskID, id.UserID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.Conflict("task already completed or cancelled"))
		return
	}

	traceMeta := map[string]any{"record_id": recordID}
	if _, err := m.traceSvc.Append(r.Context(), id.TenantID, trace.EventInput{
		EventType:   traceTypeForTask(taskType),
		Title:       title,
		Description: req.Note,
		FarmID:      farmID,
		PlotID:      plotID,
		CropBatchID: cropBatchID,
		RefType:     "task",
		RefID:       taskID,
		TaskID:      taskID,
		OperatorID:  id.UserID,
		Media:       json.RawMessage(media),
		Metadata:    traceMeta,
	}); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}

	m.pub.Publish(id.TenantID, "agri.task.updated", map[string]any{"task_id": taskID, "status": "completed"})
	httpx.Respond(w, r, map[string]any{
		"task": m.getTaskMap(r, id.TenantID, taskID), "record_id": recordID,
	}, nil)
}

func traceTypeForTask(taskType string) string {
	switch taskType {
	case "tillage", "fertilize", "spray", "irrigate", "weed", "prune", "inspect", "harvest":
		return taskType
	default:
		return "note"
	}
}

func (m *Module) listTaskRecords(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	rows, err := m.pool.Query(r.Context(), `
		SELECT rec.id, rec.task_id, rec.operator_id, u.display_name AS operator_name, rec.note,
			ST_AsGeoJSON(rec.location)::jsonb AS location, rec.started_at, rec.finished_at, rec.media, rec.created_at
		FROM agricultural_task_records rec
		LEFT JOIN users u ON u.id = rec.operator_id
		WHERE rec.tenant_id=$1 AND rec.task_id=$2 ORDER BY rec.created_at DESC`,
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

func (m *Module) createTaskRecord(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	taskID := chi.URLParam(r, "id")
	var req taskRecordWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if err := tenantref.MustExist(r.Context(), m.pool, id.TenantID, "task", taskID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	var loc any
	if req.Latitude != 0 || req.Longitude != 0 {
		loc = "POINT(" + strconv.FormatFloat(req.Longitude, 'f', 6, 64) + " " +
			strconv.FormatFloat(req.Latitude, 'f', 6, 64) + ")"
	}
	media, _ := json.Marshal(req.Media)
	newID := ulidx.New()
	if _, err := m.pool.Exec(r.Context(), `
		INSERT INTO agricultural_task_records (id, tenant_id, task_id, operator_id, note, location, media)
		VALUES ($1,$2,$3,$4,$5,COALESCE($6::geometry,NULL),to_jsonb(COALESCE($7::jsonb,'[]'::jsonb)))`,
		newID, id.TenantID, taskID, id.UserID, req.Note, loc, string(media)); err != nil {
		httpx.Respond(w, r, nil, apierr.Invalid("invalid record (task not found?)"))
		return
	}
	httpx.Created(w, r, map[string]any{"id": newID})
}

// ---------- input usages ----------

type inputUsageWrite struct {
	TaskID       string  `json:"task_id"`
	InputBatchID string  `json:"input_batch_id"`
	Quantity     float64 `json:"quantity"`
	Note         string  `json:"note"`
}

func (m *Module) listInputUsages(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	args := []any{id.TenantID}
	conds := []string{"us.tenant_id = $1"}
	if v := r.URL.Query().Get("task_id"); v != "" {
		args = append(args, v)
		conds = append(conds, "us.task_id = $"+strconv.Itoa(len(args)))
	}
	if v := r.URL.Query().Get("input_batch_id"); v != "" {
		args = append(args, v)
		conds = append(conds, "us.input_batch_id = $"+strconv.Itoa(len(args)))
	}
	var total int64
	if err := m.pool.QueryRow(r.Context(),
		"SELECT count(*) FROM input_usages us WHERE "+strings.Join(conds, " AND "), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := m.pool.Query(r.Context(), `
		SELECT us.id, us.tenant_id, us.task_id, us.input_batch_id, ib.code AS input_batch_code,
			i.name AS input_name, i.category, us.quantity::float8 AS quantity, us.unit,
			us.applied_by, us.applied_at, us.note, us.created_at
		FROM input_usages us
		LEFT JOIN input_batches ib ON ib.id = us.input_batch_id
		LEFT JOIN inputs i ON i.id = ib.input_id
		WHERE `+strings.Join(conds, " AND ")+` ORDER BY us.applied_at DESC
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
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

func (m *Module) createInputUsage(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	var req inputUsageWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.TaskID == "" || req.InputBatchID == "" || req.Quantity <= 0 {
		httpx.Respond(w, r, nil, apierr.Invalid("task_id, input_batch_id and positive quantity are required"))
		return
	}
	if err := tenantref.MustExist(r.Context(), m.pool, id.TenantID, "task", req.TaskID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	newID := ulidx.New()
	err := m.pool.QueryRow(r.Context(), `
		WITH ib AS (
			SELECT id, quantity, unit FROM input_batches WHERE tenant_id=$1 AND id=$2 FOR UPDATE
		), ins AS (
			INSERT INTO input_usages (id, tenant_id, task_id, input_batch_id, quantity, unit, applied_by, note)
			SELECT $3, $1, $4, ib.id, $5, ib.unit, $6, $7 FROM ib
			WHERE ib.quantity >= $5
			RETURNING id, unit
		), upd AS (
			UPDATE input_batches SET quantity = quantity - $5, status = CASE WHEN quantity - $5 <= 0 THEN 'used' ELSE status END
			WHERE tenant_id=$1 AND id=$2 AND quantity >= $5
			RETURNING 1
		)
		SELECT id, unit FROM ins`, id.TenantID, req.InputBatchID, newID, req.TaskID, req.Quantity, id.UserID, req.Note).
		Scan(&newID, new(any))
	if err != nil {
		httpx.Respond(w, r, nil, apierr.Invalid("usage rejected: task or batch missing, or insufficient stock"))
		return
	}
	httpx.Created(w, r, map[string]any{"id": newID})
}

// ---------- geojson + dictionary ----------

func (m *Module) farmsGeoJSON(w http.ResponseWriter, r *http.Request) {
	m.geoJSON(w, r, "farms")
}

func (m *Module) plotsGeoJSON(w http.ResponseWriter, r *http.Request) {
	m.geoJSON(w, r, "plots")
}

func (m *Module) geoJSON(w http.ResponseWriter, r *http.Request, table string) {
	id, _ := ctxid.Must(r.Context())
	geomCol := "boundary"
	rows, err := m.pool.Query(r.Context(), `
		SELECT id, name, code, ST_AsGeoJSON(`+geomCol+`)::jsonb AS geometry
		FROM `+table+` WHERE tenant_id=$1 AND boundary IS NOT NULL`, id.TenantID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	defer rows.Close()
	features := []map[string]any{}
	for rows.Next() {
		var fid, name, code string
		var geom map[string]any
		if err := rows.Scan(&fid, &name, &code, &geom); err == nil {
			features = append(features, map[string]any{
				"type":       "Feature",
				"id":         fid,
				"geometry":   geom,
				"properties": map[string]any{"name": name, "code": code},
			})
		}
	}
	httpx.Respond(w, r, map[string]any{
		"type":     "FeatureCollection",
		"features": features,
	}, nil)
}

func (m *Module) listDiseasePests(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	category := r.URL.Query().Get("category")
	rows, err := m.pool.Query(r.Context(), `
		SELECT id, COALESCE(tenant_id::text,'') AS tenant_id, category, name_zh, name_en, scientific_name, description
		FROM disease_pests WHERE tenant_id IS NULL OR tenant_id = $1
		ORDER BY category, name_zh`, id.TenantID)
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
	if category != "" {
		filtered := items[:0]
		for _, it := range items {
			if it["category"] == category {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}
	httpx.Respond(w, r, items, nil)
}

func parseTimeParam(v string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
