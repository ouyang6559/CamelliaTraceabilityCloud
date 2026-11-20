-- 0005_trace.sql — trace events (hash chain), trace codes, evidence, blockchain proofs

CREATE TABLE trace_events (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id),
    event_type   TEXT NOT NULL, -- planting / fertilize / spray / irrigate / weed / prune / inspect / uav_mission / robot_mission / harvest / warehouse_in / processing / quality_test / packing / logistics / warehouse_out / note
    event_time   TIMESTAMPTZ NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    farm_id      TEXT REFERENCES farms(id),
    plot_id      TEXT REFERENCES plots(id),
    crop_batch_id TEXT REFERENCES crop_batches(id),
    ref_type     TEXT NOT NULL DEFAULT '',  -- crop_batch | harvest | raw_material_batch | processing_batch | product_batch | package | mission | ...
    ref_id       TEXT NOT NULL DEFAULT '',
    task_id      TEXT REFERENCES agricultural_tasks(id),
    device_id    TEXT,
    mission_id   TEXT,
    operator_id  TEXT,
    location     GEOMETRY(Point, 4326),
    quantity     NUMERIC(14,3),
    unit         TEXT NOT NULL DEFAULT '',
    inputs       JSONB NOT NULL DEFAULT '[]',
    media        JSONB NOT NULL DEFAULT '[]',
    weather      JSONB,
    metadata     JSONB NOT NULL DEFAULT '{}',
    sequence     BIGINT NOT NULL DEFAULT 0, -- per-tenant chain sequence
    prev_hash    TEXT NOT NULL DEFAULT '',
    hash         TEXT NOT NULL DEFAULT '',  -- SHA-256 of canonical payload + prev_hash
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by   TEXT,
    version      INT NOT NULL DEFAULT 1
);
CREATE INDEX idx_trace_events_tenant_time ON trace_events(tenant_id, event_time DESC);
CREATE INDEX idx_trace_events_ref ON trace_events(ref_type, ref_id);
CREATE INDEX idx_trace_events_crop_batch ON trace_events(crop_batch_id);
CREATE INDEX idx_trace_events_plot ON trace_events(plot_id);

CREATE TABLE trace_codes (
    id               TEXT PRIMARY KEY,
    tenant_id        TEXT NOT NULL REFERENCES tenants(id),
    code             TEXT NOT NULL UNIQUE,
    code_type        TEXT NOT NULL DEFAULT 'package', -- batch | package | carton | pallet | logistics | consumer
    ref_type         TEXT NOT NULL DEFAULT '',
    ref_id           TEXT NOT NULL DEFAULT '',
    product_batch_id TEXT REFERENCES product_batches(id),
    package_id       TEXT REFERENCES packages(id),
    status           TEXT NOT NULL DEFAULT 'active', -- active | revoked | expired
    scan_count       BIGINT NOT NULL DEFAULT 0,
    first_scanned_at TIMESTAMPTZ,
    last_scanned_at  TIMESTAMPTZ,
    expires_at       TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by       TEXT,
    version          INT NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_trace_codes_updated BEFORE UPDATE ON trace_codes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_trace_codes_tenant ON trace_codes(tenant_id, created_at DESC);
CREATE INDEX idx_trace_codes_ref ON trace_codes(ref_type, ref_id);

CREATE TABLE evidence (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL REFERENCES tenants(id),
    trace_event_id TEXT REFERENCES trace_events(id),
    ref_type       TEXT NOT NULL DEFAULT '',
    ref_id         TEXT NOT NULL DEFAULT '',
    evidence_type  TEXT NOT NULL DEFAULT 'photo', -- photo | video | pdf | report | gps | signature | other
    file_id        TEXT,
    url            TEXT NOT NULL DEFAULT '',
    filename       TEXT NOT NULL DEFAULT '',
    mime           TEXT NOT NULL DEFAULT '',
    size           BIGINT NOT NULL DEFAULT 0,
    sha256         TEXT NOT NULL,
    algorithm      TEXT NOT NULL DEFAULT 'sha256',
    captured_at    TIMESTAMPTZ,
    note           TEXT NOT NULL DEFAULT '',
    uploaded_by    TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    version        INT NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_evidence_updated BEFORE UPDATE ON evidence
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_evidence_tenant ON evidence(tenant_id, created_at DESC);
CREATE INDEX idx_evidence_ref ON evidence(ref_type, ref_id);

CREATE TABLE blockchain_proofs (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL REFERENCES tenants(id),
    ref_type       TEXT NOT NULL,
    ref_id         TEXT NOT NULL,
    evidence_id    TEXT REFERENCES evidence(id),
    algorithm      TEXT NOT NULL DEFAULT 'sha256', -- sha256 | sm3
    hash           TEXT NOT NULL,
    provider       TEXT NOT NULL DEFAULT 'mock',   -- mock | antchain | tencent-zxin | ...
    provider_tx_id TEXT NOT NULL DEFAULT '',
    block_height   BIGINT,
    anchor_url     TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'confirmed', -- pending | confirmed | failed
    confirmed_at   TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by     TEXT,
    version        INT NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_proofs_updated BEFORE UPDATE ON blockchain_proofs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_proofs_tenant ON blockchain_proofs(tenant_id, created_at DESC);
CREATE INDEX idx_proofs_ref ON blockchain_proofs(ref_type, ref_id);
