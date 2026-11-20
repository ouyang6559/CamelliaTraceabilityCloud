# 贡献指南 Contributing to CamelliaTraceabilityCloud

感谢参与！本项目是开源（Apache-2.0）的农业数字基础设施平台。请先阅读
[specs/README.md](specs/README.md) 与 [specs/24-PHASE0-IMPLEMENTATION-DECISIONS.md](specs/24-PHASE0-IMPLEMENTATION-DECISIONS.md)。

## 开发铁律（specs/19）

1. **Core Domain ≠ Vendor Integration** — 厂商协议/SDK 只出现在 `adapters/`，核心领域禁止出现 DJI/XAG/区块链/AI 厂商等具体名字。
2. **TraceEvent First** — 一切生产活动进入统一 TraceEvent（哈希链），不要为单个业务造孤立日志。
3. **Local First** — 本地提交永远成功；第三方平台异步同步，故障不阻塞业务请求。
4. **不伪造第三方 API** — 没有官方文档时只允许 `Interface + Mock Adapter + Integration TODO`。
5. **高危操作必须人工批准** — Mission 状态机不可绕过；AI 无权执行任何动作。
6. **Secrets 不入代码库** — 全部通过环境变量注入。

## 每个功能必须同时具备（Definition of Done）

```
数据库迁移 → API → 权限 → 审计 → 后端实现 → 前端页面 → 测试 → 文档/种子
```

## 提交规范（Conventional Commits）

```text
feat:     新功能
fix:      缺陷修复
refactor: 重构（不改行为）
docs:     文档
test:     测试
chore:    工程/依赖
perf:     性能
security: 安全
```

## 开发流程

```bash
# 1. 起基础设施
docker compose -f deployments/docker/docker-compose.yml up -d postgres redis nats mosquitto

# 2. 跑后端（自动迁移）
export CTC_DATABASE_URL='postgres://ctc:ctc@localhost:15432/ctc?sslmode=disable'
export CTC_REDIS_URL='redis://localhost:16379/0'
export CTC_NATS_URL='nats://localhost:14222'
export CTC_MQTT_URL='tcp://localhost:11883'
go run ./apps/api/cmd/ctc-api -mode seed && go run ./apps/api/cmd/ctc-api

# 3. 迭代 + 验证
go test ./...
bash tests/e2e/smoke.sh
gofmt -w . && go vet ./...
```

## 新增一个业务资源的标准步骤

1. `migrations/000X_*.sql` 增表（含 tenant_id/created_at/updated_at/created_by/version 基础列）
2. 在对应模块的 `Mount` 中用 `crud.NewStore` 声明列（Perm 对齐权限目录）或手写 handler
3. `internal/modules/identity` 权限目录增加 `resource:action` 码（若新增）
4. 前端 `pages/SimpleRoutes.tsx` 增加 SimpleResource 或新建专属页面 + 路由 + i18n 词条
5. `tests/e2e/smoke.sh` 增加验收步骤
6. `packages/openapi/openapi.yaml` 更新契约

## Pull Request

- 一个 PR 聚焦一件事，包含测试与文档
- CI 全绿（backend / admin-web / trace-web / compose config）
- 涉及 schema 变更需提供幂等迁移（`IF NOT EXISTS`）
