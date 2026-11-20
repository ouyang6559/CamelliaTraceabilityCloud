package trace

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
)

// MountPublic registers the consumer-facing endpoints (no authentication,
// specs/11.5). Internal management fields are never exposed here.
func (api *API) MountPublic(r chi.Router) {
	r.Get("/public/trace/{code}", api.publicTrace)
	r.Get("/public/trace/{code}/verify", api.publicVerify)
}

func (api *API) publicTrace(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	ctx := r.Context()

	var codeID, codeType, refType, refID, status, tenantID string
	err := api.svc.pool.QueryRow(ctx, `
		SELECT id, code_type, ref_type, ref_id, status, tenant_id FROM trace_codes WHERE code = $1 AND status = 'active'`,
		code).Scan(&codeID, &codeType, &refType, &refID, &status, &tenantID)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("trace code"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}

	// Mark the scan (best effort).
	_, _ = api.svc.pool.Exec(ctx, `
		UPDATE trace_codes SET scan_count = scan_count + 1,
			first_scanned_at = COALESCE(first_scanned_at, now()),
			last_scanned_at = now() WHERE code = $1`, code)

	// Resolve the product/origin summary from whatever the code points to.
	summary := api.consumerSummary(ctx, tenantID, refType, refID)

	// All trace events reachable through the chain from this code.
	events, err := api.chainEvents(ctx, tenantID, refType, refID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}

	evidence, err := api.chainEvidence(ctx, tenantID, refType, refID)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}

	proofs, _ := api.chainProofs(ctx, tenantID, refType, refID)

	httpx.Respond(w, r, map[string]any{
		"code":      code,
		"code_type": codeType,
		"summary":   summary,
		"timeline":  events,
		"evidence":  evidence,
		"proofs":    proofs,
	}, nil)
}

// consumerSummary resolves consumer-facing product/origin info without leaking
// internal IDs or management fields (specs/05.8).
func (api *API) consumerSummary(ctx context.Context, tenantID, refType, refID string) map[string]any {
	out := map[string]any{}
	// Walk upstream to product_batch and plot.
	curType, curID := refType, refID
	var plotID, productBatchID string
	for i := 0; i < 12 && curType != "" && curID != ""; i++ {
		switch curType {
		case "product_batch":
			productBatchID = curID
		case "plot":
			plotID = curID
		}
		parentType, parentID := api.parentOf(ctx, tenantID, curType, curID)
		curType, curID = parentType, parentID
	}
	if productBatchID != "" {
		var productName, productSpec, batchCode string
		if err := api.svc.pool.QueryRow(ctx, `
			SELECT p.name, p.spec, pb.code FROM product_batches pb
			JOIN products p ON p.id = pb.product_id
			WHERE pb.id = $1`, productBatchID).Scan(&productName, &productSpec, &batchCode); err == nil {
			out["product"] = map[string]any{"name": productName, "spec": productSpec, "batch_code": batchCode}
		}
	}
	if plotID != "" {
		var farmName, plotName, address string
		if err := api.svc.pool.QueryRow(ctx, `
			SELECT f.name, p.name, f.address FROM plots p
			JOIN farms f ON f.id = p.farm_id
			WHERE p.id = $1`, plotID).Scan(&farmName, &plotName, &address); err == nil {
			out["origin"] = map[string]any{"farm": farmName, "plot": plotName, "address": address}
		}
	}
	return out
}

// chainEvents collects trace events for the whole upstream chain, oldest first.
func (api *API) chainEvents(ctx context.Context, tenantID, refType, refID string) ([]map[string]any, error) {
	curType, curID := refType, refID
	var all []map[string]any
	for i := 0; i < 12 && curType != "" && curID != ""; i++ {
		rows, err := api.svc.pool.Query(ctx, `
			SELECT event_type, event_time, title, description, quantity::float8 AS quantity, unit, sequence, hash
			FROM trace_events WHERE tenant_id = $1 AND ref_type = $2 AND ref_id = $3 ORDER BY event_time`,
			tenantID, curType, curID)
		if err != nil {
			return nil, err
		}
		items, err := pgx.CollectRows(rows, pgx.RowToMap)
		rows.Close()
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		parentType, parentID := api.parentOf(ctx, tenantID, curType, curID)
		curType, curID = parentType, parentID
	}
	return all, nil
}

// chainEvidence resolves media evidence files for events on the chain.
func (api *API) chainEvidence(ctx context.Context, tenantID, refType, refID string) ([]map[string]any, error) {
	curType, curID := refType, refID
	var all []map[string]any
	for i := 0; i < 12 && curType != "" && curID != ""; i++ {
		rows, err := api.svc.pool.Query(ctx, `
			SELECT e.id, e.evidence_type, e.filename, e.mime, e.sha256, e.captured_at, f.id AS file_id
			FROM evidence e
			LEFT JOIN files f ON f.id = e.file_id
			WHERE e.tenant_id = $1 AND e.ref_type = $2 AND e.ref_id = $3
			ORDER BY e.created_at`, tenantID, curType, curID)
		if err != nil {
			return nil, err
		}
		items, err := pgx.CollectRows(rows, pgx.RowToMap)
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if it["file_id"] != nil {
				it["url"] = "/api/v1/public/trace-media/" + it["file_id"].(string)
			}
			all = append(all, it)
		}
		parentType, parentID := api.parentOf(ctx, tenantID, curType, curID)
		curType, curID = parentType, parentID
	}
	return all, nil
}

func (api *API) chainProofs(ctx context.Context, tenantID, refType, refID string) ([]map[string]any, error) {
	rows, err := api.svc.pool.Query(ctx, `
		SELECT algorithm, hash, provider, provider_tx_id, status, confirmed_at
		FROM blockchain_proofs WHERE tenant_id = $1 AND ref_type = $2 AND ref_id = $3 AND status = 'confirmed'
		ORDER BY created_at`, tenantID, refType, refID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToMap)
}

// publicVerify exposes chain verification for consumers (specs/05.5/05.8).
func (api *API) publicVerify(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	var refType, refID, tenantID string
	err := api.svc.pool.QueryRow(r.Context(),
		`SELECT ref_type, ref_id, tenant_id FROM trace_codes WHERE code=$1 AND status='active'`, code).
		Scan(&refType, &refID, &tenantID)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("trace code"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	proofs, _ := api.chainProofs(r.Context(), tenantID, refType, refID)
	httpx.Respond(w, r, map[string]any{
		"code":   code,
		"valid":  len(proofs) > 0,
		"proofs": proofs,
	}, nil)
}

// MountPublicMedia serves evidence files referenced by a traceable chain
// (authorization = the file must be linked to evidence; no tenant data leaks).
func (api *API) MountPublicMedia(r chi.Router, serveFile func(w http.ResponseWriter, r *http.Request, fileID string)) {
	r.Get("/public/trace-media/{fileID}", func(w http.ResponseWriter, req *http.Request) {
		serveFile(w, req, chi.URLParam(req, "fileID"))
	})
}
