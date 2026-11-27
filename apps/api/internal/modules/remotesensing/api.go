// Remote-sensing REST API: job management and STAC-lite catalog.
package remotesensing

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

// API exposes /remote-sensing/jobs and /stac endpoints.
type API struct {
	pool *pgxpool.Pool
}

func NewAPI(pool *pgxpool.Pool) *API { return &API{pool: pool} }

// Mount registers routes (authorize wraps permission middleware).
func (api *API) Mount(r chi.Router, authorize func(resource, action string) func(http.Handler) http.Handler) {
	r.Route("/remote-sensing/jobs", func(rr chi.Router) {
		rr.Use(authorize("remote-sensing", "read"))
		rr.Get("/", api.listJobs)
		rr.With(authorize("remote-sensing", "create")).Post("/", api.createJob)
		rr.Get("/{id}", api.getJob)
		rr.With(authorize("remote-sensing", "update")).Post("/{id}/cancel", api.cancelJob)
	})
	r.Route("/stac", func(rr chi.Router) {
		rr.Use(authorize("remote-sensing", "read"))
		rr.Get("/collections", api.listCollections)
		rr.Get("/collections/{collection}/items", api.listItems)
		rr.Get("/collections/{collection}/items/{id}", api.getItem)
		rr.With(authorize("remote-sensing", "create")).Post("/collections/{collection}/items", api.importItem)
	})
}

// ---------- jobs ----------

const jobSelect = `SELECT j.id, j.tenant_id, j.mission_id, j.job_type, j.status, j.progress,
	j.processor, j.external_task_id, j.inputs, j.outputs, j.error, j.created_at, j.updated_at,
	j.started_at, j.finished_at
	FROM remote_sensing_jobs j`

func (api *API) listJobs(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	pp := httpx.GetPageParams(r)
	args := []any{id.TenantID}
	conds := []string{"j.tenant_id = $1"}
	if v := r.URL.Query().Get("status"); v != "" {
		args = append(args, v)
		conds = append(conds, "j.status = $"+itoa(len(args)))
	}
	if v := r.URL.Query().Get("mission_id"); v != "" {
		args = append(args, v)
		conds = append(conds, "j.mission_id = $"+itoa(len(args)))
	}
	var total int64
	if err := api.pool.QueryRow(r.Context(),
		"SELECT count(*) FROM remote_sensing_jobs j WHERE "+joinAnd(conds), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := api.pool.Query(r.Context(), jobSelect+" WHERE "+joinAnd(conds)+
		" ORDER BY j.created_at DESC LIMIT $"+itoa(len(args)-1)+" OFFSET $"+itoa(len(args)), args...)
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

type jobCreate struct {
	JobType   string   `json:"job_type"`
	MissionID string   `json:"mission_id"`
	Processor string   `json:"processor"`
	FileIDs   []string `json:"file_ids"`
}

func (api *API) createJob(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	var req jobCreate
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	switch req.JobType {
	case "orthophoto", "ndvi", "dem":
	default:
		httpx.Respond(w, r, nil, apierr.Invalid("job_type must be orthophoto|ndvi|dem"))
		return
	}
	if len(req.FileIDs) == 0 || len(req.FileIDs) > 50 {
		httpx.Respond(w, r, nil, apierr.Invalid("1..50 uploaded file_ids are required (upload via POST /files)"))
		return
	}
	if req.MissionID != "" {
		if err := tenantrefExists(r, api.pool, id.TenantID, "mission", req.MissionID); err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
	}
	// Every referenced file must belong to the caller's tenant (ADR-006).
	for _, fid := range req.FileIDs {
		if err := tenantrefExists(r, api.pool, id.TenantID, "file", fid); err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
	}
	processor := req.Processor
	if processor == "" {
		processor = "mock"
	}
	inputs := make([]JobInput, 0, len(req.FileIDs))
	for _, fid := range req.FileIDs {
		var filename string
		if err := api.pool.QueryRow(r.Context(),
			`SELECT filename FROM files WHERE tenant_id=$1 AND id=$2`,
			id.TenantID, fid).Scan(&filename); err != nil {
			httpx.Respond(w, r, nil, apierr.Invalid("file %s not found in your tenant", fid))
			return
		}
		inputs = append(inputs, JobInput{FileID: fid, Filename: filename})
	}
	inputsJSON, _ := json.Marshal(inputs)
	newID := ulidx.New()
	if _, err := api.pool.Exec(r.Context(), `
		INSERT INTO remote_sensing_jobs (id, tenant_id, mission_id, job_type, status, processor, inputs, created_by)
		VALUES ($1,$2,NULLIF($3,''),$4,'queued',$5,to_jsonb($6::jsonb),$7)`,
		newID, id.TenantID, req.MissionID, req.JobType, processor, string(inputsJSON), id.UserID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	// NOTE: mission_id ownership is validated above; jobs reference it loosely.
	httpx.Created(w, r, map[string]any{"id": newID, "status": "queued"})
}

func (api *API) getJob(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	rows, err := api.pool.Query(r.Context(), jobSelect+" WHERE j.tenant_id=$1 AND j.id=$2",
		id.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	item, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	rows.Close()
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("remote sensing job"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, item, nil)
}

func (api *API) cancelJob(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	ct, err := api.pool.Exec(r.Context(), `
		UPDATE remote_sensing_jobs SET status='cancelled', finished_at=now(), updated_at=now()
		WHERE tenant_id=$1 AND id=$2 AND status IN ('queued','running')`,
		id.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.Conflict("job not cancellable (missing or terminal)"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- STAC-lite catalog ----------

func (api *API) listCollections(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	rows, err := api.pool.Query(r.Context(), `
		SELECT collection_id, count(*) AS items, max(created_at) AS last_created
		FROM stac_items WHERE tenant_id=$1 GROUP BY collection_id ORDER BY collection_id`,
		id.TenantID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	defer rows.Close()
	collections := []map[string]any{}
	for rows.Next() {
		var cid string
		var items int64
		var last time.Time
		if err := rows.Scan(&cid, &items, &last); err == nil {
			collections = append(collections, map[string]any{
				"id":           cid,
				"stac_version": "1.0.0",
				"description":  "Remote sensing assets (" + cid + ")",
				"item_count":   items,
				"last_created": last,
			})
		}
	}
	httpx.Respond(w, r, map[string]any{"collections": collections}, nil)
}

func (api *API) listItems(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	pp := httpx.GetPageParams(r)
	collection := chi.URLParam(r, "collection")
	var total int64
	if err := api.pool.QueryRow(r.Context(),
		`SELECT count(*) FROM stac_items WHERE tenant_id=$1 AND collection_id=$2`,
		id.TenantID, collection).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	rows, err := api.pool.Query(r.Context(), `
		SELECT id, collection_id, item, mission_id, job_id, created_at
		FROM stac_items WHERE tenant_id=$1 AND collection_id=$2
		ORDER BY created_at DESC LIMIT $3 OFFSET $4`,
		id.TenantID, collection, pp.PageSize, pp.Offset())
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

func (api *API) getItem(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	rows, err := api.pool.Query(r.Context(), `
		SELECT id, collection_id, item, mission_id, job_id, created_at
		FROM stac_items WHERE tenant_id=$1 AND collection_id=$2 AND id=$3`,
		id.TenantID, chi.URLParam(r, "collection"), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	item, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	rows.Close()
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("stac item"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, item, nil)
}

// importItem registers an externally produced asset (e.g. a GeoTIFF uploaded
// via /files) as a STAC item — the manual path for orthophotos produced
// outside the platform (specs/04.10).
func (api *API) importItem(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	collection := chi.URLParam(r, "collection")
	var req struct {
		FileID    string    `json:"file_id"`
		Title     string    `json:"title"`
		Lat       float64   `json:"lat"`
		Lng       float64   `json:"lng"`
		BBox      []float64 `json:"bbox"` // [minLng, minLat, maxLng, maxLat] -> Polygon footprint
		MissionID string    `json:"mission_id"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	var filename, fileID string
	err = api.pool.QueryRow(r.Context(),
		`SELECT filename, id FROM files WHERE tenant_id=$1 AND id=$2`,
		id.TenantID, req.FileID).Scan(&filename, &fileID)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("file"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	itemID := ulidx.New()
	title := req.Title
	if title == "" {
		title = filename
	}
	item := map[string]any{
		"stac_version": "1.0.0",
		"type":         "Feature",
		"id":           itemID,
		"properties": map[string]any{
			"datetime": time.Now().UTC().Format(time.RFC3339),
			"title":    title,
		},
		"assets": map[string]any{
			"data": map[string]any{
				"href":  "/api/v1/files/" + fileID + "/content",
				"title": title,
			},
		},
	}
	switch {
	case len(req.BBox) == 4:
		// Polygon footprint from bbox (MapLibre overlay / STAC standard shape).
		minLng, minLat, maxLng, maxLat := req.BBox[0], req.BBox[1], req.BBox[2], req.BBox[3]
		item["geometry"] = map[string]any{
			"type":        "Polygon",
			"coordinates": [][][]float64{{{minLng, minLat}, {maxLng, minLat}, {maxLng, maxLat}, {minLng, maxLat}, {minLng, minLat}}},
		}
		item["properties"].(map[string]any)["bbox"] = req.BBox
	case req.Lat != 0 || req.Lng != 0:
		item["geometry"] = map[string]any{
			"type":        "Point",
			"coordinates": []float64{req.Lng, req.Lat},
		}
		item["properties"].(map[string]any)["lat"] = req.Lat
		item["properties"].(map[string]any)["lng"] = req.Lng
	}
	itemJSON, _ := json.Marshal(item)
	if _, err := api.pool.Exec(r.Context(), `
		INSERT INTO stac_items (id, tenant_id, collection_id, item, mission_id, created_by)
		VALUES ($1,$2,$3,to_jsonb($4::jsonb),NULLIF($5,''),$6)`,
		itemID, id.TenantID, collection, string(itemJSON), req.MissionID, id.UserID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Created(w, r, map[string]any{"id": itemID, "collection": collection})
}

// ---------- helpers ----------

func tenantrefExists(r *http.Request, pool *pgxpool.Pool, tenantID, refType, refID string) error {
	var n int
	var table string
	switch refType {
	case "mission":
		table = "missions"
	case "file":
		table = "files"
	default:
		return apierr.Invalid("unknown reference type %q", refType)
	}
	err := pool.QueryRow(r.Context(),
		"SELECT count(*) FROM "+table+" WHERE tenant_id=$1 AND id=$2", tenantID, refID).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.Invalid("referenced %s does not exist in your tenant", refType)
	}
	return nil
}

func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}

func itoa(n int) string { return strconv.Itoa(n) }
