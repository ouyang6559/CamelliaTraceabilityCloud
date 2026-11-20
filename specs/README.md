# 茶源智控溯源云 CamelliaTraceabilityCloud

> Open Agriculture Digital Infrastructure / 开放农业数字基础设施

本目录是 **茶源智控溯源云 CamelliaTraceabilityCloud** 的完整规格文档（SPEC）集合，面向 Zcode / AI Coding Agent 进行分阶段开发。

## 项目定位

CamelliaTraceabilityCloud 不是单纯的油茶种植管理软件，而是：

- 农业生产数字化
- GIS / 农业数字孪生
- IoT / 传感器
- 无人机
- 无人车
- 农业机器人
- 农机
- 农事任务
- AI 农业助手
- 质量安全
- 一物一码
- 可信溯源
- 区块链存证
- 第三方溯源平台适配
- 开放设备生态

组成的开放农业数字基础设施平台。

## 核心原则

1. Open Source First
2. API First
3. Standard First
4. Local First
5. Device Abstraction First
6. TraceEvent First
7. Human-in-the-loop for high-risk operations
8. Cloud + Edge
9. Modular Monolith First
10. 国际标准优先、厂商 Adapter 解耦

## 建议开源许可证

Apache License 2.0。

## 文档阅读顺序

1. `00-PROJECT-OVERVIEW.md`
2. `01-ARCHITECTURE.md`
3. `02-DOMAIN-MODEL.md`
4. `03-DEVICE-PLATFORM.md`
5. `04-AGRICULTURE-GIS-IOT.md`
6. `05-TRACEABILITY.md`
7. `06-EXTERNAL-INTEGRATIONS.md`
8. `07-AI-PLATFORM.md`
9. `08-SECURITY.md`
10. `09-API-CONVENTIONS.md`
11. `10-DATABASE-SCHEMA.md`
12. `11-FRONTEND-MOBILE.md`
13. `12-DEPLOYMENT-EDGE.md`
14. `13-TESTING-QA.md`
15. `14-ROADMAP.md`
16. `15-ZCODE-IMPLEMENTATION.md`
17. `24-PHASE0-IMPLEMENTATION-DECISIONS.md`（第一阶段落地决策）

## 重要开发约束

- 不伪造不存在的第三方 API。
- DJI、极飞、国家/省级平台、商业溯源平台必须通过 Adapter 接入。
- 核心业务不能依赖任何单一厂商。
- 第一阶段采用模块化单体，不要一开始拆成几十个微服务。
- 高风险设备动作必须经过安全检查和人工授权。
- 第三方平台故障不能阻塞本地核心业务。
- 所有关键生产事件都必须可审计、可追溯、可验证。
