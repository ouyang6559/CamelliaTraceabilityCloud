# 09 API 规范

## 1. API First

```text
Proto
 -> Buf
 -> Go
 -> TypeScript SDK
 -> OpenAPI
```

前端不得手工重复维护 API 类型。

## 2. API Version

```text
/api/v1
```

破坏性变化使用新版本。

## 3. ID

ULID。

## 4. 时间

ISO-8601。

## 5. Pagination

统一：

```text
page
page_size
```

后续可增加 cursor pagination。

## 6. Filtering

统一支持：

```text
filter
sort
search
```

## 7. 错误

统一错误结构：

```text
code
message
details
request_id
```

## 8. Idempotency

以下接口必须支持：

- create mission
- create trace event
- sync
- webhook
- blockchain proof

通过：

```text
Idempotency-Key
```

## 9. Webhook

支持：

- signature
- timestamp
- retry
- idempotency
- event_id

## 10. Realtime

- WebSocket
- SSE

## 11. Device

设备侧：

- MQTT
- HTTPS
- vendor API
- ROS2 gateway
- MAVLink gateway
