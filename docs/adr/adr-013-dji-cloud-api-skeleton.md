# ADR-013 DJI 上云API 适配器骨架

- 状态：部分实现（遥测骨架已落地，任务操作为诚实 TODO）（2026-11-28）
- 关联：specs/03.6（DJI Adapter 要求）、specs/06.11（禁止伪造）、ADR-005

## 决策

### 已实现（按公开文档 https://developer.dji.com/doc/cloud-api-tutorial/en/）
- MQTT 主题约定：`thing/product/{gateway_sn}/osd`（遥测 ~2Hz）、
  `/services`（服务请求）——topic 构造由测试钉死。
- OSD 信封解析（防御性子集，字段随机型变化）：经纬度/相对高度位置、
  battery.capacity_percent、mode_code。
- Connect 订阅 OSD 主题（Endpoint=DJI 侧 MQTT broker，Token=MQTT 密码，
  Metadata["username"]=MQTT 用户——specs/08.3/08.7 凭证走配置）。
- GetTelemetry/GetStatus/GetDeviceInfo；SendCommand 支持 ping（OSD 存活）、
  locate、return_home（文档信封）。
- 真实 MQTT 回环测试：向 `thing/product/{sn}/osd` 发布文档形 OSD 帧，
  端到端断言位置/电量/状态（本机 mosquitto；CI 加 mosquitto service，无 broker 时 SKIP）。

### 明确未实现（诚实 TODO，specs/03.6/06.11）
flighttask 航线任务、DRC 飞控通道、直播、媒体管理——全部需要：
1. DJI 开发者企业账号（App ID/Key/Secret，配置位已留 `CTC_DJI_APP_*`）；
2. EMQX 集群与云后端对接；
3. 真实设备（Dock 或带 Pilot 的经纬系列）验证。
对应方法返回可操作的 TODO 错误而非伪造调用。

## 后果
- 设备 metadata 填 adapter_type=dji-cloud + Endpoint/凭证即可接入 OSD 遥测，
  任务操作在硬件验证后按官方文档补齐。
