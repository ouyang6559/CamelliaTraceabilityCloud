# 21 Trace Provider SDK

## 目标

任何合规的第三方溯源平台都可以实现 Provider。

## Provider 能力

```text
PRODUCT
BATCH
TRACE_EVENT
INSPECTION
CODE
QUERY
VERIFY
EVIDENCE
```

Provider 必须声明 Capability。

## 状态

```text
CONNECTED
AUTH_REQUIRED
DISCONNECTED
ERROR
RATE_LIMITED
SYNCING
```

## Sync

必须：

- idempotent
- retryable
- observable
- auditable

## Local First

Provider 失败时：

```text
Local Data = valid
External Sync = pending/failed
```

## 安全

Provider credential 必须存储在 Secret Manager 或加密配置中。
