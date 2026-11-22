-- 0011_remote_sensing.sql — Phase 4 remote sensing baseline (specs/04.9-04.11).
-- Processing jobs (UAV imagery -> orthophoto/NDVI) and a STAC-lite asset
-- catalog (OGC STAC Item JSON stored per tenant, docs/adr/adr-008).

CREATE TABLE remote_sensing_jobs (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id),
    mission_id      TEXT,
    job_type        TEXT NOT NULL DEFAULT 'orthophoto', -- orthophoto | ndvi | dem
    status          TEXT NOT NULL DEFAULT 'queued',     -- queued | running | succeeded | failed | cancelled
    progress        INT NOT NULL DEFAULT 0,
    processor       TEXT NOT NULL DEFAULT 'mock',       -- mock | nodeodm
    external_task_id TEXT NOT NULL DEFAULT '',
    inputs          JSONB NOT NULL DEFAULT '[]',        -- [{file_id, filename}]
    outputs         JSONB NOT NULL DEFAULT '[]',        -- [{file_id, type, name}]
    error           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    created_by      TEXT,
    version         INT NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id)
);
DROP TRIGGER IF EXISTS trg_rs_jobs_updated ON remote_sensing_jobs;
CREATE TRIGGER trg_rs_jobs_updated BEFORE UPDATE ON remote_sensing_jobs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_rs_jobs_tenant ON remote_sensing_jobs(tenant_id, created_at DESC);
CREATE INDEX idx_rs_jobs_status ON remote_sensing_jobs(status, created_at DESC);

CREATE TABLE stac_items (
    id            TEXT PRIMARY KEY,           -- STAC item id (ULID)
    tenant_id     TEXT NOT NULL REFERENCES tenants(id),
    collection_id TEXT NOT NULL,              -- e.g. 'orthophoto' | 'ndvi' | custom
    item          JSONB NOT NULL,             -- full STAC Item JSON (specs/04.10)
    mission_id    TEXT,
    job_id        TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by    TEXT,
    version       INT NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, collection_id, id)
);
CREATE INDEX idx_stac_items_tenant ON stac_items(tenant_id, collection_id, created_at DESC);
