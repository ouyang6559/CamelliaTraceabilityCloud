-- 0006_device_iot.sql — device models, devices, telemetry, events, missions, geofences, SensorThings-lite

CREATE TABLE device_models (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id),
    vendor       TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL,
    device_type  TEXT NOT NULL, -- UAV / UGV / TRACTOR / MOWER / SPRAYER / WEATHER_STATION / SOIL_SENSOR / CAMERA / GNSS / RTK / GATEWAY
    capabilities TEXT[] NOT NULL DEFAULT '{}',
    metadata     JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by   TEXT,
    version      INT NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_device_models_updated BEFORE UPDATE ON device_models
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_device_models_tenant ON device_models(tenant_id);

CREATE TABLE devices (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL REFERENCES tenants(id),
    model_id      TEXT REFERENCES device_models(id),
    name          TEXT NOT NULL,
    code          TEXT NOT NULL, -- device serial / sn
    device_type   TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'offline', -- online | offline | error | maintenance
    capabilities  TEXT[] NOT NULL DEFAULT '{}',
    token_hash    TEXT NOT NULL DEFAULT '',        -- sha256(device token)
    last_seen_at  TIMESTAMPTZ,
    farm_id       TEXT REFERENCES farms(id),
    plot_id       TEXT REFERENCES plots(id),
    location      GEOMETRY(Point, 4326),
    battery_level INT,
    firmware      TEXT NOT NULL DEFAULT '',
    metadata      JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by    TEXT,
    updated_by    TEXT,
    version       INT NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);
CREATE TRIGGER trg_devices_updated BEFORE UPDATE ON devices
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_devices_tenant ON devices(tenant_id);
CREATE INDEX idx_devices_location_gix ON devices USING GIST (location);

CREATE TABLE device_telemetry (
    id        TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    device_id TEXT NOT NULL,
    ts        TIMESTAMPTZ NOT NULL,
    topic     TEXT NOT NULL DEFAULT '',
    metrics   JSONB NOT NULL DEFAULT '{}',
    payload   JSONB NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_telemetry_device_ts ON device_telemetry(device_id, ts DESC);
CREATE INDEX idx_telemetry_tenant_ts ON device_telemetry(tenant_id, ts DESC);

CREATE TABLE device_events (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL,
    device_id  TEXT NOT NULL,
    type       TEXT NOT NULL, -- status_change | error | low_battery | geofence_exit | command | ...
    severity   TEXT NOT NULL DEFAULT 'info', -- info | warning | error | critical
    message    TEXT NOT NULL DEFAULT '',
    payload    JSONB NOT NULL DEFAULT '{}',
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_device_events_device ON device_events(device_id, occurred_at DESC);

CREATE TABLE missions (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL REFERENCES tenants(id),
    device_id      TEXT NOT NULL REFERENCES devices(id),
    name           TEXT NOT NULL,
    mission_type   TEXT NOT NULL DEFAULT 'inspect', -- spray | mow | plow | seed | fertilize | map | inspect | transport | harvest
    status         TEXT NOT NULL DEFAULT 'draft',
    -- draft | validation_failed | safety_check | awaiting_confirmation | approved
    -- | executing | paused | completed | failed | cancelled
    risk_level     TEXT NOT NULL DEFAULT 'medium', -- low | medium | high
    route          JSONB NOT NULL DEFAULT '[]',    -- [{lat,lng,alt,action,speed,...}]
    route_geometry GEOMETRY(LineString, 4326),
    planned_start_at TIMESTAMPTZ,
    planned_end_at   TIMESTAMPTZ,
    started_at     TIMESTAMPTZ,
    finished_at    TIMESTAMPTZ,
    progress       NUMERIC(5,2) NOT NULL DEFAULT 0,
    result         JSONB,
    safety_check   JSONB,              -- {passed, checks:[{name,passed,detail}]}
    approved_by    TEXT,
    approved_at    TIMESTAMPTZ,
    created_by     TEXT,
    metadata       JSONB NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    version        INT NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_missions_updated BEFORE UPDATE ON missions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_missions_tenant_status ON missions(tenant_id, status, created_at DESC);
CREATE INDEX idx_missions_device ON missions(device_id);

CREATE TABLE mission_events (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    mission_id  TEXT NOT NULL,
    type        TEXT NOT NULL, -- created | validated | safety_checked | approved | started | paused | resumed | progress | completed | failed | cancelled | rejected
    message     TEXT NOT NULL DEFAULT '',
    payload     JSONB NOT NULL DEFAULT '{}',
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_mission_events_mission ON mission_events(mission_id, occurred_at);

CREATE TABLE geofences (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id),
    farm_id    TEXT REFERENCES farms(id),
    name       TEXT NOT NULL,
    fence_type TEXT NOT NULL DEFAULT 'keep_out', -- keep_out | work_area
    boundary   GEOMETRY(Polygon, 4326) NOT NULL,
    active     BOOLEAN NOT NULL DEFAULT true,
    note       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by TEXT,
    version    INT NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_geofences_updated BEFORE UPDATE ON geofences
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX idx_geofences_tenant ON geofences(tenant_id);
CREATE INDEX idx_geofences_boundary_gix ON geofences USING GIST (boundary);

-- SensorThings-lite
CREATE TABLE sensors (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    device_id   TEXT NOT NULL REFERENCES devices(id),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_sensors_device ON sensors(device_id);

CREATE TABLE datastreams (
    id               TEXT PRIMARY KEY,
    tenant_id        TEXT NOT NULL REFERENCES tenants(id),
    sensor_id        TEXT NOT NULL REFERENCES sensors(id),
    device_id        TEXT NOT NULL REFERENCES devices(id),
    name             TEXT NOT NULL,
    observed_property TEXT NOT NULL, -- temperature / humidity / soil_moisture / npk_n ...
    unit             TEXT NOT NULL DEFAULT '',
    description      TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_datastreams_device ON datastreams(device_id);
CREATE INDEX idx_datastreams_sensor ON datastreams(sensor_id);

CREATE TABLE observations (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    datastream_id   TEXT NOT NULL REFERENCES datastreams(id),
    device_id       TEXT NOT NULL,
    result_number   DOUBLE PRECISION,
    result_string   TEXT,
    result_json     JSONB,
    phenomenon_time TIMESTAMPTZ NOT NULL,
    result_time     TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_observations_stream_time ON observations(datastream_id, phenomenon_time DESC);
CREATE INDEX idx_observations_device_time ON observations(device_id, phenomenon_time DESC);
