// Package quality implements quality tests and reports (specs/05.4 evidence,
// specs/14 Phase 5).
package quality

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/crud"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/trace"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

// Module wires quality routes.
type Module struct {
	pool     *pgxpool.Pool
	traceSvc *trace.Service
}

func NewModule(pool *pgxpool.Pool, traceSvc *trace.Service) *Module {
	return &Module{pool: pool, traceSvc: traceSvc}
}

// Mount registers /quality-tests and /quality-reports.
func (m *Module) Mount(r chi.Router, authorize func(resource, action string) func(http.Handler) http.Handler) {
	reports := crud.NewStore(m.pool, crud.Resource{
		Table: "quality_reports", Perm: "quality-report",
		Columns: []crud.Col{
			{Name: "code", Required: true},
			{Name: "ref_type"},
			{Name: "ref_id"},
			{Name: "title", Required: true},
			{Name: "conclusion"},
			{Name: "tester"},
			{Name: "tested_at"},
			{Name: "file_id"},
			{Name: "items"},
		},
		Search:  []string{"code", "title", "tester"},
		Filters: []string{"ref_type", "ref_id", "conclusion"},
	})
	reports.Mount(r, authorize)

	r.Route("/quality-tests", func(rr chi.Router) {
		rr.Use(authorize("quality-test", "read"))
		rr.Get("/", m.listTests)
		rr.With(authorize("quality-test", "create")).Post("/", m.createTest)
		rr.With(authorize("quality-test", "delete")).Delete("/{id}", m.deleteTest)
	})
}

type testWrite struct {
	RefType     string   `json:"ref_type"`
	RefID       string   `json:"ref_id"`
	Item        string   `json:"item"`
	Standard    string   `json:"standard"`
	Method      string   `json:"method"`
	ResultValue *float64 `json:"result_value"`
	ResultText  string   `json:"result_text"`
	Unit        string   `json:"unit"`
	Passed      bool     `json:"passed"`
	TestedBy    string   `json:"tested_by"`
	Note        string   `json:"note"`
}

func (m *Module) listTests(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	args := []any{id.TenantID}
	conds := []string{"qt.tenant_id = $1"}
	if v := r.URL.Query().Get("ref_type"); v != "" {
		args = append(args, v)
		conds = append(conds, "qt.ref_type = $"+itoa(len(args)))
	}
	if v := r.URL.Query().Get("ref_id"); v != "" {
		args = append(args, v)
		conds = append(conds, "qt.ref_id = $"+itoa(len(args)))
	}
	var total int64
	if err := m.pool.QueryRow(r.Context(),
		"SELECT count(*) FROM quality_tests qt WHERE "+joinAnd(conds), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := m.pool.Query(r.Context(), `
		SELECT qt.id, qt.tenant_id, qt.ref_type, qt.ref_id, qt.item, qt.standard, qt.method,
			qt.result_value::float8 AS result_value, qt.result_text, qt.unit, qt.passed,
			qt.tested_by, qt.tested_at, qt.note, qt.created_at
		FROM quality_tests qt WHERE `+joinAnd(conds)+` ORDER BY qt.tested_at DESC
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

// createTest records a test result and appends a quality_test trace event.
func (m *Module) createTest(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	var req testWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.RefID == "" || req.Item == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("ref_id and item are required"))
		return
	}
	if req.RefType == "" {
		req.RefType = "product_batch"
	}
	testedBy := req.TestedBy
	if testedBy == "" {
		testedBy = id.UserID
	}
	newID := ulidx.New()
	if _, err := m.pool.Exec(r.Context(), `
		INSERT INTO quality_tests (id, tenant_id, ref_type, ref_id, item, standard, method,
			result_value, result_text, unit, passed, tested_by, note)
		VALUES ($1,$2,$3,$4,$5,COALESCE(NULLIF($6,''),''),COALESCE(NULLIF($7,''),''),
			$8,COALESCE(NULLIF($9,''),''),COALESCE(NULLIF($10,''),''),$11,$12,COALESCE(NULLIF($13,''),''))`,
		newID, id.TenantID, req.RefType, req.RefID, req.Item, req.Standard, req.Method,
		req.ResultValue, req.ResultText, req.Unit, req.Passed, testedBy, req.Note); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if m.traceSvc != nil {
		_, _ = m.traceSvc.Append(r.Context(), id.TenantID, trace.EventInput{
			EventType:  "quality_test",
			Title:      "Quality test: " + req.Item,
			RefType:    req.RefType,
			RefID:      req.RefID,
			OperatorID: id.UserID,
			Weather:    nil,
			Metadata: map[string]any{
				"item": req.Item, "passed": req.Passed,
				"result_value": req.ResultValue, "unit": req.Unit,
			},
		})
	}
	httpx.Created(w, r, map[string]any{"id": newID})
}

func (m *Module) deleteTest(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	ct, err := m.pool.Exec(r.Context(), `DELETE FROM quality_tests WHERE tenant_id=$1 AND id=$2`,
		id.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.NotFound("quality test"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

func itoa(n int) string { return strconvItoa(n) }
