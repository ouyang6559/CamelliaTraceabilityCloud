# 13 测试与质量

## 1. 测试层级

```text
Unit
Integration
API
E2E
GIS
Device Simulator
Mission
Trace
Security
Performance
```

## 2. 必须测试

### 网络

- disconnect
- reconnect
- packet loss
- duplicate message
- out-of-order message

### 设备

- offline
- low battery
- GPS loss
- RTK loss
- command timeout
- mission failure
- emergency stop

### 第三方

- timeout
- rate limit
- invalid token
- API unavailable
- duplicate sync
- retry
- dead letter

### 溯源

- batch split
- batch merge
- rollback
- duplicate QR
- invalid QR
- evidence hash mismatch

## 3. 安全测试

- authorization bypass
- tenant isolation
- SQL injection
- XSS
- CSRF where applicable
- SSRF
- file upload
- secret leakage

## 4. 性能目标

MVP 目标：

- 普通 API P95 < 300ms（不含外部 API）
- WebSocket 实时状态延迟目标 < 2s
- 任务创建应为异步友好
- 第三方同步不能阻塞核心请求

实际目标以压测结果调整。

## 5. Definition of Done

每个模块必须具备：

- backend
- database
- proto
- API
- frontend
- permission
- audit
- validation
- tests
- docs
- seed data
