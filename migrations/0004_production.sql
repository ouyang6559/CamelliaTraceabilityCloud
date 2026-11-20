-- 0004_production.sql — harvest, raw material, processing, products, packages, warehouse, logistics

CREATE TABLE harvests (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL REFERENCES tenants(id),
    plot_id       TEXT NOT NULL REFERENCES plots(id),
    crop_batch_id TEXT REFERENCES crop_batches(id),
    harvested_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    quantity      NUMERIC(14,3) NOT NULL DEFAULT 0,
    unit          TEXT NOT NULL DEFAULT 'kg',
    operator_id   TEXT REFERENCES users(id),
    device_id     TEXT,
    mission_id    TEXT,
    note          TEXT NOT NULL DEFAULT '',
    media         JSONB NOT NULL DEFAULT '[]',
    status        TEXT NOT NULL DEFAULT 'completed',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by    TEXT,
    version       INT NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_harvests_updated BEFORE UPDATE ON harvests
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_harvests_tenant ON harvests(tenant_id, harvested_at DESC);

CREATE TABLE raw_material_batches (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id),
    code         TEXT NOT NULL,
    harvest_id   TEXT REFERENCES harvests(id),
    plot_id      TEXT REFERENCES plots(id),
    crop_batch_id TEXT REFERENCES crop_batches(id),
    quantity     NUMERIC(14,3) NOT NULL DEFAULT 0,
    unit         TEXT NOT NULL DEFAULT 'kg',
    received_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    status       TEXT NOT NULL DEFAULT 'received', -- received | processing | consumed
    note         TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by   TEXT,
    version      INT NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);
CREATE TRIGGER trg_raw_batches_updated BEFORE UPDATE ON raw_material_batches
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_raw_batches_tenant ON raw_material_batches(tenant_id, created_at DESC);

CREATE TABLE processing_batches (
    id                    TEXT PRIMARY KEY,
    tenant_id             TEXT NOT NULL REFERENCES tenants(id),
    code                  TEXT NOT NULL,
    raw_material_batch_id TEXT NOT NULL REFERENCES raw_material_batches(id),
    process_type          TEXT NOT NULL DEFAULT 'drying', -- drying / shelling / pressing / refining / packing / other
    input_quantity        NUMERIC(14,3) NOT NULL DEFAULT 0,
    output_quantity       NUMERIC(14,3) NOT NULL DEFAULT 0,
    unit                  TEXT NOT NULL DEFAULT 'kg',
    operator_id           TEXT REFERENCES users(id),
    started_at            TIMESTAMPTZ,
    finished_at           TIMESTAMPTZ,
    parameters            JSONB NOT NULL DEFAULT '{}',
    status                TEXT NOT NULL DEFAULT 'in_progress', -- in_progress | completed | failed
    note                  TEXT NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by            TEXT,
    version               INT NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);
CREATE TRIGGER trg_processing_batches_updated BEFORE UPDATE ON processing_batches
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_processing_batches_tenant ON processing_batches(tenant_id, created_at DESC);

CREATE TABLE products (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    name        TEXT NOT NULL,
    code        TEXT NOT NULL DEFAULT '',
    category    TEXT NOT NULL DEFAULT 'camellia-oil',
    spec        TEXT NOT NULL DEFAULT '',
    unit        TEXT NOT NULL DEFAULT 'bottle',
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  TEXT,
    version     INT NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);
CREATE TRIGGER trg_products_updated BEFORE UPDATE ON products
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE product_batches (
    id                   TEXT PRIMARY KEY,
    tenant_id            TEXT NOT NULL REFERENCES tenants(id),
    code                 TEXT NOT NULL,
    product_id           TEXT NOT NULL REFERENCES products(id),
    processing_batch_id  TEXT REFERENCES processing_batches(id),
    produced_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    quantity             NUMERIC(14,3) NOT NULL DEFAULT 0,
    unit                 TEXT NOT NULL DEFAULT 'bottle',
    status               TEXT NOT NULL DEFAULT 'available',
    note                 TEXT NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by           TEXT,
    version              INT NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);
CREATE TRIGGER trg_product_batches_updated BEFORE UPDATE ON product_batches
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_product_batches_tenant ON product_batches(tenant_id, created_at DESC);

CREATE TABLE packages (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id),
    code            TEXT NOT NULL,
    product_batch_id TEXT NOT NULL REFERENCES product_batches(id),
    quantity        NUMERIC(14,3) NOT NULL DEFAULT 1,
    unit            TEXT NOT NULL DEFAULT 'bottle',
    net_weight      NUMERIC(12,3),
    gross_weight    NUMERIC(12,3),
    packed_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    status          TEXT NOT NULL DEFAULT 'stocked', -- stocked | shipped | sold
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      TEXT,
    version         INT NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);
CREATE TRIGGER trg_packages_updated BEFORE UPDATE ON packages
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_packages_tenant ON packages(tenant_id, created_at DESC);
CREATE INDEX idx_packages_batch ON packages(product_batch_id);

CREATE TABLE warehouses (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id),
    name       TEXT NOT NULL,
    code       TEXT NOT NULL DEFAULT '',
    address    TEXT NOT NULL DEFAULT '',
    location   GEOMETRY(Point, 4326),
    capacity   NUMERIC(14,3),
    unit       TEXT NOT NULL DEFAULT 'kg',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by TEXT,
    version    INT NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);
CREATE TRIGGER trg_warehouses_updated BEFORE UPDATE ON warehouses
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE inventory_items (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id),
    warehouse_id TEXT NOT NULL REFERENCES warehouses(id),
    ref_type     TEXT NOT NULL, -- product_batch | package
    ref_id       TEXT NOT NULL,
    quantity     NUMERIC(14,3) NOT NULL DEFAULT 0,
    unit         TEXT NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    version      INT NOT NULL DEFAULT 1,
    UNIQUE (warehouse_id, ref_type, ref_id)
);
CREATE TRIGGER trg_inventory_items_updated BEFORE UPDATE ON inventory_items
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE inventory_movements (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id),
    warehouse_id TEXT NOT NULL REFERENCES warehouses(id),
    ref_type     TEXT NOT NULL,
    ref_id       TEXT NOT NULL,
    direction    TEXT NOT NULL, -- in | out | adjust
    quantity     NUMERIC(14,3) NOT NULL DEFAULT 0,
    unit         TEXT NOT NULL DEFAULT '',
    reason       TEXT NOT NULL DEFAULT '',
    operator_id  TEXT REFERENCES users(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_inventory_movements_tenant ON inventory_movements(tenant_id, created_at DESC);

CREATE TABLE logistics (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id),
    package_id   TEXT REFERENCES packages(id),
    carrier      TEXT NOT NULL DEFAULT '',
    tracking_no  TEXT NOT NULL DEFAULT '',
    origin       TEXT NOT NULL DEFAULT '',
    destination  TEXT NOT NULL DEFAULT '',
    departed_at  TIMESTAMPTZ,
    arrived_at   TIMESTAMPTZ,
    status       TEXT NOT NULL DEFAULT 'pending', -- pending | in_transit | arrived | received
    note         TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by   TEXT,
    version      INT NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_logistics_updated BEFORE UPDATE ON logistics
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
