# 06 第三方平台集成

## 1. 统一 Adapter

```go
type TraceProvider interface {
    CreateProduct(...)
    CreateBatch(...)
    CreateTraceEvent(...)
    UploadInspection(...)
    GenerateCode(...)
    QueryTrace(...)
    Verify(...)
    Sync(...)
}
```

实际接口必须通过 Proto 定义。

## 2. 目标平台

```text
NationalTraceAdapter
HubeiTraceAdapter
HiMarkingAdapter
AntChainAdapter
TencentZhiXinAdapter
NongZhanAdapter
LiFeiAdapter
```

## 3. 国家农产品质量安全追溯

目标：

- 国家农产品质量安全追溯管理信息平台

要求：

- 平台账号/企业资质独立管理
- 官方 API/数据交换规范作为唯一接口依据
- 没有公开 API 时不得假造 endpoint
- 支持人工导出/导入作为过渡方案
- 同步任务可重试

## 4. 湖北

目标：

- 鄂惠农 / 荆楚农安
- 湖北农业农村相关质量安全追溯体系

具体接口必须以当前官方接口、SDK、数据交换规范和授权为准。

## 5. HiMarking

建立：

```text
adapters/himarking/
```

支持：

- product
- batch
- trace event
- code
- anti-counterfeit
- query

具体接口以中选科技官方商业 API/SDK 为准。

## 6. 蚂蚁链

建立：

```text
adapters/antchain/
```

仅通过合法官方 API/SDK/企业授权。

## 7. 腾讯云至信链

建立：

```text
adapters/tencent-zxin/
```

用于关键证据/摘要存证。

## 8. 农展网

建立：

```text
adapters/nongzhan/
```

具体接口以官方平台能力和合作接口为准。

## 9. 立非智码

建立：

```text
adapters/lifei/
```

在取得正式接口文档后实现。

## 10. 所有外部平台必须

- timeout
- retry
- circuit breaker
- rate limit
- idempotency
- dead letter
- sync log

## 11. 绝对禁止

禁止编造：

- API endpoint
- SDK
- authentication flow
- response schema
- 商业平台公开能力
