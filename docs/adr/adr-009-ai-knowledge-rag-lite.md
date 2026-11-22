# ADR-009 AI 知识库：RAG-lite 检索增强对话

- 状态：已接受（2026-11-23）
- 关联：specs/07.6（AI 知识库）、specs/07.7（审计）、ADR-006（租户隔离）

## 决策

### 1. 知识库模型
- `knowledge_docs`（租户隔离：title/category/source/content/chunk_count）+
  `knowledge_chunks`（doc_id + chunk_index + content）。
- 导入两条路径：粘贴文本（POST /ai/knowledge）与上传 .txt/.md（multipart import）。
  PDF/DOCX 文本抽取为明确 TODO（拒绝并提示，不伪造）。
- 分块：段落优先合并至 ≤400 rune，超出部分 160 rune 滑窗 + 40 rune 重叠；
  确定性、可单测。

### 2. 检索（RAG-lite，诚实边界）
- **关键词/shingle 评分在 Go 应用层**：查询分词（拉丁词 ≥2 字符 + CJK 二元组，
  去重）→ 对租户内分块（上限 800 行）计分 → 每文档保留最高分块 → 排序取 Top-4。
- **为什么不是向量检索**：pgvector 不在 postgis 基础镜像中，且中文语义向量需要
  embedding 模型依赖。关键词评分对结构化农事知识（规程/周期/用量）召回可靠且
  完全可测。文档明确标注为 RAG-lite；pgvector + embedding 为后续升级
  （届时仅需替换 retrieveKnowledge 实现，接口不变）。
- 租户隔离：检索限定 `kc.tenant_id = $1`。

### 3. 对话集成
- `POST /ai/chat` 请求增加 `use_knowledge`：检索最后一条用户消息 → 命中则将
  `[知识库摘录]` 上下文块置于 prompt 之前（对 Mock 与 OpenAI 兼容 Provider 一致生效）。
- 响应与 `ai_jobs.output` 携带 `sources`（doc_id/title/category/score/excerpt）——
  来源可追溯（specs/07.7），前端在回答下方展示引用标签。

### 4. 审计与安全
- 知识文档按租户隔离；删除级联分块；AI 输出仍为 RECOMMENDATION，不自动执行
  任何动作（specs/07.8）；prompt 中注入的摘录标记为知识库内容而非指令。

## 后果
- 中文知识文档检索可用（无需外部 embedding 服务）；文档结构变化不影响 Provider 层。
- 已知折衷：关键词评分对同义改写召回有限；分块为定长滑窗（段落边界优先）；
  检索扫描上限 800 行（规模增长需引入向量索引）。
