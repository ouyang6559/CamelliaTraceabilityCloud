-- 0003_agriculture.sql — farms, plots, crops, varieties, crop batches, tasks, inputs, diseases

CREATE TABLE farms (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    name        TEXT NOT NULL,
    code        TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    address     TEXT NOT NULL DEFAULT '',
    location    GEOMETRY(Point, 4326),
    boundary    GEOMETRY(Polygon, 4326),
    area_m2     NUMERIC(14,2) NOT NULL DEFAULT 0,
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  TEXT,
    updated_by  TEXT,
    version     INT NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);
CREATE TRIGGER trg_farms_updated BEFORE UPDATE ON farms
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_farms_tenant ON farms(tenant_id);
CREATE INDEX idx_farms_boundary_gix ON farms USING GIST (boundary);

CREATE TABLE plots (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id),
    farm_id      TEXT NOT NULL REFERENCES farms(id),
    name         TEXT NOT NULL,
    code         TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    boundary     GEOMETRY(Geometry, 4326),
    area_m2      NUMERIC(14,2) NOT NULL DEFAULT 0,
    soil_type    TEXT NOT NULL DEFAULT '',
    planted_year INT,
    status       TEXT NOT NULL DEFAULT 'active',
    srid         INT NOT NULL DEFAULT 4326,
    accuracy_m   NUMERIC(10,2),
    source       TEXT NOT NULL DEFAULT 'manual', -- manual | survey | uav
    captured_at  TIMESTAMPTZ,
    metadata     JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by   TEXT,
    updated_by   TEXT,
    version      INT NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);
CREATE TRIGGER trg_plots_updated BEFORE UPDATE ON plots
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_plots_tenant ON plots(tenant_id);
CREATE INDEX idx_plots_farm ON plots(farm_id);
CREATE INDEX idx_plots_boundary_gix ON plots USING GIST (boundary);

CREATE TABLE crops (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    name        TEXT NOT NULL,
    code        TEXT NOT NULL DEFAULT '',
    category    TEXT NOT NULL DEFAULT '',   -- oil-tea / vegetable / fruit / grain ...
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  TEXT,
    version     INT NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);
CREATE TRIGGER trg_crops_updated BEFORE UPDATE ON crops
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE varieties (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    crop_id     TEXT NOT NULL REFERENCES crops(id),
    name        TEXT NOT NULL,
    code        TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    traits      JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  TEXT,
    version     INT NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);
CREATE TRIGGER trg_varieties_updated BEFORE UPDATE ON varieties
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_varieties_crop ON varieties(crop_id);

CREATE TABLE crop_batches (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    plot_id     TEXT NOT NULL REFERENCES plots(id),
    crop_id     TEXT NOT NULL REFERENCES crops(id),
    variety_id  TEXT REFERENCES varieties(id),
    name        TEXT NOT NULL,
    code        TEXT NOT NULL DEFAULT '',
    planted_at  TIMESTAMPTZ,
    area_m2     NUMERIC(14,2) NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'growing', -- growing | harvested | closed
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  TEXT,
    updated_by  TEXT,
    version     INT NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);
CREATE TRIGGER trg_crop_batches_updated BEFORE UPDATE ON crop_batches
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_crop_batches_tenant ON crop_batches(tenant_id);
CREATE INDEX idx_crop_batches_plot ON crop_batches(plot_id);

CREATE TABLE agricultural_tasks (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL REFERENCES tenants(id),
    farm_id        TEXT REFERENCES farms(id),
    plot_id        TEXT REFERENCES plots(id),
    crop_batch_id  TEXT REFERENCES crop_batches(id),
    mission_id     TEXT,
    type           TEXT NOT NULL DEFAULT 'other', -- tillage / fertilize / prune / spray / irrigate / weed / inspect / harvest / other
    title          TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    priority       TEXT NOT NULL DEFAULT 'normal', -- low | normal | high | urgent
    status         TEXT NOT NULL DEFAULT 'planned', -- planned | in_progress | completed | cancelled
    planned_start_at TIMESTAMPTZ,
    planned_end_at   TIMESTAMPTZ,
    started_at     TIMESTAMPTZ,
    completed_at   TIMESTAMPTZ,
    assignee_id    TEXT REFERENCES users(id),
    source         TEXT NOT NULL DEFAULT 'manual', -- manual | mission | ai | import
    metadata       JSONB NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by     TEXT,
    updated_by     TEXT,
    version        INT NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_tasks_updated BEFORE UPDATE ON agricultural_tasks
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_tasks_tenant_status ON agricultural_tasks(tenant_id, status, planned_start_at);
CREATE INDEX idx_tasks_plot ON agricultural_tasks(plot_id);

CREATE TABLE agricultural_task_records (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    task_id     TEXT NOT NULL REFERENCES agricultural_tasks(id),
    operator_id TEXT REFERENCES users(id),
    note        TEXT NOT NULL DEFAULT '',
    location    GEOMETRY(Point, 4326),
    started_at  TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    media       JSONB NOT NULL DEFAULT '[]',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_task_records_task ON agricultural_task_records(task_id);

CREATE TABLE inputs (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    name        TEXT NOT NULL,
    code        TEXT NOT NULL DEFAULT '',
    category    TEXT NOT NULL DEFAULT 'fertilizer', -- fertilizer | pesticide | seed | other
    unit        TEXT NOT NULL DEFAULT 'kg',
    spec        TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  TEXT,
    version     INT NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);
CREATE TRIGGER trg_inputs_updated BEFORE UPDATE ON inputs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE input_batches (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id),
    input_id   TEXT NOT NULL REFERENCES inputs(id),
    code       TEXT NOT NULL,
    supplier   TEXT NOT NULL DEFAULT '',
    quantity   NUMERIC(14,3) NOT NULL DEFAULT 0,
    unit       TEXT NOT NULL DEFAULT '',
    produced_at TIMESTAMPTZ,
    expired_at TIMESTAMPTZ,
    status     TEXT NOT NULL DEFAULT 'in_stock', -- in_stock | used | expired
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by TEXT,
    version    INT NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);
CREATE TRIGGER trg_input_batches_updated BEFORE UPDATE ON input_batches
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_input_batches_input ON input_batches(input_id);

CREATE TABLE input_usages (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL REFERENCES tenants(id),
    task_id        TEXT NOT NULL REFERENCES agricultural_tasks(id),
    input_batch_id TEXT NOT NULL REFERENCES input_batches(id),
    quantity       NUMERIC(14,3) NOT NULL DEFAULT 0,
    unit           TEXT NOT NULL DEFAULT '',
    applied_by     TEXT REFERENCES users(id),
    applied_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    note           TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_input_usages_task ON input_usages(task_id);

-- dictionary + records for diseases & pests
CREATE TABLE disease_pests (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT REFERENCES tenants(id), -- NULL = public dictionary
    category        TEXT NOT NULL,               -- disease | pest | weed
    name_zh         TEXT NOT NULL,
    name_en         TEXT NOT NULL DEFAULT '',
    scientific_name TEXT NOT NULL DEFAULT '',
    description     TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE disease_records (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL REFERENCES tenants(id),
    plot_id       TEXT REFERENCES plots(id),
    crop_batch_id TEXT REFERENCES crop_batches(id),
    category      TEXT NOT NULL DEFAULT 'disease',
    name_zh       TEXT NOT NULL,
    name_en       TEXT NOT NULL DEFAULT '',
    severity      TEXT NOT NULL DEFAULT 'medium', -- low | medium | high
    location      GEOMETRY(Point, 4326),
    discovered_by TEXT REFERENCES users(id),
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    status        TEXT NOT NULL DEFAULT 'open',   -- open | treating | resolved
    treatment     TEXT NOT NULL DEFAULT '',
    media         JSONB NOT NULL DEFAULT '[]',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    version       INT NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_disease_records_updated BEFORE UPDATE ON disease_records
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_disease_records_tenant ON disease_records(tenant_id, discovered_at DESC);
