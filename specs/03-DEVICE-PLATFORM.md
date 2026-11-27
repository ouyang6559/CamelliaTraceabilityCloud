# 03 农业设备平台

## 1. 目标

建立世界级农业设备抽象层，让不同厂商设备进入同一平台。

```text
CamelliaTrace
       |
 Device Gateway
       |
 +-----+------+--------+--------+
 |            |        |        |
DJI           XAG    MAVLink   ROS2
 |                     |        |
UAV                   UAV/UGV  Robot
```

## 2. DeviceAdapter

> **落地状态（2026-11-22）**：SDK 契约位于 `adapters/device/deviceadapter.go`；
> mission 执行链路已接通适配器分发（`device.Registry`，见 docs/adr/adr-005）——
> 任务 start/pause/resume/stop 会在本地状态提交后经 `adapter_type` 调用对应适配器，
> NATS 广播保留。
>
> 已注册适配器（docs/adr/adr-007）：
> - `generic-mqtt`（P0 参考实现）
> - `mavlink`（P0，TASK-030 ✅：任务上传协议/解锁/任务启动/暂停恢复/RTL，mock 载具回环测试）
> - `rosbridge`（P0 ROS 2，TASK-031 ✅：**Nav2 action 协议**（send_action_goal/
>   action_result 逐航点校验）+ /goal_pose 话题回退（nav_mode="topic"）+ /cmd_vel 停车）
> - `dji-cloud`（TASK-032 骨架 ✅：OSD 遥测 MQTT 接入按公开文档实现 + 回环测试；
>   flighttask 任务操作为诚实 TODO，需企业凭证 + 硬件验证）
> - XAG（TASK-033）待官方凭证/授权（不得伪造），目录占位。

建议接口：

```go
type DeviceAdapter interface {
    Connect(ctx context.Context, device *Device) error
    Disconnect(ctx context.Context, device *Device) error
    GetDeviceInfo(ctx context.Context, device *Device) (*DeviceInfo, error)
    GetCapabilities(ctx context.Context, device *Device) ([]Capability, error)
    GetStatus(ctx context.Context, device *Device) (*DeviceStatus, error)
    GetTelemetry(ctx context.Context, device *Device) (*Telemetry, error)
    SendCommand(ctx context.Context, device *Device, command Command) error
    CreateMission(ctx context.Context, device *Device, mission Mission) error
    StartMission(ctx context.Context, device *Device, missionID string) error
    PauseMission(ctx context.Context, device *Device, missionID string) error
    ResumeMission(ctx context.Context, device *Device, missionID string) error
    StopMission(ctx context.Context, device *Device, missionID string) error
    GetMission(ctx context.Context, device *Device, missionID string) (*MissionStatus, error)
    GetLogs(ctx context.Context, device *Device) ([]DeviceLog, error)
}
```

接口只是初稿，最终以 Proto/API contract 为准。

## 3. Capability

```text
TELEMETRY
GPS
RTK
VIDEO
PHOTO
MISSION
AUTONOMOUS
REMOTE_CONTROL
SPRAY
SPREAD
MOW
PLOW
HARROW
SEED
FERTILIZE
INSPECTION
MAPPING
RETURN_HOME
CHARGE
DOCK
```

设备必须声明 Capability，核心不能假设所有设备能力相同。

## 4. 设备类型

```text
UAV
UGV
TRACTOR
MOWER
WEEDER
SPRAYER
TRANSPORT_ROBOT
HARVEST_ROBOT
WEATHER_STATION
SOIL_SENSOR
CAMERA
GNSS
RTK
GATEWAY
```

## 5. 协议优先级

P0：

- MQTT
- HTTP
- WebSocket
- ROS 2
- MAVLink

P1：

- DJI API/SDK
- XAG API/SDK

P2：

- ISO 11783 / ISOBUS
- ISOXML
- ADAPT

P3：

- 厂商专用协议

## 6. DJI Adapter

必须独立：

```text
adapters/dji/
```

可能的接口层：

- DJI Cloud API
- DJI Mobile SDK
- DJI Payload SDK

具体能力以实际获得的官方 SDK/API 和设备型号为准。

不得伪造接口。

## 7. XAG Adapter

```text
adapters/xag/
```

具体 API、SDK、设备型号和商业授权必须以极飞官方资料为准。

## 8. ROS 2

业务系统不得直接控制电机。

```text
CamelliaTrace
 -> Robot Gateway
 -> ROS 2
 -> Robot Controller
 -> Actuator
```

## 9. MAVLink

统一支持：

- telemetry
- GPS
- mission
- battery
- vehicle state
- geofence

## 10. 安全

以下动作必须：

```text
Mission
 -> Validation
 -> Safety Check
 -> Authorization
 -> Execute
```

高风险动作：

- 起飞
- 喷洒
- 割草
- 耕作
- 自动驾驶
- 远程控制

不得由普通 CRUD API 直接执行。

## 11. Device Simulator

必须提供模拟器：

```text
simulator/
  drone/
  rover/
  tractor/
  mower/
  iot/
```

无真实设备也可以开发和测试。
