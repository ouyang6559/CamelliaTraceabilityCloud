# 19 Zcode Master Prompt

你正在开发 **茶源智控溯源云 CamelliaTraceabilityCloud**。

项目目标不是开发一个普通油茶 ERP，而是开发一个世界级、开源、可扩展、标准优先的农业数字基础设施平台。

你必须遵循 `/specs` 下全部 SPEC。

## 第一原则

```text
Core Domain != Vendor Integration
```

DJI、XAG、机器人、拖拉机、区块链、第三方溯源平台、AI Provider 都必须 Adapter 化。

## 第二原则

```text
TraceEvent First
```

生产活动必须进入统一 TraceEvent。

## 第三原则

```text
Device Abstraction First
```

设备通过 DeviceAdapter + Capability + Mission 接入。

## 第四原则

```text
Local First
```

本地核心数据先提交，第三方平台异步同步。

## 第五原则

```text
Standard First
```

优先采用：

- OGC
- SensorThings
- STAC
- GeoJSON
- GeoPackage
- GeoTIFF/COG
- ADAPT
- ISO 11783 / ISOBUS
- ROS 2
- MAVLink
- OpenAPI
- gRPC
- MQTT
- OpenTelemetry

## 第六原则

高风险设备操作必须：

```text
Validation
 -> Safety Check
 -> Authorization
 -> Human Confirmation
 -> Execution
 -> Audit
```

## 技术栈

后端：

- Go
- go-kratos
- Ent
- PostgreSQL
- PostGIS
- Redis
- NATS
- MQTT

前端：

- React
- TypeScript
- Vite
- Ant Design
- TanStack Query
- Zustand
- ECharts
- MapLibre

## 开发方法

禁止一次性生成整个系统。

必须：

1. 阅读 SPEC
2. 检查代码
3. 分析依赖
4. 修改数据库
5. 修改 Proto
6. 生成 API/SDK
7. 实现 Domain
8. 实现 Application
9. 实现 Infrastructure
10. 实现 Frontend
11. 增加测试
12. 更新文档
13. lint
14. test
15. build

## 第三方 API

没有官方文档时：

```text
Interface
+
Mock
+
TODO
```

绝对不能猜 API。

## 完成标准

每个功能必须同时具备：

- 数据模型
- Proto
- API
- 权限
- 审计
- 后端
- 前端
- 测试
- 文档

## 第一任务

先输出：

1. 当前代码库结构与现状分析
2. 与本 SPEC 的差异
3. 第一阶段实施计划
4. 数据库迁移计划
5. Proto 目录设计
6. 模块依赖图

不要立即生成大量业务代码。

等待架构确认后，再开始 TASK-001。
