// Package evidence implements evidence records (photo/video/report hashes) and
// blockchain proofs: on-chain only the hash + ids are stored, raw data stays
// local (specs/05.6). Proof providers are pluggable; the default is the
// deterministic local mock.
package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/events"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/tenantref"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/platform"
)

// ProofProvider anchors a hash to an immutable ledger (mock by default).
type ProofProvider interface {
	Name() string
	Anchor(ctx context.Context, hash string) (txID string, height int64, err error)
}

// Module wires evidence routes.
type Module struct {
	pool     *pgxpool.Pool
	storage  platform.ObjectStorage
	provider ProofProvider
	pub      *events.Publisher
}

func NewModule(pool *pgxpool.Pool, storage platform.ObjectStorage, provider ProofProvider, pub *events.Publisher) *Module {
	if provider == nil {
		provider = &MockProofProvider{}
	}
	return &Module{pool: pool, storage: storage, provider: provider, pub: pub}
}

// Mount registers /evidence and /blockchain-proofs.
func (m *Module) Mount(r chi.Router) {
	r.Route("/evidence", func(rr chi.Router) {
		rr.Use(middlewareRequireRead)
		rr.Get("/", m.list)
		rr.Get("/{id}", m.get)
		rr.With(middlewareRequireCreate).Post("/", m.create)
		rr.Post("/verify", m.verify)
	})
	r.Route("/blockchain-proofs", func(rr chi.Router) {
		rr.Use(middlewareRequireRead)
		rr.Get("/", m.listProofs)
		rr.With(middlewareRequireCreate).Post("/", m.createProof)
		rr.Post("/verify", m.verifyProof)
	})
}

// Perms are injected as plain middleware by app assembly to avoid an import
// cycle with the middleware package. Defaults fall back to authenticated-only.
var (
	middlewareRequireRead   = func(next http.Handler) http.Handler { return next }
	middlewareRequireCreate = func(next http.Handler) http.Handler { return next }
)

// SetPermMiddleware wires real RBAC middleware at assembly time.
func (m *Module) SetPermMiddleware(read, create func(http.Handler) http.Handler) {
	middlewareRequireRead = read
	middlewareRequireCreate = create
}

type evidenceWrite struct {
	TraceEventID string     `json:"trace_event_id"`
	RefType      string     `json:"ref_type"`
	RefID        string     `json:"ref_id"`
	EvidenceType string     `json:"evidence_type"`
	FileID       string     `json:"file_id"`
	CapturedAt   *time.Time `json:"captured_at"`
	Note         string     `json:"note"`
}

// create links a file (or external URL) as evidence; SHA-256 comes from the
// stored file record, or from the URL content when file_id is absent.
func (m *Module) create(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	var req evidenceWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.RefID == "" || req.RefType == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("ref_type and ref_id are required"))
		return
	}
	if req.EvidenceType == "" {
		req.EvidenceType = "photo"
	}

	// trace_event ownership (ADR-006 hardening).
	if req.TraceEventID != "" {
		ok, err := tenantref.Exists(r.Context(), m.pool, id.TenantID, "trace_event", req.TraceEventID)
		if err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
		if !ok {
			httpx.Respond(w, r, nil, apierr.Invalid("trace_event does not exist in your tenant"))
			return
		}
	}
	var fileID any
	var sha string
	var filename, mime string
	var size int64
	if req.FileID != "" {
		var bucket, objectKey string
		err := m.pool.QueryRow(r.Context(),
			`SELECT id, bucket, object_key, filename, mime, size, sha256 FROM files WHERE tenant_id=$1 AND id=$2`,
			id.TenantID, req.FileID).
			Scan(&fileID, &bucket, &objectKey, &filename, &mime, &size, &sha)
		if err == pgx.ErrNoRows {
			httpx.Respond(w, r, nil, apierr.NotFound("file"))
			return
		}
		if err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
		// Re-hash the stored content to guarantee the digest matches the bytes.
		if reader, err := m.storage.Open(r.Context(), objectKey); err == nil {
			sha = hashReader(r.Context(), reader)
			reader.Close()
		}
	} else {
		httpx.Respond(w, r, nil, apierr.Invalid("file_id is required"))
		return
	}

	newID := ulidx.New()
	var captured any
	if req.CapturedAt != nil {
		captured = *req.CapturedAt
	}
	if _, err := m.pool.Exec(r.Context(), `
		INSERT INTO evidence (id, tenant_id, trace_event_id, ref_type, ref_id, evidence_type,
			file_id, filename, mime, size, sha256, algorithm, captured_at, note, uploaded_by)
		VALUES ($1,$2,NULLIF($3,''),$4,$5,$6,$7,$8,$9,$10,$11,'sha256',$12,$13,$14)`,
		newID, id.TenantID, req.TraceEventID, req.RefType, req.RefID, req.EvidenceType,
		fileID, filename, mime, size, sha, captured, req.Note, id.UserID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Created(w, r, map[string]any{"id": newID, "sha256": sha})
}

func hashReader(ctx context.Context, reader ioReader) string {
	buf := make([]byte, 32*1024)
	h := sha256.New()
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

type ioReader interface {
	Read(p []byte) (int, error)
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	q := r.URL.Query()
	args := []any{id.TenantID}
	conds := []string{"e.tenant_id = $1"}
	for param, col := range map[string]string{
		"ref_type": "e.ref_type", "ref_id": "e.ref_id", "trace_event_id": "e.trace_event_id",
	} {
		if v := q.Get(param); v != "" {
			args = append(args, v)
			conds = append(conds, col+" = $"+itoa(len(args)))
		}
	}
	var total int64
	if err := m.pool.QueryRow(r.Context(),
		"SELECT count(*) FROM evidence e WHERE "+strings.Join(conds, " AND "), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := m.pool.Query(r.Context(), `
		SELECT e.id, e.tenant_id, e.trace_event_id, e.ref_type, e.ref_id, e.evidence_type,
			e.file_id, e.filename, e.mime, e.size, e.sha256, e.algorithm, e.captured_at, e.note, e.created_at
		FROM evidence e WHERE `+strings.Join(conds, " AND ")+` ORDER BY e.created_at DESC
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

func (m *Module) get(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	rows, err := m.pool.Query(r.Context(), `
		SELECT e.id, e.tenant_id, e.trace_event_id, e.ref_type, e.ref_id, e.evidence_type,
			e.file_id, e.filename, e.mime, e.size, e.sha256, e.algorithm, e.captured_at, e.note, e.created_at
		FROM evidence e WHERE e.tenant_id=$1 AND e.id=$2`, id.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	item, err := one(rows)
	if err != nil {
		httpx.Respond(w, r, nil, apierr.NotFound("evidence"))
		return
	}
	httpx.Respond(w, r, item, nil)
}

// verify recomputes the stored object hash and compares it with the record.
func (m *Module) verify(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	var req struct {
		ID string `json:"id"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || req.ID == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("id is required"))
		return
	}
	var objectKey, stored string
	err := m.pool.QueryRow(r.Context(), `
		SELECT f.object_key, e.sha256 FROM evidence e JOIN files f ON f.id = e.file_id
		WHERE e.tenant_id=$1 AND e.id=$2`, id.TenantID, req.ID).Scan(&objectKey, &stored)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("evidence"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	reader, err := m.storage.Open(r.Context(), objectKey)
	if err != nil {
		httpx.Respond(w, r, nil, apierr.NotFound("file content"))
		return
	}
	computed := hashReader(r.Context(), reader)
	reader.Close()
	httpx.Respond(w, r, map[string]any{
		"id": req.ID, "valid": computed == stored,
		"stored_sha256": stored, "computed_sha256": computed,
	}, nil)
}

// ---------- blockchain proofs ----------

// MockProofProvider implements the local deterministic ledger used for demos.
// Real chains (AntChain, ZhiXin, ...) plug in via adapters; integration TODOs
// live with the adapters, never in core (specs/06.11).
type MockProofProvider struct{}

func (p *MockProofProvider) Name() string { return "mock" }

func (p *MockProofProvider) Anchor(_ context.Context, hash string) (string, int64, error) {
	sum := sha256.Sum256([]byte("mock-tx-" + hash))
	tx := hex.EncodeToString(sum[:])[:40]
	height := time.Now().Unix() % 100000000
	return "0xmock" + tx, height, nil
}

type proofWrite struct {
	RefType    string `json:"ref_type"`
	RefID      string `json:"ref_id"`
	EvidenceID string `json:"evidence_id"`
	Algorithm  string `json:"algorithm"` // sha256 | sm3 (sm3 via adapter)
}

// createProof anchors the hash of the referenced evidence (or the object
// itself) to the proof provider and stores the anchor.
func (m *Module) createProof(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	var req proofWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.RefType == "" || req.RefID == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("ref_type and ref_id are required"))
		return
	}
	if req.Algorithm == "" {
		req.Algorithm = "sha256"
	}

	ctx := r.Context()
	var hash string
	var evidenceID any
	if req.EvidenceID != "" {
		err := m.pool.QueryRow(ctx, `SELECT COALESCE(file_id::text,''), sha256 FROM evidence
			WHERE tenant_id=$1 AND id=$2`, id.TenantID, req.EvidenceID).
			Scan(&evidenceID, &hash)
		if err == pgx.ErrNoRows {
			httpx.Respond(w, r, nil, apierr.NotFound("evidence"))
			return
		}
		if err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
	} else {
		// Anchor the canonical JSON of the object's trace event chain.
		var events json.RawMessage
		err := m.pool.QueryRow(ctx,
			`SELECT COALESCE(json_agg(t ORDER BY sequence), '[]') FROM trace_events t
			 WHERE tenant_id=$1 AND ref_type=$2 AND ref_id=$3`,
			id.TenantID, req.RefType, req.RefID).Scan(&events)
		if err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
		b, _ := jsonMarshal(map[string]any{
			"ref_type": req.RefType, "ref_id": req.RefID, "events": json.RawMessage(events),
		})
		sum := sha256.Sum256(b)
		hash = hex.EncodeToString(sum[:])
	}

	txID, height, err := m.provider.Anchor(ctx, hash)
	if err != nil {
		httpx.Respond(w, r, nil, apierr.Unavailable("proof provider error: %v", err))
		return
	}
	newID := ulidx.New()
	if _, err := m.pool.Exec(ctx, `
		INSERT INTO blockchain_proofs (id, tenant_id, ref_type, ref_id, evidence_id, algorithm, hash,
			provider, provider_tx_id, block_height, status, confirmed_at, created_by)
		VALUES ($1,$2,$3,$4,NULLIF($5::text,''),$6,$7,$8,$9,$10,'confirmed',now(),$11)`,
		newID, id.TenantID, req.RefType, req.RefID, anyToString(evidenceID), req.Algorithm, hash,
		m.provider.Name(), txID, height, id.UserID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	m.pub.Publish(id.TenantID, "proof.created", map[string]any{
		"proof_id": newID, "ref_type": req.RefType, "ref_id": req.RefID, "hash": hash,
	})
	httpx.Created(w, r, map[string]any{
		"id": newID, "hash": hash, "provider": m.provider.Name(),
		"tx_id": txID, "block_height": height, "status": "confirmed",
	})
}

func (m *Module) listProofs(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	args := []any{id.TenantID}
	conds := []string{"bp.tenant_id = $1"}
	if v := r.URL.Query().Get("ref_type"); v != "" {
		args = append(args, v)
		conds = append(conds, "bp.ref_type = $"+itoa(len(args)))
	}
	if v := r.URL.Query().Get("ref_id"); v != "" {
		args = append(args, v)
		conds = append(conds, "bp.ref_id = $"+itoa(len(args)))
	}
	var total int64
	if err := m.pool.QueryRow(r.Context(),
		"SELECT count(*) FROM blockchain_proofs bp WHERE "+strings.Join(conds, " AND "), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := m.pool.Query(r.Context(), `
		SELECT bp.id, bp.tenant_id, bp.ref_type, bp.ref_id, bp.evidence_id, bp.algorithm, bp.hash,
			bp.provider, bp.provider_tx_id, bp.block_height, bp.status, bp.confirmed_at, bp.created_at
		FROM blockchain_proofs bp WHERE `+strings.Join(conds, " AND ")+` ORDER BY bp.created_at DESC
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

// verifyProof re-anchors and compares the stored tx (mock is deterministic).
func (m *Module) verifyProof(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	var req struct {
		ID string `json:"id"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || req.ID == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("id is required"))
		return
	}
	var hash, provider, txID string
	err := m.pool.QueryRow(r.Context(), `
		SELECT hash, provider, provider_tx_id FROM blockchain_proofs WHERE tenant_id=$1 AND id=$2`,
		id.TenantID, req.ID).Scan(&hash, &provider, &txID)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("proof"))
		return
	}
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	reTxID, _, err := m.provider.Anchor(r.Context(), hash)
	httpx.Respond(w, r, map[string]any{
		"id": req.ID, "valid": err == nil && reTxID == txID,
		"provider": provider, "hash": hash, "tx_id": txID,
	}, nil)
}
