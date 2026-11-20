package trace

import (
	"context"
	"net/http"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
)

// traceGraph builds the upstream chain for a business object:
// Package -> ProductBatch -> ProcessingBatch -> RawMaterialBatch -> Harvest
// -> CropBatch -> Plot, plus trace event counts per node (specs/05.9).
func (api *API) traceGraph(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	q := r.URL.Query()
	refType, refID := q.Get("ref_type"), q.Get("ref_id")
	if refType == "" || refID == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("ref_type and ref_id are required"))
		return
	}
	ctx := r.Context()

	nodes := []map[string]any{}
	edges := []map[string]any{}
	seen := map[string]bool{}
	curType, curID := refType, refID

	for i := 0; i < 12 && curType != "" && curID != ""; i++ {
		key := curType + ":" + curID
		if seen[key] {
			break
		}
		seen[key] = true
		node := map[string]any{"type": curType, "id": curID}
		var count int64
		_ = api.svc.pool.QueryRow(ctx,
			`SELECT count(*) FROM trace_events WHERE tenant_id=$1 AND ref_type=$2 AND ref_id=$3`,
			id.TenantID, curType, curID).Scan(&count)
		node["event_count"] = count
		node["label"], node["detail"] = api.labelFor(ctx, id.TenantID, curType, curID)
		nodes = append(nodes, node)

		parentType, parentID := api.parentOf(ctx, id.TenantID, curType, curID)
		if parentType != "" && parentID != "" {
			edges = append(edges, map[string]any{"source": key, "target": parentType + ":" + parentID})
		}
		curType, curID = parentType, parentID
	}

	httpx.Respond(w, r, map[string]any{"nodes": nodes, "edges": edges}, nil)
}

// parentOf returns the upstream node in the batch chain, if any.
func (api *API) parentOf(ctx context.Context, tenantID, refType, refID string) (string, string) {
	const q = `SELECT $1, $2`
	var parentType, parentID string
	var ok bool
	switch refType {
	case "package":
		err := api.svc.pool.QueryRow(ctx,
			`SELECT 'product_batch', COALESCE(product_batch_id::text,'') FROM packages WHERE tenant_id=$1 AND id=$2`,
			tenantID, refID).Scan(&parentType, &parentID)
		ok = err == nil
	case "product_batch":
		err := api.svc.pool.QueryRow(ctx,
			`SELECT 'processing_batch', COALESCE(processing_batch_id::text,'') FROM product_batches WHERE tenant_id=$1 AND id=$2`,
			tenantID, refID).Scan(&parentType, &parentID)
		ok = err == nil
	case "processing_batch":
		err := api.svc.pool.QueryRow(ctx,
			`SELECT 'raw_material_batch', COALESCE(raw_material_batch_id::text,'') FROM processing_batches WHERE tenant_id=$1 AND id=$2`,
			tenantID, refID).Scan(&parentType, &parentID)
		ok = err == nil
	case "raw_material_batch":
		err := api.svc.pool.QueryRow(ctx,
			`SELECT 'harvest', COALESCE(harvest_id::text,'') FROM raw_material_batches WHERE tenant_id=$1 AND id=$2`,
			tenantID, refID).Scan(&parentType, &parentID)
		ok = err == nil
	case "harvest":
		err := api.svc.pool.QueryRow(ctx,
			`SELECT 'crop_batch', COALESCE(crop_batch_id::text,'') FROM harvests WHERE tenant_id=$1 AND id=$2`,
			tenantID, refID).Scan(&parentType, &parentID)
		ok = err == nil
	case "crop_batch":
		err := api.svc.pool.QueryRow(ctx,
			`SELECT 'plot', COALESCE(plot_id::text,'') FROM crop_batches WHERE tenant_id=$1 AND id=$2`,
			tenantID, refID).Scan(&parentType, &parentID)
		ok = err == nil
	}
	if !ok {
		return "", ""
	}
	return parentType, parentID
}

// labelFor resolves a human-readable label for a chain node.
func (api *API) labelFor(ctx context.Context, tenantID, refType, refID string) (string, map[string]any) {
	type labelQuery struct {
		sql  string
		kind string
	}
	queries := map[string]labelQuery{
		"plot":               {`SELECT name, code FROM plots WHERE tenant_id=$1 AND id=$2`, "plot"},
		"crop_batch":         {`SELECT name, COALESCE(code,'') FROM crop_batches WHERE tenant_id=$1 AND id=$2`, "crop batch"},
		"harvest":            {`SELECT to_char(harvested_at,'YYYY-MM-DD'), quantity::float8::text FROM harvests WHERE tenant_id=$1 AND id=$2`, "harvest"},
		"raw_material_batch": {`SELECT code, quantity::float8::text FROM raw_material_batches WHERE tenant_id=$1 AND id=$2`, "raw material"},
		"processing_batch":   {`SELECT code, process_type FROM processing_batches WHERE tenant_id=$1 AND id=$2`, "processing"},
		"product_batch":      {`SELECT code, quantity::float8::text FROM product_batches WHERE tenant_id=$1 AND id=$2`, "product batch"},
		"package":            {`SELECT code, status FROM packages WHERE tenant_id=$1 AND id=$2`, "package"},
	}
	lq, ok := queries[refType]
	if !ok {
		return refType + " " + refID, nil
	}
	var a, b string
	if err := api.svc.pool.QueryRow(ctx, lq.sql, tenantID, refID).Scan(&a, &b); err != nil {
		return refType + " " + refID, nil
	}
	label := a
	if b != "" && b != a {
		label = a + " (" + b + ")"
	}
	return label, map[string]any{"kind": lq.kind, "value": b}
}
