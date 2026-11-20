# 24 Phase 0/1 实施决策（ADR）

> 本文档记录第一阶段落地的关键技术决策。所有决策以"可运行、可演进、不伪造"为最高优先级，
> 与 01/19 中"标准优先、Adapter 解耦、模块化单体"的原则保持一致。

## ADR-001 HTTP 框架：chi + pgx，而非 go-kratos + Ent

**决策**：Phase 0/1 后端采用 `net/http` + chi v5 路由 + pgx v5 直连 PostgreSQL（手写 SQL 迁移），
JWT 鉴权，slog 日志。

**理由**：

1. go-kratos 以 Proto/protoc 代码生成为中心，第一版若强行引入会显著增加构建链复杂度（protoc、
   buf、多套插件），降低"每个阶段可运行"的达成率。
2. Ent 代码生成覆盖约 60 张表时维护成本高，手写迁移 SQL + pgx 更透明，PostGIS 能力
   （geometry 函数、空间索引）可直接使用。
3. chi 是标准库风格的最小路由器，后续向 go-kratos/proto-first 迁移时路由与 handler 层可被
   生成代码替换，domain/service 层不受影响。

**迁移路径**：Phase 2+ 引入 Buf + Proto，按模块逐个替换 HTTP 层（Proto -> go-kratos HTTP/gRPC
stub），OpenAPI 由 Proto 生成；此前以手写 `packages/openapi/openapi.yaml` 作为 API 契约。

## ADR-002 模块化单体布局

物理目录允许少于逻辑领域（见 18），采用：

```text
apps/api/internal/modules/<module>   # router.go / handlers.go / service.go / store.go
adapters/device/...                  # DeviceAdapter 实现（generic-mqtt 等）
adapters/trace/...                   # TraceProvider 实现（mock 等）
simulator/ctc-sim                    # 设备模拟器 CLI
```

单一 Go module（`github.com/ouyang6559/CamelliaTraceabilityCloud`），编译产物：

- `ctc-api`（API 单体）
- `ctc-sim`（设备模拟器）

## ADR-003 事件与实时通道

- NATS 承载领域事件（`agri.*` / `device.*` / `trace.*` / `alert.*` / `sync.*` / `ai.*`），
  与 01.6 一致。
- 遥测接入双通道：MQTT（模拟器/真实网关）与 HTTPS device-gateway；两者统一进入
  telemetry ingest pipeline（校验 -> 存储 -> 规则告警 -> NATS）。
- 前端实时使用 SSE（`/api/v1/stream`），订阅 NATS 转发；WebSocket 后续按需增加。

## ADR-004 ID / 时间 / 单位

- 主键：ULID（字符串存储，有序可索引），与 09.3 一致。
- 时间：数据库 UTC，API ISO-8601。
- 空间：EPSG:4326，geometry 列 + `ST_AsGeoJSON`/`ST_GeomFromGeoJSON` 交换。

## ADR-005 高危任务安全流水线

Mission 状态机强制：

```text
DRAFT -> VALIDATION -> SAFETY_CHECK -> AWAITING_CONFIRMATION
      -> APPROVED -> EXECUTING -> (PAUSED) -> COMPLETED / FAILED / CANCELLED
```

`APPROVE` 必须显式人工调用（human-in-the-loop），AI Provider 无权执行该动作（07.8）。
Geofence 越界、低电量、RTK 丢失在 Safety Check 中校验（02/13）。

## ADR-006 第三方平台

严格遵循 06：未取得官方文档的平台只提供 `Interface + Mock Adapter + Integration TODO`，
不编造 endpoint。Mock Adapter 明确标记为 `mock`，产出确定性结果用于联调与演示。

## ADR-007 Phase 0/1 交付范围

| 交付物 | 说明 |
|---|---|
| `ctc-api` | 全部 MVP 模块：identity/agriculture/production/trace/evidence/device/mission/iot/alert/quality/sync/ai/audit/dashboard/files |
| `ctc-admin-web` | PC 管理台（响应式，覆盖 Mobile H5 关键页：今日任务/扫码/上报） |
| `ctc-trace-web` | 无登录消费者溯源页 |
| `ctc-sim` | 无人机 / 无人车 / 土壤传感器 / 气象站模拟器 |
| `deployments/docker` | `docker compose up -d` 一键启动全栈 |
| seed | 演示租户全链路数据（基地->地块->批次->任务->采收->加工->产品->QR->溯源） |

## ADR-008 密码与设备凭证

- 用户密码：bcrypt。
- 设备凭证：创建设备时生成一次性显示的 token，仅存 SHA-256 摘要。
- 所有 secret 通过环境变量/配置文件注入，不入库源码（08.7）。
