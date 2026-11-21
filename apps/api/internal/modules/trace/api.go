package trace

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/geo"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/tenantref"
)

// API exposes the trace module REST endpoints.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

// Mount registers /trace-events, /trace-codes, /trace-graph under an authorized router.
func (api *API) Mount(r chi.Router) {
	r.Route("/trace-events", func(rr chi.Router) {
		rr.Get("/", api.listEvents)
		rr.Post("/", api.createEvent)
		rr.Get("/{id}", api.getEvent)
		rr.Post("/verify-chain", api.verifyChain)
	})
	r.Route("/trace-codes", func(rr chi.Router) {
		rr.Get("/", api.listCodes)
		rr.Post("/", api.createCode)
		rr.Get("/{code}", api.getCode)
	})
	r.Get("/trace-graph", api.traceGraph)
}

type eventWrite struct {
	EventType   string     `json:"event_type"`
	EventTime   *time.Time `json:"event_time"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	FarmID      string     `json:"farm_id"`
	PlotID      string     `json:"plot_id"`
	CropBatchID string     `json:"crop_batch_id"`
	RefType     string     `json:"ref_type"`
	RefID       string     `json:"ref_id"`
	TaskID      string     `json:"task_id"`
	DeviceID    string     `json:"device_id"`
	MissionID   string     `json:"mission_id"`
	OperatorID  string     `json:"operator_id"`
	Location    *struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	} `json:"location"`
	Quantity *float64       `json:"quantity"`
	Unit     string         `json:"unit"`
	Inputs   any            `json:"inputs"`
	Media    any            `json:"media"`
	Weather  map[string]any `json:"weather"`
	Metadata map[string]any `json:"metadata"`
}

func (api *API) listEvents(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	pp := httpx.GetPageParams(r)
	q := r.URL.Query()
	args := []any{id.TenantID}
	conds := []string{"tenant_id = $1"}
	addCond := func(col string, op string, val any) {
		args = append(args, val)
		conds = append(conds, col+" "+op+" $"+itoa(len(args)))
	}
	if v := q.Get("ref_type"); v != "" {
		addCond("ref_type", "=", v)
	}
	if v := q.Get("ref_id"); v != "" {
		addCond("ref_id", "=", v)
	}
	if v := q.Get("crop_batch_id"); v != "" {
		addCond("crop_batch_id", "=", v)
	}
	if v := q.Get("plot_id"); v != "" {
		addCond("plot_id", "=", v)
	}
	if v := q.Get("device_id"); v != "" {
		addCond("device_id", "=", v)
	}
	if v := q.Get("event_type"); v != "" {
		addCond("event_type", "=", v)
	}
	if v := q.Get("from"); v != "" {
		if t, ok := parseTime(v); ok {
			addCond("event_time", ">=", t)
		}
	}
	if v := q.Get("to"); v != "" {
		if t, ok := parseTime(v); ok {
			addCond("event_time", "<=", t)
		}
	}
	if v := q.Get("search"); v != "" {
		args = append(args, "%"+strings.ToLower(v)+"%")
		conds = append(conds, "(lower(title) LIKE $"+itoa(len(args))+" OR lower(event_type) LIKE $"+itoa(len(args))+")")
	}

	var total int64
	if err := api.svc.pool.QueryRow(r.Context(),
		"SELECT count(*) FROM trace_events WHERE "+strings.Join(conds, " AND "), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	sql := `SELECT id, tenant_id, event_type, event_time, title, description, farm_id, plot_id,
		crop_batch_id, ref_type, ref_id, task_id, device_id, mission_id, operator_id,
		ST_AsGeoJSON(location)::jsonb AS location, quantity::float8 AS quantity, unit,
		inputs, media, weather, metadata, sequence, prev_hash, hash, created_at
		FROM trace_events WHERE ` + strings.Join(conds, " AND ") +
		` ORDER BY sequence DESC LIMIT $` + itoa(len(args)-1) + ` OFFSET $` + itoa(len(args))
	rows, err := api.svc.pool.Query(r.Context(), sql, args...)
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

func (api *API) getEvent(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	row, err := api.svc.pool.Query(r.Context(), `SELECT id, tenant_id, event_type, event_time, title, description,
		farm_id, plot_id, crop_batch_id, ref_type, ref_id, task_id, device_id, mission_id, operator_id,
		ST_AsGeoJSON(location)::jsonb AS location, quantity::float8 AS quantity, unit,
		inputs, media, weather, metadata, sequence, prev_hash, hash, created_at
		FROM trace_events WHERE tenant_id=$1 AND id=$2`, id.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	item, err := pgx.CollectOneRow(row, pgx.RowToMap)
	row.Close()
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("trace event"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, item, nil)
}

func (api *API) createEvent(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	var req eventWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	in := EventInput{
		EventType: req.EventType, Title: req.Title, Description: req.Description,
		FarmID: req.FarmID, PlotID: req.PlotID, CropBatchID: req.CropBatchID,
		RefType: req.RefType, RefID: req.RefID,
		TaskID: req.TaskID, DeviceID: req.DeviceID, MissionID: req.MissionID,
		OperatorID: id.UserID, Quantity: req.Quantity, Unit: req.Unit,
		Inputs: req.Inputs, Media: req.Media, Weather: req.Weather, Metadata: req.Metadata,
	}
	if req.EventTime != nil {
		in.EventTime = *req.EventTime
	}
	if req.Location != nil {
		in.Location = &geo.Point{Lat: req.Location.Lat, Lng: req.Location.Lng}
	}
	// Reference ownership: farm/plot/batch/task/device/mission must belong to
	// the caller's tenant (ADR-006 hardening).
	if err := tenantref.MustExistAll(r.Context(), api.svc.pool, id.TenantID,
		[2]string{"farm", req.FarmID},
		[2]string{"plot", req.PlotID},
		[2]string{"crop_batch", req.CropBatchID},
		[2]string{"task", req.TaskID},
		[2]string{"device", req.DeviceID},
		[2]string{"mission", req.MissionID},
	); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	created, err := api.svc.Append(r.Context(), id.TenantID, in)
	if err != nil {
		httpx.Respond(w, r, nil, apierr.Invalid("%v", err))
		return
	}
	httpx.Created(w, r, created)
}

// verifyChain recomputes the per-tenant hash chain and reports the first break.
func (api *API) verifyChain(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	rows, err := api.svc.pool.Query(r.Context(), `SELECT id, sequence, prev_hash, hash, event_type,
		title, description, COALESCE(farm_id,''), COALESCE(plot_id,''), COALESCE(crop_batch_id,''),
		COALESCE(ref_type,''), COALESCE(ref_id,''), COALESCE(task_id,''), COALESCE(device_id,''), COALESCE(mission_id,''),
		COALESCE(operator_id,''), ST_AsGeoJSON(location)::jsonb, quantity::float8, COALESCE(unit,''),
		inputs, media, COALESCE(weather,'{}'::jsonb), COALESCE(metadata,'{}'::jsonb)
		FROM trace_events WHERE tenant_id = $1 ORDER BY sequence`, id.TenantID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	defer rows.Close()

	var prevHash string
	var seq int64
	for rows.Next() {
		var evID, storedPrev, evHash string
		var in EventInput
		var locGeo *map[string]any
		if err := rows.Scan(&evID, &seq, &storedPrev, &evHash, &in.EventType,
			&in.Title, &in.Description, &in.FarmID, &in.PlotID, &in.CropBatchID,
			&in.RefType, &in.RefID, &in.TaskID, &in.DeviceID, &in.MissionID,
			&in.OperatorID, &locGeo, &in.Quantity, &in.Unit,
			&in.Inputs, &in.Media, &in.Weather, &in.Metadata); err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
		if locGeo != nil {
			in.Location = pointFromGeoJSON(*locGeo)
		}
		expect := computeHash(seq, prevHash, id.TenantID, canonicalJSON(in))
		if storedPrev != prevHash || evHash != expect {
			httpx.Respond(w, r, map[string]any{
				"valid":         false,
				"break_at":      map[string]any{"event_id": evID, "sequence": seq},
				"expected_hash": expect,
				"stored_hash":   evHash,
			}, nil)
			return
		}
		prevHash = evHash
	}
	if err := rows.Err(); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, map[string]any{"valid": true, "events": seq}, nil)
}

func pointFromGeoJSON(m map[string]any) *geo.Point {
	coords, _ := m["coordinates"].([]any)
	if len(coords) != 2 {
		return nil
	}
	lng, _ := coords[0].(float64)
	lat, _ := coords[1].(float64)
	return &geo.Point{Lat: lat, Lng: lng}
}

func parseTime(v string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func itoa(n int) string { return strconv.Itoa(n) }
