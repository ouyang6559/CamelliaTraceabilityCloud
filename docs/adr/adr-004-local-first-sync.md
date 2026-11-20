# ADR-004 Local-First 外部同步

- 状态：已接受（2026-11-21）
- 关联：specs/01.7、specs/05.10、specs/06、specs/21

## 决策

1. 本地写路径永不依赖外部平台：业务事务提交后经 NATS `sync.*` 事件驱动，由后台 worker 消费
   `sync_tasks` 队列（PENDING → SYNCING → SYNCED / RETRYING / FAILED）。
2. 每任务带 `idempotency_key`（ULID），Provider 必须幂等；失败按指数退避重试（≤ max_attempts），
   超限进入 FAILED（dead-letter），可人工 `POST /retry`。
3. Provider 合同（packages/traceprovider）与 specs/21 一致；凭据仅来自配置（specs/08.7）。
4. 严格遵守 specs/06.11：未取得官方文档的平台只提供 `Interface + Mock + Integration TODO`。
   当前已注册：mock。国家平台/湖北/HiMarking/蚂蚁链/至信链/农展网/立非智码 等待官方接口文档。

## 后果

- 第三方不可用时本地业务照常运行（冒烟验证断言 SYNCED 状态迁移）。
- 全部同步动作留痕于 sync_logs，可审计可观测。
