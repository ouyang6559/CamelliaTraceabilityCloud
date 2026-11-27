# Changelog

本项目的所有重要变更记录。格式遵循 Keep a Changelog，版本遵循 SemVer。

## [Unreleased]

### Added — Phase 2 IoT 增强：气象接入（ADR-010）
- `WeatherProvider` 抽象：Open-Meteo 公开接口实现（免费无密钥，specs/04.5 全指标）
  + 确定性 mock；内存 TTL 缓存（15 分钟）
- `GET /weather/current` / `GET /weather/forecast`：lat/lng 或 farm_id
  （农场位置/边界质心解析 + 租户归属校验）
- AI 对话 `use_weather` + `farm_id`：实时气象上下文注入 prompt，响应携带快照
- 修复审计遗留：/sensors、/datastreams、/observations 补 `observation:read` 授权
- 前端「智能农业 → 气象」页：基地选择/坐标、当前天气卡、7 日预报表 + 温度/降水图
- 冒烟新增气象段（mock provider 断言，不依赖外网）

### Added — Phase 6 AI 深化：知识库 RAG-lite（ADR-009）
- `knowledge_docs` + `knowledge_chunks`（租户隔离），粘贴/.txt/.md 导入，
  段落优先分块（≤400 rune 滑窗 + 40 重叠）
- Go 层检索评分：查询分词（拉丁词 + CJK 二元组）→ 分块计分 → 每文档 Top 命中
  → Top-4 注入对话上下文（诚实标注 RAG-lite；pgvector 为后续升级）
- `POST /ai/chat` 增 `use_knowledge`：回答携带 `sources[]` 引用（可追溯，specs/07.7）
- 前端 AI 助手改 Tabs：对话（知识库开关 + 来源标签）/ 知识库管理（导入/删除）/
  任务记录
- 冒烟新增 RAG 段：导入施肥规程 → use_knowledge 对话 → 断言回答引用来源

### Added — Phase 4 遥感基线：STAC 目录 + 影像任务管线（ADR-008）
- `remote_sensing_jobs` + 后台 Worker：queued→running→succeeded/failed 状态机、
  进度上报、NATS `remote-sensing.*` 事件
- `Processor` 接口：mock（确定性演示处理器）+ NodeODM 真实客户端（公开 API，
  无实例时诚实失败，specs/06.10/06.11）
- STAC-lite 资产目录：OGC STAC 1.0.0 Item JSON 按租户/collection 存储，
  `GET /stac/collections`、items 列表/详情、手动导入外部正射影像
- API 增权限码 `remote-sensing:read/create/update`（operator 含 read/create）
- 前端「智能农业 → 遥感」页：任务列表/上传创建/STAC 目录浏览（中英双语）
- 冒烟新增遥感段：文件上传 → 建任务 → worker 处理 → STAC 资产断言

### Added — Phase 3 设备适配器：MAVLink + ROS 2（ADR-007）
- **MAVLink adapter**（`adapters/device/mavlink`，gomavlib v4）：ArduPilot/PX4
  生态真机接入——遥测（GPS/高度/电池/解锁）、任务上传完整协议
  （MISSION_COUNT→REQUEST_INT→ITEM_INT→ACK）、解锁+任务启动、暂停/恢复
  （DO_PAUSE_CONTINUE）、停止（RTL）、命令映射（ping/locate/return_home/set_mode）
- **ROS 2 rosbridge adapter**（`adapters/device/rosbridge`）：rosbridge v2 JSON
  over WebSocket——订阅 /odom、/battery_state，航点逐个发布 PoseStamped 到
  /goal_pose（Nav2 消费），/odom 到点自动推进，暂停=零速度 Twist
- **协议级回环测试**：mock MAVLink 载具（UDP 真实帧：任务上传握手/解锁/ACK）
  与 mock rosbridge_server（WebSocket）：无需真机即可 CI 验证
- registry 注册 `mavlink`/`rosbridge` 适配器；`adapters/dji`、`adapters/xag`
  目录占位（官方凭证/授权后接入，specs/03.6/06.11）

## [Unreleased]

### Added — SaaS 多租户补全（ADR-006）
- 平台管理员模型（`users.is_platform_admin` + 系统租户 `platform` + JWT `pa` claim）
- 租户生命周期：`ProvisionTenant` 事务化开户；公开自助注册 `POST /auth/register`
  （`CTC_REGISTRATION_ENABLED` 开关）；平台端 `GET/POST /platform/tenants`、
  `PATCH /platform/tenants/{id}`（停用/启用）、`POST /platform/tenants/{id}/reset-admin-password`
- 登录多租户歧义修复：密码命中多租户返回 409 `MULTI_TENANT_AMBIGUOUS` + 租户选项（先验密防枚举）
- 停用租户在登录/refresh 即刻拒绝（存量 token ≤30min 窗口，ADR-006）
- 隔离加固：公开溯源媒体绑定 active trace_code、`PATCH /users/{id}` 租户归属校验、
  `loadPerms` 角色租户过滤、`/sync-tasks/run` 租户化、新增 `pkg/tenantref` 覆盖 10 处
  创建接口的跨租户引用注入
- 前端：登录租户选择弹窗、自助注册表单、顶栏租户徽标、平台管理页、`/tenant` 租户信息页
- 冒烟新增多租户隔离段（跨租户读取/写入/铸造/停用/登录歧义断言）

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
