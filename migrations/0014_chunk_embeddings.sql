-- 0014_chunk_embeddings.sql — semantic retrieval upgrade (docs/adr/adr-014).
-- Embeddings are stored as jsonb float arrays and ranked in Go (cosine);
-- the pgvector extension + HNSW index is the documented scale upgrade once
-- the deployment image ships it (postgis base image has no pgvector yet).

ALTER TABLE knowledge_chunks ADD COLUMN IF NOT EXISTS embedding jsonb;
ALTER TABLE knowledge_chunks ADD COLUMN IF NOT EXISTS embedding_model TEXT NOT NULL DEFAULT '';
