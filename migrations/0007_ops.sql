-- 0007_ops.sql — quality, alerts, AI jobs, external platforms & sync

CREATE TABLE quality_tests (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id),
    ref_type     TEXT NOT NULL DEFAULT 'product_batch',
    ref_id       TEXT NOT NULL,
    item         TEXT NOT NULL,          -- e.g. acid_value / moisture / pesticide_residue
    standard     TEXT NOT NULL DEFAULT '',
    method       TEXT NOT NULL DEFAULT '',
    result_value NUMERIC(14,4),
    result_text  TEXT NOT NULL DEFAULT '',
    unit         TEXT NOT NULL DEFAULT '',
    passed       BOOLEAN NOT NULL DEFAULT true,
    tested_by    TEXT,
    tested_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    note         TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_quality_tests_tenant ON quality_tests(tenant_id, tested_at DESC);
CREATE INDEX idx_quality_tests_ref ON quality_tests(ref_type, ref_id);

CREATE TABLE quality_reports (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id),
    code       TEXT NOT NULL,
    ref_type   TEXT NOT NULL DEFAULT '',
    ref_id     TEXT NOT NULL DEFAULT '',
    title      TEXT NOT NULL,
    conclusion TEXT NOT NULL DEFAULT '',   -- pass | fail | conditional
    tester     TEXT NOT NULL DEFAULT '',
    tested_at  TIMESTAMPTZ,
    file_id    TEXT,
    items      JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by TEXT,
    version    INT NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);
CREATE TRIGGER trg_quality_reports_updated BEFORE UPDATE ON quality_reports
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE alert_rules (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    name        TEXT NOT NULL,
    device_type TEXT NOT NULL DEFAULT '',
    metric      TEXT NOT NULL,           -- telemetry metric key, e.g. temperature
    operator    TEXT NOT NULL DEFAULT '>', -- > | < | >= | <= | ==
    threshold   NUMERIC(14,4) NOT NULL,
    severity    TEXT NOT NULL DEFAULT 'warning', -- info | warning | critical
    enabled     BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  TEXT,
    version     INT NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_alert_rules_updated BEFORE UPDATE ON alert_rules
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE alerts (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id),
    rule_id         TEXT REFERENCES alert_rules(id),
    device_id       TEXT,
    mission_id      TEXT,
    severity        TEXT NOT NULL DEFAULT 'warning',
    status          TEXT NOT NULL DEFAULT 'open', -- open | acknowledged | resolved
    title           TEXT NOT NULL,
    message         TEXT NOT NULL DEFAULT '',
    payload         JSONB NOT NULL DEFAULT '{}',
    triggered_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    acknowledged_by TEXT,
    acknowledged_at TIMESTAMPTZ,
    resolved_by     TEXT,
    resolved_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    version         INT NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_alerts_updated BEFORE UPDATE ON alerts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_alerts_tenant_status ON alerts(tenant_id, status, triggered_at DESC);
CREATE INDEX idx_alerts_device ON alerts(device_id);

CREATE TABLE ai_jobs (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL REFERENCES tenants(id),
    provider       TEXT NOT NULL DEFAULT 'mock',
    model          TEXT NOT NULL DEFAULT '',
    task_type      TEXT NOT NULL DEFAULT 'chat', -- chat | vision | disease | pest | yield | cost | summary
    status         TEXT NOT NULL DEFAULT 'succeeded', -- queued | running | succeeded | failed
    input_ref_type TEXT NOT NULL DEFAULT '',
    input_ref_id   TEXT NOT NULL DEFAULT '',
    prompt_version TEXT NOT NULL DEFAULT 'v1',
    input_payload  JSONB NOT NULL DEFAULT '{}',
    output         JSONB,
    output_type    TEXT NOT NULL DEFAULT 'RECOMMENDATION', -- FACT | OBSERVATION | PREDICTION | RECOMMENDATION | ACTION
    confidence     NUMERIC(5,4),
    error          TEXT NOT NULL DEFAULT '',
    created_by     TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at    TIMESTAMPTZ,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    version        INT NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_ai_jobs_updated BEFORE UPDATE ON ai_jobs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_ai_jobs_tenant ON ai_jobs(tenant_id, created_at DESC);

CREATE TABLE ai_reviews (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id),
    job_id     TEXT NOT NULL REFERENCES ai_jobs(id),
    actor_id   TEXT,
    action     TEXT NOT NULL, -- approved | rejected
    note       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_ai_reviews_job ON ai_reviews(job_id);

CREATE TABLE external_platforms (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL REFERENCES tenants(id),
    name          TEXT NOT NULL,
    provider_type TEXT NOT NULL, -- mock | national | hubei | himarking | antchain | tencent_zxin | nongzhan | lifei
    status        TEXT NOT NULL DEFAULT 'CONNECTED', -- CONNECTED | AUTH_REQUIRED | DISCONNECTED | ERROR | RATE_LIMITED | SYNCING
    capabilities  TEXT[] NOT NULL DEFAULT '{}',
    config        JSONB NOT NULL DEFAULT '{}', -- credentials redacted on read
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by    TEXT,
    version       INT NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_platforms_updated BEFORE UPDATE ON external_platforms
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_platforms_tenant ON external_platforms(tenant_id);

CREATE TABLE sync_tasks (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id),
    platform_id     TEXT NOT NULL REFERENCES external_platforms(id),
    ref_type        TEXT NOT NULL, -- product | product_batch | trace_event | quality_report | evidence
    ref_id          TEXT NOT NULL,
    action          TEXT NOT NULL DEFAULT 'push', -- push | verify | query
    status          TEXT NOT NULL DEFAULT 'LOCAL_ONLY',
    -- LOCAL_ONLY | PENDING | SYNCING | SYNCED | FAILED | RETRYING
    attempts        INT NOT NULL DEFAULT 0,
    max_attempts    INT NOT NULL DEFAULT 5,
    idempotency_key TEXT NOT NULL DEFAULT '',
    payload         JSONB NOT NULL DEFAULT '{}',
    external_id     TEXT NOT NULL DEFAULT '',
    last_error      TEXT NOT NULL DEFAULT '',
    scheduled_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    synced_at       TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    version         INT NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_sync_tasks_updated BEFORE UPDATE ON sync_tasks
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_sync_tasks_tenant_status ON sync_tasks(tenant_id, status, scheduled_at);
CREATE INDEX idx_sync_tasks_ref ON sync_tasks(ref_type, ref_id);

CREATE TABLE sync_logs (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL,
    sync_task_id TEXT NOT NULL,
    level        TEXT NOT NULL DEFAULT 'info', -- info | warn | error
    message      TEXT NOT NULL DEFAULT '',
    detail       JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_sync_logs_task ON sync_logs(sync_task_id, created_at DESC);
