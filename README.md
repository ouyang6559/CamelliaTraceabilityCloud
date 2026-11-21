<div align="center">

<img src="docs/assets/logo.svg" alt="CamelliaTraceabilityCloud logo" width="88" />

# 茶源智控溯源云 CamelliaTraceabilityCloud

**Open Agriculture Digital Infrastructure / 开放农业数字基础设施**

面向油茶及可扩展农业场景的开源农业数字基础设施平台：统一连接土地、农事、IoT、GIS、无人机、
无人车、农业机器人、农机、AI、生产加工、质量安全和可信溯源。

[![CI](https://github.com/ouyang6559/CamelliaTraceabilityCloud/actions/workflows/ci.yml/badge.svg)](https://github.com/ouyang6559/CamelliaTraceabilityCloud/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

</div>

---

## 核心原则（specs）

| 原则 | 落地 |
|---|---|
| **Core Domain ≠ Vendor Integration** | DJI/XAG/区块链/第三方平台/AI 全部 Adapter 化（`adapters/`），核心领域零厂商污染 |
| **TraceEvent First** | 所有生产活动进入统一 `trace_events`，按租户 SHA-256 哈希链逐事件串联，可验证防篡改 |
| **Device Abstraction First** | `DeviceAdapter` SDK + Capability 声明 + Mission 安全流水线 |
| **Local First** | 本地提交即生效；第三方平台经 NATS 异步同步（PENDING/SYNCING/SYNCED/RETRYING/FAILED），故障不阻塞业务 |
| **Human-in-the-loop** | 高危任务强制 `校验 → 安全检查 → 人工批准 → 执行 → 审计`，AI 无权执行任何动作 |

## 功能总览（Phase 0/1 MVP）

- **身份与多租户**：JWT + 刷新轮换、RBAC 权限目录（~130 权限码）、传输层自动审计
- **农业数字化**：基地/地块（PostGIS 边界）/作物/品种/种植批次/农事任务（完成即溯源）/投入品/病虫害
- **生产加工链**：采收 → 原料批次 → 加工批次 → 产品/产品批次 → 包装 → 仓储/库存 → 物流
- **可信溯源**：哈希链 TraceEvent、一物一码（QR）、溯源链路图、证据（SHA-256）、区块链存证（Mock Provider）
- **设备平台**：设备注册（一次性 Token）、MQTT/HTTPS 双通道遥测、SensorThings-lite（Sensor/Datastream/Observation）、告警规则引擎、电子围栏
- **任务安全流水线**：能力校验 → 围栏/电量安全检查 → 人工批准 → 执行 → 进度 → 完成（自动生成 TraceEvent）
- **外部同步**：TraceProvider SDK + Mock 适配器（国家平台/蚂蚁链/至信链等需官方文档后接入）
- **AI 助手**：OpenAI 兼容 Provider + Mock；输出强制标注 AI 生成 + 人工采纳/驳回
- **实时**：NATS 领域事件 → SSE（`/api/v1/stream`，租户隔离）
- **国际化**：zh-CN / en-US 全量前端

## 快速开始（一键全栈）

```bash
git clone https://github.com/ouyang6559/CamelliaTraceabilityCloud.git
cd CamelliaTraceabilityCloud

# 一键启动：PostgreSQL/PostGIS + Redis + NATS + Mosquitto + API(自动迁移+种子) + 双前端 + 设备模拟器
docker compose -f deployments/docker/docker-compose.yml up -d --build
```

| 服务 | 地址 | 说明 |
|---|---|---|
| 管理台 | http://localhost:5173 | 登录 `admin / admin123`（租户 `demo`） |
| 消费者溯源 | http://localhost:8081/t/{code} | 扫码页（无登录） |
| API | http://localhost:8080 | OpenAPI: `packages/openapi/openapi.yaml` |
| MinIO 控制台（可选） | http://localhost:9001 | `docker compose --profile s3 up -d` |

种子数据包含完整演示链路：湖南油茶示范园 → 3 地块 → 油茶批次 → 农事任务 → 采收 →
压榨加工 → 山茶油 750ml × 900 瓶 → 包装 + 消费者溯源码 → 全链路 TraceEvent 哈希链。

**体验设备闭环**：登录管理台 → 智能设备 → 任务 → 选择种子任务 `北坡巡检+喷洒演示` →
`校验 → 安全检查 → 人工批准 → 下发执行` —— 设备模拟器将自动接收 NATS 指令，按航线飞行
（遥测 + GPS + 进度实时上报），完成后自动生成 `uav_mission` 溯源事件。

## 本地开发

```bash
make build      # 编译 bin/ctc-api + bin/ctc-sim
make up         # docker compose 全栈
make seed       # 单独灌种子数据
make test       # Go 单元测试
make smoke      # 端到端冒烟（tests/e2e/smoke.sh，32 步验收链路）

# 前端热更新
cd apps/admin-web && npm i && npm run dev   # :5173
cd apps/trace-web && npm i && npm run dev   # :5174
```

## 仓库结构

```text
apps/api/          Go 模块化单体（chi + pgx，全部业务模块 internal/modules/*）
apps/admin-web/    React + AntD 管理台（响应式覆盖移动端）
apps/trace-web/    消费者溯源页（无登录）
adapters/device/   Device Adapter SDK（specs/20）
adapters/trace/    TraceProvider 实现（mock；其余待官方文档）
simulator/ctc-sim  设备模拟器（气象站/土壤传感器/无人机/割草机器人）
migrations/        PostgreSQL/PostGIS 全量 SQL（嵌入式自动迁移）
packages/          openapi.yaml / traceprovider SDK / mqttclient
deployments/       docker compose + Dockerfile + nginx
specs/             完整规格文档（00~24）
tests/e2e/         冒烟测试脚本
```

## 测试

- `make test` — 单元测试：哈希链确定性/GeoJSON round-trip、RBAC 通配、任务能力映射、告警比较、Geo 距离等
- `make smoke` — E2E：登录 → 基地/地块 → 批次 → 任务 → 采收 → 加工 → 产品 → QR → 公共溯源 →
  任务安全流水线（含"未批准不可启动"断言） → 遥测/观测 → 告警 → 存证 → 哈希链验证 → 同步 → AI → 审计

## Roadmap

Phase 0/1（已完成，含**真机执行链路** + **SaaS 多租户**：自助注册/平台管理/隔离加固）
→ ✅ **Phase 3 设备适配器**：`mavlink`（ArduPilot/PX4，mock 载具回环测试）、
`rosbridge`（ROS 2 无人车，rosbridge 协议测试）已实现；DJI 上云API（需企业凭证）、
XAG（需商业授权）为官方对接项（[ADR-007](docs/adr/adr-007-device-adapters-phase3.md)）
→ Phase 2 IoT 增强 → Phase 4 遥感（WebODM/STAC/NDVI） → Phase 5 官方溯源平台接入 → Phase 6 AI（RAG/Vision）。
实现任一厂商适配器 = 实现 `device.Adapter` 接口 + 注册一行，核心零改动（[ADR-005](docs/adr/adr-005-mission-device-dispatch.md)）。
详见 [specs/14-ROADMAP.md](specs/14-ROADMAP.md) 与 [specs/24-PHASE0-IMPLEMENTATION-DECISIONS.md](specs/24-PHASE0-IMPLEMENTATION-DECISIONS.md)。

## 文档

- [specs/README.md](specs/README.md) — 25 份规格文档索引
- [packages/openapi/openapi.yaml](packages/openapi/openapi.yaml) — API 契约
- [docs/adr/](docs/adr) — 架构决策记录

## 许可证

[Apache License 2.0](LICENSE)
