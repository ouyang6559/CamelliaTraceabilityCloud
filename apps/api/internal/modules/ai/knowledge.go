// AI knowledge base: document ingestion, chunking and RAG-lite retrieval
// (specs/07.6 AI 知识库, docs/adr/adr-009).
//
// Retrieval is keyword/shingle-based scoring in Go — works for Chinese content
// without extension dependencies (CJK bigrams + latin words). Vector search
// (pgvector) is the documented future upgrade; this layer is deliberately
// honest about being RAG-lite, not full semantic search.
package ai

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

const (
	chunkMaxRunes    = 400
	chunkOverlap     = 40
	retrieveTopK     = 4
	retrieveMaxRows  = 800
	retrieveMinScore = 1
)

// ---------- chunking ----------

// chunkContent splits normalized text into overlapping chunks. Paragraphs
// (lines) are kept intact when possible; long paragraphs fall back to a
// sliding window with overlap. Deterministic and unit-tested.
func chunkContent(content string, maxRunes, overlap int) []string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return nil
	}
	if overlap >= maxRunes {
		overlap = maxRunes / 4
	}
	var chunks []string
	var buf []rune
	flush := func() {
		text := strings.TrimSpace(string(buf))
		if text != "" {
			chunks = append(chunks, text)
		}
		if len(buf) > overlap {
			buf = []rune(string(buf[len(buf)-overlap:]))
		} else {
			buf = nil
		}
	}
	for _, line := range strings.Split(normalized, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lineRunes := []rune(line)
		if len(buf)+len(lineRunes)+1 > maxRunes {
			if len(lineRunes) > maxRunes {
				// Oversized single paragraph: sliding window.
				flush()
				for i := 0; i < len(lineRunes); i += maxRunes - overlap {
					end := i + maxRunes
					if end > len(lineRunes) {
						end = len(lineRunes)
					}
					chunks = append(chunks, strings.TrimSpace(string(lineRunes[i:end])))
					if end >= len(lineRunes) {
						break
					}
				}
				buf = nil
				continue
			}
			flush()
		}
		if buf != nil {
			buf = append(buf, ' ')
		}
		buf = append(buf, lineRunes...)
	}
	flush()
	if chunks == nil {
		chunks = []string{normalized}
	}
	return chunks
}

// ---------- retrieval (RAG-lite scoring) ----------

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}

// tokenizeQuery extracts latin words (>= 2 runes) and CJK bigrams for scoring.
func tokenizeQuery(q string) []string {
	q = strings.ToLower(q)
	seen := map[string]bool{}
	var tokens []string
	for _, w := range strings.FieldsFunc(q, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len([]rune(w)) >= 2 && !seen[w] {
			tokens = append(tokens, w)
			seen[w] = true
		}
	}
	runes := []rune(q)
	for i := 0; i+1 < len(runes); i++ {
		if isCJK(runes[i]) && isCJK(runes[i+1]) {
			bg := string(runes[i : i+2])
			if !seen[bg] {
				tokens = append(tokens, bg)
				seen[bg] = true
			}
		}
	}
	return tokens
}

// scoreChunk counts token occurrences in the chunk (lowercased).
func scoreChunk(content string, tokens []string) int {
	lc := strings.ToLower(content)
	score := 0
	for _, tk := range tokens {
		score += strings.Count(lc, tk)
	}
	return score
}

// KnowledgeSource is one retrieval hit attached to an AI answer.
type KnowledgeSource struct {
	DocID    string `json:"doc_id"`
	Title    string `json:"title"`
	Category string `json:"category"`
	Score    int    `json:"score"`
	Excerpt  string `json:"excerpt"`
}

// chunkHit is one candidate chunk for ranking.
type chunkHit struct {
	chunkID   string
	docID     string
	content   string
	title     string
	cat       string
	keyword   int
	embedding []float64
	score     float64 // combined score (keyword + cosine weight), computed by rankHits
}

// rankHits scores hits by keyword count plus semantic cosine (when embeddings
// exist on both sides), dedups per doc keeping the best chunk, sorts desc and
// returns the top-K. Pure function — unit-tested (ADR-014).
func rankHits(hits []chunkHit, tokens []string, queryEmbedding []float64) []chunkHit {
	for i := range hits {
		kw := scoreChunk(hits[i].content, tokens)
		hits[i].keyword = kw
		score := float64(kw)
		if queryEmbedding != nil && len(hits[i].embedding) > 0 {
			// cosine ∈ [-1,1]; weight 10 makes a strong semantic match
			// comparable to ~10 keyword hits.
			score += 10 * cosine(queryEmbedding, hits[i].embedding)
		}
		hits[i].score = score
	}
	best := map[string]chunkHit{}
	for _, h := range hits {
		if cur, ok := best[h.docID]; !ok || h.score > cur.score {
			best[h.docID] = h
		}
	}
	ranked := make([]chunkHit, 0, len(best))
	for _, h := range best {
		ranked = append(ranked, h)
	}
	for i := 1; i < len(ranked); i++ {
		for j := i; j > 0 && ranked[j].score > ranked[j-1].score; j-- {
			ranked[j], ranked[j-1] = ranked[j-1], ranked[j]
		}
	}
	if len(ranked) > retrieveTopK {
		ranked = ranked[:retrieveTopK]
	}
	return ranked
}

// retrieveKnowledge ranks the tenant's chunks against the query: keyword
// scoring (ADR-009) merged with semantic cosine when embeddings exist
// (ADR-014). Capped at retrieveMaxRows rows scanned.
func (m *Module) retrieveKnowledge(ctx context.Context, tenantID, query string) ([]KnowledgeSource, error) {
	tokens := tokenizeQuery(query)
	if len(tokens) == 0 {
		return nil, nil
	}

	// Query embedding (best-effort; keyword ranking remains the fallback).
	var queryEmbedding []float64
	if m.embedder != nil {
		embCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if vecs, err := m.embedder.Embed(embCtx, []string{query}); err == nil && len(vecs) > 0 {
			queryEmbedding = vecs[0]
		}
		cancel()
	}

	rows, err := m.pool.Query(ctx, `
		SELECT kc.id, kc.doc_id, kc.content, d.title, COALESCE(d.category,'general'), kc.embedding
		FROM knowledge_chunks kc
		JOIN knowledge_docs d ON d.id = kc.doc_id AND d.tenant_id = kc.tenant_id AND d.status = 'ready'
		WHERE kc.tenant_id = $1
		LIMIT $2`, tenantID, retrieveMaxRows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hits []chunkHit
	for rows.Next() {
		var h chunkHit
		var embedding []float64
		if err := rows.Scan(&h.chunkID, &h.docID, &h.content, &h.title, &h.cat, &embedding); err != nil {
			return nil, err
		}
		if queryEmbedding != nil || scoreChunk(h.content, tokens) >= retrieveMinScore {
			h.embedding = embedding
			hits = append(hits, h)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	ranked := rankHits(hits, tokens, queryEmbedding)

	sources := make([]KnowledgeSource, 0, len(ranked))
	for _, h := range ranked {
		if h.score <= 0 {
			continue
		}
		excerpt := h.content
		rn := []rune(excerpt)
		if len(rn) > 160 {
			excerpt = string(rn[:160]) + "…"
		}
		sources = append(sources, KnowledgeSource{
			DocID: h.docID, Title: h.title, Category: h.cat,
			Score: int(h.score), Excerpt: excerpt,
		})
	}
	return sources, nil
}

// buildKnowledgeContext renders the retrieved hits as a prompt context block.
func buildKnowledgeContext(sources []KnowledgeSource) string {
	if len(sources) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[知识库摘录 / Knowledge base excerpts]\n")
	for i, src := range sources {
		b.WriteString(fmt.Sprintf("%d. (%s) %s: %s\n", i+1, src.Category, src.Title, src.Excerpt))
	}
	return b.String()
}

// ---------- HTTP handlers ----------

type knowledgeWrite struct {
	Title    string `json:"title"`
	Category string `json:"category"`
	Content  string `json:"content"`
}

func (m *Module) listKnowledge(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	rows, err := m.pool.Query(r.Context(), `
		SELECT d.id, d.title, d.category, d.source, d.chunk_count, d.status, d.created_at
		FROM knowledge_docs d WHERE d.tenant_id=$1 ORDER BY d.created_at DESC`, id.TenantID)
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
	httpx.Respond(w, r, httpx.NewPage(items, int64(len(items)), httpx.PageParams{Page: 1, PageSize: len(items)}), nil)
}

func (m *Module) createKnowledge(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	var req knowledgeWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Content) == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("title and content are required"))
		return
	}
	if req.Category == "" {
		req.Category = "general"
	}
	newID := ulidx.New()
	chunks := chunkContent(req.Content, chunkMaxRunes, chunkOverlap)
	tx, err := m.pool.Begin(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO knowledge_docs (id, tenant_id, title, category, source, content, chunk_count, status, created_by)
		VALUES ($1,$2,$3,$4,'manual',$5,$6,'ready',$7)`,
		newID, id.TenantID, req.Title, req.Category, req.Content, len(chunks), id.UserID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	for i, ch := range chunks {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO knowledge_chunks (id, tenant_id, doc_id, chunk_index, content)
			VALUES ($1,$2,$3,$4,$5)`, ulidx.New(), id.TenantID, newID, i, ch); err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Created(w, r, map[string]any{"id": newID, "chunks": len(chunks)})
}

// importKnowledgeFile ingests an uploaded text/markdown file (multipart) as a
// knowledge document. Binary formats (PDF/DOCX) are rejected honestly — text
// extraction is a documented TODO.
func (m *Module) importKnowledgeFile(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		httpx.Respond(w, r, nil, apierr.Invalid("multipart form required (max 4MB)"))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Respond(w, r, nil, apierr.Invalid("file field is required"))
		return
	}
	defer file.Close()
	name := strings.ToLower(header.Filename)
	if !strings.HasSuffix(name, ".txt") && !strings.HasSuffix(name, ".md") {
		httpx.Respond(w, r, nil, apierr.Invalid("only .txt/.md are supported; PDF/DOCX text extraction is a TODO"))
		return
	}
	var content strings.Builder
	buf := make([]byte, 32*1024)
	for {
		n, err := file.Read(buf)
		if n > 0 {
			content.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	title := strings.TrimSuffix(header.Filename, ".txt")
	title = strings.TrimSuffix(title, ".md")

	var req knowledgeWrite
	req.Title = title
	req.Category = r.FormValue("category")
	req.Content = content.String()
	if strings.TrimSpace(req.Content) == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("file is empty"))
		return
	}
	if req.Category == "" {
		req.Category = "general"
	}
	newID := ulidx.New()
	chunks := chunkContent(req.Content, chunkMaxRunes, chunkOverlap)
	tx, err := m.pool.Begin(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO knowledge_docs (id, tenant_id, title, category, source, content, chunk_count, status, created_by)
		VALUES ($1,$2,$3,$4,'upload',$5,$6,'ready',$7)`,
		newID, id.TenantID, req.Title, req.Category, req.Content, len(chunks), id.UserID); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	for i, ch := range chunks {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO knowledge_chunks (id, tenant_id, doc_id, chunk_index, content)
			VALUES ($1,$2,$3,$4,$5)`, ulidx.New(), id.TenantID, newID, i, ch); err != nil {
			httpx.Respond(w, r, nil, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Created(w, r, map[string]any{"id": newID, "chunks": len(chunks)})
}

func (m *Module) deleteKnowledge(w http.ResponseWriter, r *http.Request) {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	ct, err := m.pool.Exec(r.Context(),
		`DELETE FROM knowledge_docs WHERE tenant_id=$1 AND id=$2`, id.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Respond(w, r, nil, apierr.NotFound("knowledge document"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
