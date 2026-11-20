# Changelog

本项目的所有重要变更记录。格式遵循 Keep a Changelog，版本遵循 SemVer。

## [0.1.0] - 2026-11-21

### Added — Phase 0/1 MVP（首个可运行版本）

**平台基础**
- Go 模块化单体 `ctc-api`（chi v5 + pgx v5 + PostgreSQL/PostGIS + Redis + NATS + MQTT）
- 嵌入式 SQL 迁移（0001~0008），启动即自动建库
- 多租户 + JWT（access 30min / refresh 7d 轮换）+ RBAC 权限目录（~130 权限码，内置 tenant_admin/operator/viewer）
- 统一错误信封、分页、传输层审计（写操作自动落 audit_logs，敏感字段脱敏）、Redis 限流、CORS、SSE 实时事件（NATS 驱动，租户隔离）
- 设备模拟器 `ctc-sim`（气象站/土壤传感器 MQTT 遥测；无人机/割草机器人 NATS 任务自动执行）

**农业数字化**
- 基地/地块（PostGIS polygon 边界 + GeoJSON 端点）/作物/品种/种植批次
- 农事任务（开始/完成流转，完成自动生成 TraceEvent + 执行记录）
- 投入品/批次/领用（自动扣减库存）、病虫害记录 + 字典

**生产加工与可信溯源**
- 采收 → 原料批次 → 加工批次 → 产品/产品批次 → 包装 → 仓储库存 → 物流 全链路
- TraceEvent 按租户 SHA-256 哈希链（advisory lock 串行化），`/trace-events/verify-chain` 完整性验证
- 一物一码：批量铸造 CTC-U-xxx 消费者码、扫码计数、公共溯源页（产品/产地/时间线/证据/验证）
- 证据（SHA-256 + 对象重哈希校验）、区块链存证 Provider（Mock 确定性账本，仅存 hash+id 不上链原始数据）
- 溯源链路图（Package→ProductBatch→Processing→RawMaterial→Harvest→CropBatch→Plot）

**设备与 IoT**
- 设备型号/注册（一次性 Token + 轮换）、能力声明、MQTT+HTTPS 双通道遥测摄取
- SensorThings-lite：Sensor/Datastream/Observation（遥测自动注册），时间序列查询
- 告警规则引擎（阈值/去重/生命周期）+ 种子规则
- 电子围栏（keep_out/work_area），Mission 安全引擎（围栏穿越/电量/航线检查）
- Mission 状态机：draft→validation_failed|safety_check→awaiting_confirmation→approved→executing→paused|completed|failed|cancelled，人工批准（mission:approve 权限）强制 human-in-the-loop
- 设备侧进度上报（device-gateway），完成自动生成 uav_mission/robot_mission TraceEvent

**外部集成与 AI**
- TraceProvider SDK（packages/traceprovider）+ Mock 适配器 + 同步任务队列（重试/退避/dead-letter/日志）
- AI Provider 抽象（Mock + OpenAI 兼容），输出强制 output_type 标注 + confidence + 人工采纳/驳回

**前端**
- `ctc-admin-web`：30+ 业务页面（登录/工作台/地图/任务/任务流水线/溯源码 QR/设备地图/AI 对话/用户角色/审计），zh-CN/en-US 国际化
- `ctc-trace-web`：消费者扫码溯源页（时间线/证据图/存证验证）

**工程**
- docker compose 一键全栈（含自动迁移+种子+模拟器），nginx 反代
- OpenAPI 契约（packages/openapi/openapi.yaml）
- 单元测试（哈希链确定性/GeoJSON round-trip/RBAC/能力映射/告警比较）
- E2E 冒烟 `tests/e2e/smoke.sh`：32 步覆盖 specs/17 MVP 验收标准
- GitHub Actions CI（backend/web×2/compose 校验）
- Apache-2.0 LICENSE、CONTRIBUTING、完整 specs 文档（00~24）
