# ADR-014 语义检索升级：嵌入向量 + 余弦排序（pgvector-ready）

- 状态：已接受（2026-11-28）
- 关联：ADR-009（RAG-lite 关键词检索）、specs/07.6

## 决策

### 1. 嵌入向量
- `knowledge_chunks` 增 `embedding`（jsonb float 数组）与 `embedding_model` 列。
- `Embedder` 接口两个实现：
  - **mock**（默认）：确定性哈希字符 shingle（size 3）→ 64 维 L2 归一化向量，
    离线/免费/可复现——CI 与演示不依赖外部服务。
  - **openai**：OpenAI 兼容 `/v1/embeddings` 端点（OpenAI/vLLM/Ollama...），
    模型 `CTC_AI_EMBEDDING_MODEL`（默认 text-embedding-3-small）。
- 配置 `CTC_AI_EMBEDDINGS`：mock（默认）| openai | off（纯关键词）。
- 文档创建时逐分块嵌入（best-effort：失败仅关键词检索，不阻塞导入）。

### 2. 检索合并
- `retrieveKnowledge`：查询向量（best-effort）与分块向量做余弦，组合评分
  `score = keyword + 10×cosine`；每文档保留最高分块 → Top-4（同 ADR-009 形状）。
- 优雅降级：无嵌入（off 或历史数据）→ 纯关键词路径，行为与 ADR-009 一致。

### 3. pgvector 路线（明确记录）
- 当前：jsonb 存储 + Go 层余弦，扫描上限 800 行——中等规模（数千分块）可接受。
- 升级触发：分块规模上万或延迟不达标。需要部署镜像带 pgvector 扩展
  （如 pgvector/pgvector-pg16 或 postgis 镜像加装），届时：
  1. `ALTER TABLE ... ADD COLUMN embedding vector(N)` + HNSW 索引；
  2. 历史数据回填（jsonb → vector）；
  3. `retrieveKnowledge` 的 SQL 换成 `ORDER BY embedding <=> $query`。
  接口（Embedder / rankHits 形状）不变。

## 后果
- 中文知识库的同义改写检索可用（mock 嵌入对短语级 paraphrase 有效）；
  确定性保证 CI 可测。已知折衷：hash shingle 对长文档语义弱于真实模型，
  换 openai 端点即获得真语义，无需代码改动。
