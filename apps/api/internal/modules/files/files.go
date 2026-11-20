// Package files implements media upload/download against the configured object
// storage backend, with SHA-256 digests for evidence hashing (specs/05.4).
package files

import (
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/platform"
)

// Module wires file routes.
type Module struct {
	pool    *pgxpool.Pool
	storage platform.ObjectStorage
	bucket  string
	auth    func(http.Handler) http.Handler
}

func NewModule(pool *pgxpool.Pool, storage platform.ObjectStorage, bucket string, auth func(http.Handler) http.Handler) *Module {
	return &Module{pool: pool, storage: storage, bucket: bucket, auth: auth}
}

// Mount registers /files (JWT-protected). The public evidence endpoint is
// mounted by the trace module via ServeEvidenceFile.
func (m *Module) Mount(r chi.Router, requirePerm func(resource, action string) func(http.Handler) http.Handler) {
	r.Route("/files", func(rr chi.Router) {
		rr.Use(m.auth)
		rr.With(requirePerm("file", "create")).Post("/", m.upload)
		rr.With(requirePerm("file", "read")).Get("/{id}/content", m.download)
	})
}

func (m *Module) upload(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		httpx.Respond(w, r, nil, apierr.Invalid("multipart form required"))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Respond(w, r, nil, apierr.Invalid("file field is required"))
		return
	}
	defer file.Close()

	newID := ulidx.New()
	objectKey := id.TenantID + "/" + newID + "-" + header.Filename
	sha, err := platform.PutFile(r.Context(), m.storage, objectKey, file)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if _, err := m.pool.Exec(r.Context(), `
		INSERT INTO files (id, tenant_id, bucket, object_key, filename, mime, size, sha256, uploaded_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		newID, id.TenantID, m.bucket, objectKey, header.Filename, header.Header.Get("Content-Type"),
		header.Size, sha, id.UserID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Created(w, r, map[string]any{
		"id": newID, "filename": header.Filename, "size": header.Size, "sha256": sha,
	})
}

func (m *Module) download(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	var objectKey, filename, mime string
	err = m.pool.QueryRow(r.Context(),
		`SELECT object_key, filename, mime FROM files WHERE tenant_id=$1 AND id=$2`,
		id.TenantID, chi.URLParam(r, "id")).Scan(&objectKey, &filename, &mime)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("file"))
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
	defer reader.Close()
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filename))
	_, _ = io.Copy(w, reader)
}

// ServeEvidenceFile implements the public trace-media endpoint used by
// trace-web: only files linked to evidence records are served.
func (m *Module) ServeEvidenceFile(w http.ResponseWriter, r *http.Request, fileID string) {
	var objectKey, filename, mime string
	err := m.pool.QueryRow(r.Context(), `
		SELECT f.object_key, f.filename, f.mime FROM files f
		JOIN evidence e ON e.file_id = f.id
		WHERE f.id = $1 LIMIT 1`, fileID).Scan(&objectKey, &filename, &mime)
	if err == pgx.ErrNoRows {
		httpx.Respond(w, r, nil, apierr.NotFound("file"))
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
	defer reader.Close()
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = io.Copy(w, reader)
}
