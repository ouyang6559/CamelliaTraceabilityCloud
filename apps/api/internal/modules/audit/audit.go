// Package audit exposes the audit log (specs/08.5).
package audit

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
)

// Module wires audit routes.
type Module struct {
	pool *pgxpool.Pool
}

func NewModule(pool *pgxpool.Pool) *Module { return &Module{pool: pool} }

// Mount registers /audit-logs.
func (m *Module) Mount(r chi.Router, authorize func(resource, action string) func(http.Handler) http.Handler) {
	r.With(authorize("audit", "read")).Get("/audit-logs", m.list)
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	q := r.URL.Query()
	args := []any{id.TenantID}
	conds := []string{"al.tenant_id = $1"}
	for param, col := range map[string]string{
		"resource_type": "al.resource_type", "resource_id": "al.resource_id", "actor_id": "al.actor_id",
	} {
		if v := q.Get(param); v != "" {
			args = append(args, v)
			conds = append(conds, col+" = $"+itoa(len(args)))
		}
	}
	if v := q.Get("search"); v != "" {
		args = append(args, "%"+strings.ToLower(v)+"%")
		conds = append(conds, "lower(al.action) LIKE $"+itoa(len(args)))
	}
	var total int64
	if err := m.pool.QueryRow(r.Context(),
		"SELECT count(*) FROM audit_logs al WHERE "+strings.Join(conds, " AND "), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := m.pool.Query(r.Context(), `
		SELECT al.id, al.tenant_id, al.actor_id, al.actor_name, al.action, al.resource_type,
			al.resource_id, al.new_value, al.ip, al.user_agent, al.created_at
		FROM audit_logs al WHERE `+strings.Join(conds, " AND ")+` ORDER BY al.created_at DESC
		LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)), args...)
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

func itoa(n int) string { return strconv.Itoa(n) }
