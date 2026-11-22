-- 0013_ai_knowledge.sql — AI knowledge base for RAG-lite chat retrieval
-- (specs/07.6 AI 知识库, docs/adr/adr-009). Retrieval is keyword/shingle-based
-- scoring in the application layer (works for CJK without extension deps);
-- vector search (pgvector) is the documented future upgrade.

CREATE TABLE knowledge_docs (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    title       TEXT NOT NULL,
    category    TEXT NOT NULL DEFAULT 'general', -- 种植 | 病虫害 | 肥料 | 农药 | 农机 | 气象 | 土壤 | 质量 | 法规 | 标准 | general
    source      TEXT NOT NULL DEFAULT 'manual',  -- manual | upload
    file_id     TEXT,
    content     TEXT NOT NULL,
    chunk_count INT NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'ready',   -- ready | failed
    created_by  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    version     INT NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_knowledge_docs_updated BEFORE UPDATE ON knowledge_docs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_knowledge_docs_tenant ON knowledge_docs(tenant_id, created_at DESC);

CREATE TABLE knowledge_chunks (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    doc_id      TEXT NOT NULL REFERENCES knowledge_docs(id) ON DELETE CASCADE,
    chunk_index INT NOT NULL,
    content     TEXT NOT NULL
);
CREATE INDEX idx_knowledge_chunks_tenant ON knowledge_chunks(tenant_id);
CREATE INDEX idx_knowledge_chunks_doc ON knowledge_chunks(doc_id, chunk_index);
