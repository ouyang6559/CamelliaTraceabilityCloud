# 10 数据库设计

## 1. 数据库

PostgreSQL + PostGIS。

## 2. 核心表

```text
tenants
organizations
users
roles
permissions

farms
plots
crops
varieties
plants

agricultural_tasks
agricultural_task_records

inputs
input_batches
input_usages

diseases
pests
disease_records

devices
device_models
device_capabilities
device_telemetry
device_events

missions
mission_routes
mission_waypoints
mission_events

sensors
datastreams
observations

harvests
raw_material_batches
processing_batches
products
product_batches
packages

quality_tests
quality_reports

warehouses
inventory
logistics

trace_codes
trace_events
evidence
blockchain_proofs

external_platforms
external_adapters
sync_tasks
sync_logs

alerts
notifications

ai_jobs
ai_results
ai_audits

audit_logs
```

## 3. 所有业务表

建议具备：

```text
id
tenant_id
created_at
updated_at
created_by
updated_by
version
```

是否需要 deleted_at 根据实体类型决定。

## 4. 空间字段

使用 PostGIS：

```text
geometry
geography
```

不要把空间数据仅保存成字符串。

## 5. JSON

JSONB 仅用于：

- vendor metadata
- device metadata
- flexible attributes
- external payload
- AI metadata

核心关系字段必须结构化。

## 6. Telemetry

第一阶段可以 PostgreSQL。

规模增长后再考虑：

- TimescaleDB
- ClickHouse

不要第一版引入不必要复杂度。
