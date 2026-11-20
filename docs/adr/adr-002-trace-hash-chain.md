# ADR-002 TraceEvent 按租户哈希链

- 状态：已接受（2026-11-21）
- 关联：specs/05-TRACEABILITY.md（TraceEvent First / Hash）

## 背景

可信溯源要求生产记录防篡改可验证。specs 规定默认 SHA-256，链上仅存摘要。

## 决策

1. 每个租户维护独立链：`sequence` 单调递增，`hash = SHA-256("ctc-trace-v1|seq|prev_hash|tenant_id|canonical_payload")`。
2. 并发串行化：`pg_advisory_xact_lock(hashtext(tenant_id))` 在事务内取最新 sequence 与 prev hash。
3. canonical payload 经 JSON round-trip 归一化（nil 集合 → []/{}、json.RawMessage("null") 与 SQL NULL 等价），
   保证 append 与 verify 重建的字节完全一致（这是链可验证的前提）。
4. `POST /trace-events/verify-chain` 全链重放，报告首个断裂点；消费者端 `/public/trace/{code}/verify` 提供存证核对。
5. 区块链存证（Mock Provider）只锚定 `ref_type+ref_id+events` 的聚合哈希，原始数据永不离库。

## 后果

- 任何字段篡改都会导致后续全链 verify 失败（可精确定位断裂 sequence）。
- 事件不可删除/更新（仅追加），业务表的数据修正通过新事件表达。
- 若未来引入 SM3（中国场景），实现 Provider 接口即可，链结构不变。
