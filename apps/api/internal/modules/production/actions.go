package production

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/trace"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/tenantref"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

// acceptHarvest converts a harvest into a raw material batch, appending trace
// events for both (harvest + warehouse_in) — the first step of the trace chain.
func (m *Module) acceptHarvest(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	harvestID := chi.URLParam(r, "id")
	var req struct {
		Code string `json:"code"`
		Note string `json:"note"`
	}
	_ = httpx.DecodeJSON(r, &req) // body optional

	ctx := r.Context()
	var plotID, cropBatchID string
	var quantity float64
	var unit string
	err := m.pool.QueryRow(ctx, `
		SELECT COALESCE(plot_id::text,''), COALESCE(crop_batch_id::text,''),
			quantity::float8, COALESCE(unit,'kg')
		FROM harvests WHERE tenant_id=$1 AND id=$2`, id.TenantID, harvestID).
		Scan(&plotID, &cropBatchID, &quantity, &unit)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("harvest"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}

	code := req.Code
	if code == "" {
		code = "RM-" + ulidx.New()[:10]
	}
	rmID := ulidx.New()
	if _, err := m.pool.Exec(ctx, `
		INSERT INTO raw_material_batches (id, tenant_id, code, harvest_id, plot_id, crop_batch_id, quantity, unit, received_at, created_by)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7,$8,now(),$9)`,
		rmID, id.TenantID, code, harvestID, plotID, cropBatchID, quantity, unit, id.UserID); err != nil {
		httpx.Respond(w, r, nil, mapWriteErr(err))
		return
	}

	// Trace events: harvest reference + raw material receipt.
	if _, err := m.traceSvc.Append(ctx, id.TenantID, traceInput("harvest", harvestID, plotID, cropBatchID,
		"harvest", "Harvest recorded", quantity, unit, id.UserID)); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if _, err := m.traceSvc.Append(ctx, id.TenantID, traceInput("raw_material_batch", rmID, plotID, cropBatchID,
		"warehouse_in", "Raw material batch received", quantity, unit, id.UserID)); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}

	m.pub.Publish(id.TenantID, "trace.created", map[string]any{"ref_type": "raw_material_batch", "ref_id": rmID})
	httpx.Created(w, r, map[string]any{"id": rmID, "code": code})
}

// completeProcessing closes a processing batch and links its output to trace.
func (m *Module) completeProcessing(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	batchID := chi.URLParam(r, "id")
	var req struct {
		OutputQuantity float64 `json:"output_quantity"`
		Note           string  `json:"note"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	ctx := r.Context()
	var rmID, plotID, cropBatchID, unit string
	var inQty float64
	err := m.pool.QueryRow(ctx, `
		SELECT COALESCE(pr.raw_material_batch_id::text,''), COALESCE(rm.plot_id::text,''), COALESCE(rm.crop_batch_id::text,''),
			pr.input_quantity::float8, COALESCE(pr.unit,'kg')
		FROM processing_batches pr
		LEFT JOIN raw_material_batches rm ON rm.id = pr.raw_material_batch_id
		WHERE pr.tenant_id=$1 AND pr.id=$2`, id.TenantID, batchID).
		Scan(&rmID, &plotID, &cropBatchID, &inQty, &unit)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("processing batch"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.OutputQuantity <= 0 {
		req.OutputQuantity = inQty
	}
	ct, err := m.pool.Exec(ctx, `
		UPDATE processing_batches SET status='completed', output_quantity=$3, finished_at=now(), updated_by=$4
		WHERE tenant_id=$1 AND id=$2 AND status='in_progress'`, id.TenantID, batchID, req.OutputQuantity, id.UserID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.Conflict("processing batch is not in progress"))
		return
	}
	if _, err := m.traceSvc.Append(ctx, id.TenantID, traceInput("processing_batch", batchID, plotID, cropBatchID,
		"processing", "Processing completed", req.OutputQuantity, unit, id.UserID)); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	_, _ = m.pool.Exec(ctx, `UPDATE raw_material_batches SET status='consumed' WHERE tenant_id=$1 AND id=$2`, id.TenantID, rmID)
	httpx.Respond(w, r, map[string]any{"id": batchID, "status": "completed", "output_quantity": req.OutputQuantity}, nil)
}

// traceInput builds a trace EventInput for production steps.
func traceInput(refType, refID, plotID, cropBatchID, eventType, title string, quantity float64, unit, operatorID string) trace.EventInput {
	q := quantity
	return trace.EventInput{
		EventType:   eventType,
		Title:       title,
		Description: title,
		PlotID:      plotID,
		CropBatchID: cropBatchID,
		RefType:     refType,
		RefID:       refID,
		OperatorID:  operatorID,
		Quantity:    &q,
		Unit:        unit,
	}
}

func (m *Module) listInventory(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	args := []any{id.TenantID}
	conds := []string{"iv.tenant_id = $1"}
	if v := r.URL.Query().Get("warehouse_id"); v != "" {
		args = append(args, v)
		conds = append(conds, "iv.warehouse_id = $"+strconv.Itoa(len(args)))
	}
	if v := r.URL.Query().Get("ref_type"); v != "" {
		args = append(args, v)
		conds = append(conds, "iv.ref_type = $"+strconv.Itoa(len(args)))
	}
	var total int64
	if err := m.pool.QueryRow(r.Context(),
		"SELECT count(*) FROM inventory_items iv WHERE "+strings.Join(conds, " AND "), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := m.pool.Query(r.Context(), `
		SELECT iv.id, iv.tenant_id, iv.warehouse_id, w.name AS warehouse_name,
			iv.ref_type, iv.ref_id, iv.quantity::float8 AS quantity, iv.unit, iv.updated_at
		FROM inventory_items iv
		LEFT JOIN warehouses w ON w.id = iv.warehouse_id
		WHERE `+strings.Join(conds, " AND ")+` ORDER BY iv.updated_at DESC
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

func (m *Module) createMovement(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	var req struct {
		WarehouseID string  `json:"warehouse_id"`
		RefType     string  `json:"ref_type"`
		RefID       string  `json:"ref_id"`
		Direction   string  `json:"direction"` // in | out | adjust
		Quantity    float64 `json:"quantity"`
		Unit        string  `json:"unit"`
		Reason      string  `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.WarehouseID == "" || req.RefType == "" || req.RefID == "" || req.Quantity <= 0 {
		httpx.Respond(w, r, nil, apierr.Invalid("warehouse_id, ref_type, ref_id and positive quantity are required"))
		return
	}
	switch req.Direction {
	case "in", "out", "adjust":
	default:
		httpx.Respond(w, r, nil, apierr.Invalid("direction must be in|out|adjust"))
		return
	}
	if err := tenantref.MustExist(r.Context(), m.pool, id.TenantID, "warehouse", req.WarehouseID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	ctx := r.Context()
	newID := ulidx.New()
	if _, err := m.pool.Exec(ctx, `
		INSERT INTO inventory_movements (id, tenant_id, warehouse_id, ref_type, ref_id, direction, quantity, unit, reason, operator_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,COALESCE(NULLIF($8,''),'kg'),$9,$10)`,
		newID, id.TenantID, req.WarehouseID, req.RefType, req.RefID, req.Direction,
		req.Quantity, req.Unit, req.Reason, id.UserID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	delta := req.Quantity
	if req.Direction == "out" {
		delta = -req.Quantity
	}
	if _, err := m.pool.Exec(ctx, `
		INSERT INTO inventory_items (id, tenant_id, warehouse_id, ref_type, ref_id, quantity, unit)
		VALUES ($1,$2,$3,$4,$5,$6,COALESCE(NULLIF($7,''),'kg'))
		ON CONFLICT (warehouse_id, ref_type, ref_id)
		DO UPDATE SET quantity = inventory_items.quantity + EXCLUDED.quantity, updated_at = now()`,
		ulidx.New(), id.TenantID, req.WarehouseID, req.RefType, req.RefID, delta, req.Unit); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Created(w, r, map[string]any{"id": newID})
}

func mapWriteErr(err error) error {
	if strings.Contains(err.Error(), "duplicate key") {
		return apierr.Conflict("duplicate code")
	}
	return err
}
