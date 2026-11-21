# DJI Adapter（上云 API）— Integration TODO

本目录预留给大疆 DJI Cloud API 适配器（specs/03.6、TASK-032）。

**当前状态**：未实现。DJI 消费级无人机（Mavic 系列）不对第三方平台开放控制接口；
仅企业产品线（DJI 机场 Dock、经纬 M300/M350 RTK 等）通过 **DJI Cloud API** 支持第三方
云平台接入。接口依据：https://developer.dji.com/doc/cloud-api-tutorial/en/
（公开文档：MQTT 协议、设备管理/直播/航线任务/媒体管理）。

specs/03.6 明确「不得伪造接口」：真实能力以实际获得的官方 SDK/API 和设备型号为准。

## 实现前置条件

1. DJI 开发者企业账号（App ID / App Key / App Secret）——配置位已预留：
   `CTC_DJI_APP_ID` / `CTC_DJI_APP_KEY` / `CTC_DJI_APP_SECRET`（apps/api config）。
2. EMQX/MQTT 集群（DJI Cloud API 基于 MQTT：thing/product/{sn}/... 主题族）。
3. 可联调的真实设备（Dock 或带 Pilot 的经纬系列）。

## 实现要求（对接后）

- 包名 `package dji`，实现 `adapters/device` 的 `device.Adapter` 接口。
- 在 `apps/api/internal/app/app.go` 注册：`deviceRegistry.Register("dji-cloud", ...)`。
- 技术要点（依官方文档）：MQTT 网关、OSD 遥测解析、航线管理（wayline job）、
  设备属性上报、TSA 服务——全部按官方文档实现，不编造。

参考：docs/adr/adr-005（分发链路）、docs/adr/adr-007（MAVLink/rosbridge 先例）。
