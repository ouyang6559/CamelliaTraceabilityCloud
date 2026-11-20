# 02 领域模型

## 1. 核心实体

```text
Tenant
Organization
User
Role
Permission

Farm
Plot
Crop
Variety
Plant

AgriculturalTask
AgriculturalTaskRecord
Input
InputBatch
InputUsage

Disease
Pest
DiseaseRecord

Device
DeviceModel
DeviceCapability
DeviceTelemetry
DeviceEvent

Mission
MissionRoute
MissionWaypoint
MissionEvent

Sensor
Datastream
Observation

Harvest
RawMaterialBatch
ProcessingBatch
Product
ProductBatch
Package

QualityTest
QualityReport

Warehouse
Inventory
Logistics

TraceCode
TraceEvent
Evidence
BlockchainProof

ExternalPlatform
Adapter
SyncTask
SyncLog

Alert
Notification

AIJob
AIResult
AIAudit
```

## 2. 地块优先

第一阶段：

```text
Farm -> Plot -> Crop Batch
```

第二阶段：

```text
Plot -> Plant / Tree
```

不要第一版把单棵油茶树作为核心数据对象。

## 3. TraceEvent 是溯源核心

不要把 Product 作为溯源中心。

所有生产活动形成 TraceEvent：

- 种植
- 施肥
- 施药
- 灌溉
- 除草
- 修剪
- 巡田
- 无人机作业
- 机器人作业
- 采收
- 入库
- 加工
- 检测
- 包装
- 物流
- 出库

## 4. AgriculturalEvent

设备产生的农业活动也必须统一进入：

```text
Device
 -> Mission
 -> AgriculturalEvent
 -> TraceEvent
 -> Evidence
```

## 5. ID

推荐 ULID。

## 6. 时间

数据库保存 UTC；API 使用 ISO-8601；前端按用户时区显示。

## 7. 单位

内部使用 SI 单位，展示层支持：

- 亩
- 公顷
- acre
- hectare
- 公斤
- 升

## 8. 国际化

至少：

```text
zh-CN
en-US
```

所有用户可见文本不得硬编码。
