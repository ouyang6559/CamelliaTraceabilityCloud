# ADR-001 模块化单体与 Phase 0 技术栈

- 状态：已接受（2026-11-21）
- 关联：specs/24-PHASE0-IMPLEMENTATION-DECISIONS.md（ADR-001/002）

## 背景

specs 原始技术栈为 go-kratos + Ent + Proto 优先。go-kratos 以 protoc 代码生成为中心，
在阶段 0 引入会显著提高构建链复杂度（protoc/buf/多插件），与"每个阶段可运行"目标冲突。

## 决策

Phase 0/1 采用：

- `net/http` + chi v5 路由（标准库风格，最小依赖）
- pgx v5 直连 PostgreSQL/PostGIS，手写幂等 SQL 迁移（嵌入式，启动自动应用）
- slog 日志、golang-jwt v5、oklog/ulid v2、paho.mqtt.golang、nats.go、go-redis、minio-go

领域模块划分于 `apps/api/internal/modules/*`，与 specs/01 的逻辑领域一一对应；
HTTP 层可替换，domain/service 层不受影响。

## 后果

- 优点：每个 Phase 端到端可运行；PostGIS 能力直接可用；新人上手成本低。
- 代价：OpenAPI 为手工维护（packages/openapi/openapi.yaml）。
- 迁移路径：Phase 2+ 引入 Buf/Proto 后按模块替换 HTTP 层并生成 OpenAPI。
