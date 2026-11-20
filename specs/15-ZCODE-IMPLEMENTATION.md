# 15 Zcode 实施规范

## 1. 总原则

不要一次性生成整个项目。

必须按阶段开发。

```text
Architecture
 -> Foundation
 -> Domain
 -> API
 -> Frontend
 -> Device
 -> GIS
 -> Trace
 -> AI
```

## 2. 第一批任务

```text
TASK-001 Initialize project skeleton (API + Admin Console)
TASK-002 Project repository structure
TASK-003 PostgreSQL/PostGIS
TASK-004 Redis
TASK-005 NATS
TASK-006 MQTT
TASK-007 Object Storage
TASK-008 Tenant/RBAC
TASK-009 Farm
TASK-010 Plot
TASK-011 Crop/Variety
TASK-012 AgriculturalTask
TASK-013 Input
TASK-014 Harvest
TASK-015 Batch
TASK-016 Product
TASK-017 TraceEvent
TASK-018 TraceCode
TASK-019 Public Trace
```

## 3. 第二批

```text
TASK-020 Device model
TASK-021 Device capability
TASK-022 Device registry
TASK-023 Generic MQTT Adapter
TASK-024 Device Simulator
TASK-025 Telemetry
TASK-026 Mission
TASK-027 Geofence
TASK-028 Safety Engine
```

## 4. 第三批

```text
TASK-030 MAVLink
TASK-031 ROS2 Gateway
TASK-032 DJI Adapter
TASK-033 XAG Adapter
TASK-034 AgriculturalEvent
TASK-035 ADAPT
TASK-036 ISOBUS abstraction
```

## 5. 第四批

```text
TASK-040 GIS
TASK-041 Remote Sensing
TASK-042 WebODM integration
TASK-043 STAC
TASK-044 NDVI
```

## 6. 第五批

```text
TASK-050 Quality
TASK-051 Evidence
TASK-052 Blockchain Proof
TASK-053 National Trace Adapter
TASK-054 Hubei Trace Adapter
TASK-055 HiMarking
TASK-056 AntChain
TASK-057 Tencent ZhiXin
TASK-058 NongZhan
TASK-059 LiFei
```

## 7. 第六批

```text
TASK-070 AI Provider
TASK-071 Vision
TASK-072 RAG
TASK-073 AI Agent
TASK-074 Disease
TASK-075 Pest
TASK-076 Yield
TASK-077 Cost
```

## 8. Zcode 每次开发要求

每个任务必须先：

1. 阅读相关 SPEC
2. 检查现有代码
3. 分析依赖
4. 设计数据库变化
5. 设计 Proto/API
6. 实现后端
7. 实现前端
8. 增加测试
9. 更新文档
10. 运行 lint/test/build

## 9. 禁止

禁止：

- fake API
- fake vendor integration
- hardcoded tenant
- hardcoded admin
- vendor logic in core domain
- secrets in code
- massive generated unused code
- 为了 UI 快速开发而破坏领域模型

## 10. 第三方接口

如果没有官方文档：

```text
Interface
+
Mock Adapter
+
Integration TODO
```

而不是猜测 API。

## 11. 代码提交

推荐 Conventional Commits：

```text
feat:
fix:
refactor:
docs:
test:
chore:
perf:
security:
```

## 12. 每个阶段必须可运行

每个 Phase 结束必须：

```text
docker compose up -d
```

并完成对应 E2E Demo。

## 13. 最终验收

必须可以完整演示：

```text
创建租户
 -> 创建基地
 -> 创建地块
 -> 创建油茶批次
 -> 创建农事任务
 -> 设备执行模拟任务
 -> 产生农业事件
 -> 采收
 -> 原料批次
 -> 加工批次
 -> 产品
 -> 生成 QR
 -> 手机扫码
 -> 查看完整溯源
 -> 查看证据
 -> 查看可信存证
```
