package trace

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

type codeWrite struct {
	CodeType       string     `json:"code_type"` // batch | package | carton | pallet | logistics | consumer
	RefType        string     `json:"ref_type"`
	RefID          string     `json:"ref_id"`
	ProductBatchID string     `json:"product_batch_id"`
	PackageID      string     `json:"package_id"`
	Quantity       int        `json:"quantity"` // number of codes to mint (default 1)
	ExpiresAt      *time.Time `json:"expires_at"`
}

// createCode mints one or more trace codes; the code itself is generated server-side.
func (api *API) createCode(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	var req codeWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.RefType == "" || req.RefID == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("ref_type and ref_id are required"))
		return
	}
	if req.CodeType == "" {
		req.CodeType = "package"
	}
	n := req.Quantity
	if n < 1 {
		n = 1
	}
	if n > 500 {
		httpx.Respond(w, r, nil, apierr.Invalid("quantity must be <= 500 per request"))
		return
	}
	created := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		code := newTraceCode(req.CodeType)
		newID := ulidx.New()
		var expires any
		if req.ExpiresAt != nil {
			expires = *req.ExpiresAt
		}
		row, err := api.svc.pool.Query(r.Context(), `
			INSERT INTO trace_codes (id, tenant_id, code, code_type, ref_type, ref_id, product_batch_id, package_id, expires_at, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),$9,$10)
			RETURNING id, code, code_type, ref_type, ref_id, status, created_at`,
			newID, id.TenantID, code, req.CodeType, req.RefType, req.RefID,
			req.ProductBatchID, req.PackageID, expires, id.UserID)
		if err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
		item, err := pgx.CollectOneRow(row, pgx.RowToMap)
		row.Close()
		if err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
		created = append(created, item)
	}
	httpx.Created(w, r, map[string]any{"items": created, "count": len(created)})
}

// newTraceCode renders CTC-<type>-<random> codes (GS1-friendly, vendor-neutral).
func newTraceCode(codeType string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "CTC-" + codeTypePrefix(codeType) + "-" + hex.EncodeToString(b)
}

func codeTypePrefix(t string) string {
	switch t {
	case "batch":
		return "B"
	case "carton":
		return "C"
	case "pallet":
		return "P"
	case "logistics":
		return "L"
	case "consumer":
		return "U"
	default:
		return "K" // package
	}
}

func (api *API) listCodes(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	pp := httpx.GetPageParams(r)
	q := r.URL.Query()
	args := []any{id.TenantID}
	conds := []string{"tenant_id = $1"}
	if v := q.Get("ref_type"); v != "" {
		args = append(args, v)
		conds = append(conds, "ref_type = $"+itoa(len(args)))
	}
	if v := q.Get("ref_id"); v != "" {
		args = append(args, v)
		conds = append(conds, "ref_id = $"+itoa(len(args)))
	}
	if v := q.Get("code_type"); v != "" {
		args = append(args, v)
		conds = append(conds, "code_type = $"+itoa(len(args)))
	}
	if v := q.Get("search"); v != "" {
		args = append(args, "%"+v+"%")
		conds = append(conds, "code ILIKE $"+itoa(len(args)))
	}
	var total int64
	if err := api.svc.pool.QueryRow(r.Context(),
		"SELECT count(*) FROM trace_codes WHERE "+joinAnd(conds), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := api.svc.pool.Query(r.Context(), `
		SELECT id, code, code_type, ref_type, ref_id, product_batch_id, package_id, status,
			scan_count, first_scanned_at, last_scanned_at, expires_at, created_at
		FROM trace_codes WHERE `+joinAnd(conds)+
		` ORDER BY created_at DESC LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)), args...)
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

func (api *API) getCode(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	row, err := api.svc.pool.Query(r.Context(), `
		SELECT id, code, code_type, ref_type, ref_id, product_batch_id, package_id, status,
			scan_count, first_scanned_at, last_scanned_at, expires_at, created_at
		FROM trace_codes WHERE tenant_id=$1 AND code=$2`, id.TenantID, chi.URLParam(r, "code"))
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	item, err := pgx.CollectOneRow(row, pgx.RowToMap)
	row.Close()
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("trace code"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, item, nil)
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
