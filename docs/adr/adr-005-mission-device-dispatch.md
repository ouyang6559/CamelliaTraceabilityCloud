# ADR-005 Mission 设备指令分发（真机执行链路）

- 状态：已接受（2026-11-21）
- 关联：specs/03（设备平台）、specs/19（第六原则）、ADR-003（安全流水线）

## 背景

Phase 0/1 的任务执行是事件驱动模拟：API 批准后发 NATS `mission.started`，仅 `ctc-sim`
消费并回传进度。`adapters/device` 的 DeviceAdapter SDK 没有任何调用方——真机接入缺少
"最后一公里"：核心系统不知道该把指令发给谁、用哪个协议。

## 决策

1. **注册表分发**：`device.Registry`（`apps/api/internal/modules/device/registry.go`）
   维护 `adapter_type → sdk.Adapter` 映射。设备表新增 `adapter_type` 列决定路由；
   核心代码（mission 引擎、命令 API）只按类型分发，不 import 任何厂商包。
2. **双通道并存**：任务生命周期动作（start/pause/resume/stop）在本地状态提交后——
   - NATS `mission.started` 等领域事件继续广播（模拟器、外部网关通道）；
   - 同时经注册表调用适配器直发真机（`CreateMission/StartMission/...`）。
   两条通道共用同一信封（mission_id/device_id/mission_type/route），单一事实源。
3. **Local First**（specs/01.7）：分发失败不回滚已提交的本地状态，写
   `mission_events(type=dispatch_failed)` 并发 `mission.updated` 供人工重试/排查。
4. **命令白名单**：`POST /devices/{id}/commands` 仅允许非高危命令
   （ping/locate/sync_logs/led/charge），高危动作（起飞/喷洒/割草/耕作/自动驾驶/远程控制）
   一律 400 并指向 mission 流水线（specs/03.10，ADR-003）。
5. **凭证位**：config 增加 DJI Cloud API 凭证占位（`CTC_DJI_APP_*`），凭证为空则该
   适配器不注册——严格遵守 specs/03.6「不得伪造接口」与 specs/06.11。

## 现实接入路线（Phase 3）

| 路线 | 接口依据 | 联调方式 | 备注 |
|---|---|---|---|
| MAVLink | 开放协议（gomavlib） | ArduPilot SITL 仿真，可完全真实测试 | 覆盖 ArduPilot/PX4 生态飞控与无人车（ArduRover） |
| ROS 2 rosbridge | 公开 JSON-over-WebSocket 协议 | rosbridge_suite 容器 + 机器人仿真 | 农业无人车/机器人主流内部栈 |
| DJI 上云API | 官方公开文档（developer.dji.com） | 需企业账号（Dock/经纬系列）App Key/Secret | 消费级 Mavic 系列不对第三方开放控制 |
| XAG 极飞 | 商业授权私有 API | Interface + TODO | 待官方授权 |

实现任一厂商适配器 = 新建 `adapters/device/<type>/` 实现 `device.Adapter` 接口 +
在 app.go `Register("<type>", …)` 一行，核心零改动。

## 后果

- 换/加厂商设备不再触碰核心域；执行审计完整（mission_events 全量留痕 + transport 审计）。
- 分发为同步调用但容错（超时由适配器内部控制）；后续可演进为 outbox 重投递。
