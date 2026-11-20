# 05 可信溯源

## 1. 核心原则

TraceEvent First。

## 2. 链路

```text
Farm
 -> Plot
 -> Crop Batch
 -> Agricultural Events
 -> Harvest Batch
 -> Raw Material Batch
 -> Processing Batch
 -> Product Batch
 -> Package
 -> Trace Code
```

## 3. TraceEvent

建议字段：

```text
id
tenant_id
farm_id
plot_id
batch_id
event_type
event_time
operator_id
device_id
location
quantity
unit
inputs
media
documents
weather
metadata
evidence_hash
created_at
```

## 4. Evidence

关键记录支持：

- photo
- video
- PDF
- test report
- GPS
- timestamp
- operator
- device
- signature
- hash

## 5. Hash

默认：

- SHA-256

中国特定存证场景：

- SM3

## 6. Blockchain

不把全部业务数据上链。

链上建议保存：

```text
business_id
evidence_id
hash
timestamp
provider
transaction_id
```

原始数据留在平台。

## 7. 一物一码

支持：

- batch code
- package code
- carton code
- pallet code
- logistics code
- consumer QR

设计兼容 GS1 思路，但不得把平台锁死为单一编码体系。

## 8. 消费者页面

扫码后展示：

- 产品
- 产地
- 品种
- 采收
- 加工
- 检测
- 包装
- 物流
- 可信验证

不要直接暴露数据库字段。

## 9. 溯源图

系统必须能形成 Trace Graph：

```text
Plot A
 -> Harvest H001
 -> Raw R001
 -> Processing P001
 -> Product B001
 -> Package PK001
 -> QR
```

## 10. 第三方同步

必须 Local First：

```text
Local Commit
 -> Event
 -> Async Sync
 -> External Provider
```

第三方不可用不能阻塞本地业务。

状态：

```text
LOCAL_ONLY
PENDING
SYNCING
SYNCED
FAILED
RETRYING
```
