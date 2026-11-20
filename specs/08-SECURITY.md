# 08 安全

## 1. 身份

支持：

- JWT
- OAuth2/OIDC
- MFA
- TOTP

## 2. 权限

RBAC + ABAC。

不仅判断“有没有权限”，还要判断：

- tenant
- organization
- farm
- plot
- device
- mission
- resource

## 3. 设备认证

支持：

- device ID
- certificate
- secret
- MQTT TLS

## 4. 高危操作

必须：

```text
Authentication
 -> Authorization
 -> Safety Check
 -> Confirmation
 -> Execution
 -> Audit
```

## 5. 审计

记录：

- actor
- timestamp
- tenant
- IP
- operation
- resource
- old value
- new value
- device
- GPS
- evidence

## 6. API

必须：

- TLS
- rate limit
- validation
- authentication
- authorization
- signature where required
- idempotency

## 7. Secret

不得把：

- API key
- access token
- device secret
- private key

写进源码、Git 或前端。

## 8. AI 安全

AI 输出必须标注为 AI-generated / recommendation，不得冒充人工确认。
