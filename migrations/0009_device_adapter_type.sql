-- 0009_device_adapter_type.sql — vendor adapter routing for device command dispatch.
-- adapter_type selects the DeviceAdapter implementation in adapters/device/<type>/:
-- '' = none (ingest-only device, e.g. telemetry sensors), 'generic-mqtt' = reference
-- adapter. Future: 'dji-cloud' (requires official DJI credentials, specs/03.6),
-- 'mavlink', 'rosbridge' (ROS 2), ... — see docs/adr/adr-005.
ALTER TABLE devices ADD COLUMN IF NOT EXISTS adapter_type TEXT NOT NULL DEFAULT '';

-- generic-mqtt is the only adapter currently available; backfill so existing
-- controllable devices (UAV/MOWER/...) dispatch through it.
UPDATE devices SET adapter_type = 'generic-mqtt'
WHERE adapter_type = '' AND device_type IN
    ('UAV', 'UGV', 'TRACTOR', 'MOWER', 'WEEDER', 'SPRAYER',
     'TRANSPORT_ROBOT', 'HARVEST_ROBOT', 'GATEWAY');
