# 20 Device Adapter SDK

## 目标

让第三方设备厂商、机器人开发者和社区开发者能够独立开发插件，而不修改核心业务。

## Adapter 生命周期

```text
discover
register
authenticate
connect
sync
telemetry
mission
disconnect
```

## Adapter Manifest

```yaml
name: example-device
version: 1.0.0
type: device
vendor: example
protocol:
  - mqtt
capabilities:
  - telemetry
  - gps
  - mission
```

## 标准对象

```text
DeviceInfo
DeviceStatus
Telemetry
Capability
Mission
MissionStatus
Command
DeviceLog
```

## Vendor Metadata

允许：

```json
{
  "vendor": "example",
  "model": "X1",
  "firmware": "1.2.3"
}
```

但核心业务不得依赖 vendor metadata。

## SDK 要求

必须提供：

- Go SDK
- TypeScript types
- Adapter example
- Simulator example
- Integration test template
- Security checklist
