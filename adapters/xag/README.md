# XAG（极飞）Adapter — Integration TODO

本目录预留给极飞（XAG）农业设备适配器（specs/03.7、TASK-033）。

**当前状态**：未实现。specs/03.7 与 specs/06.11 明确：具体 API、SDK、设备型号与
商业授权必须以极飞官方资料为准，**不得伪造接口**。极飞上云私有 API 需要企业授权
（AppCode 等），未取得前仅保留本占位。

## 实现前置条件

1. 极飞农业开放平台企业账号与授权（AppCode/证书）。
2. 官方 API 文档（设备鉴权、遥测上报、航线任务下发、电子围栏等模块）。
3. 目标设备型号清单（P 系列/ V 系列无人机、A 系列无人车等）及固件版本。

## 实现要求（对接后）

- 包名 `package xag`，实现 `adapters/device` 的 `device.Adapter` 接口。
- 在 `apps/api/internal/app/app.go` 注册：`deviceRegistry.Register("xag", ...)`
  （凭证从 `CTC_XAG_*` 环境变量注入，specs/08.7）。
- 单元测试以官方 API 文档为准；没有官方文档的模块保持 TODO，不得编造。

参考：docs/adr/adr-005（分发链路）、docs/adr/adr-007（MAVLink/rosbridge 先例）。
