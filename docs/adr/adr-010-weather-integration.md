# ADR-010 Phase 2 IoT 增强：气象接入

- 状态：已接受（2026-11-28）
- 关联：specs/04.5（气象）、specs/06.10/06.11（外部接口纪律）、ADR-006

## 决策

### 1. Provider 抽象
- `WeatherProvider` 接口：`Current` + `Forecast`，覆盖 specs/04.5 要求的全部指标
  （温度/湿度/降水/风速/风向/气压；光照留待设备端 SDK 输出）。
- 两个实现：
  - **openmeteo**（默认）：调用 https://api.open-meteo.com/v1/forecast 公开接口——
    免费无密钥、文档完整（https://open-meteo.com/en/docs），符合 specs/06「官方接口」。
    WMO weather_code 映射为中文短标签。
  - **mock**：确定性数值（CI/离线演示），冒烟用 `provider` 无关的端点断言不依赖外网。
- 配置：`CTC_WEATHER_PROVIDER`（openmeteo | mock）。

### 2. 缓存
- `CachedProvider` 内存 TTL 包装（当前 15 分钟）：工作台/AI 轮询不重复打外网。
  单节点单体下足够；多节点部署可平移到 Redis（接口不变）。

### 3. API（iot 模块）
- `GET /weather/current?lat=&lng=` 或 `?farm_id=`（农场位置/边界质心解析，租户归属校验）
- `GET /weather/forecast?...&days=1..16`
- 顺带修复 ADR-006 审计遗留：/sensors、/datastreams、/observations 补上
  `observation:read` 权限中间件（此前仅认证无授权）。

### 4. AI 对话集成
- `POST /ai/chat` 增 `use_weather` + `farm_id`：后端解析农场位置 → 取当前天气 →
  `[实时气象]` 上下文块注入 prompt（Mock/OpenAI 兼容 Provider 一致生效），响应携带
  `weather` 快照供前端展示。与 use_knowledge 正交，可同时开启。

## 后果
- 无外部依赖即可演示（mock）；生产环境默认 openmeteo 免费可用。
- 已知折衷：内存缓存非分布式；Open-Meteo 为全球尺度数据（农场微气候靠本地
  气象站设备的遥测数据，本模块与其互补而非替代）；小时级曲线未暴露（日级够 MVP）。
