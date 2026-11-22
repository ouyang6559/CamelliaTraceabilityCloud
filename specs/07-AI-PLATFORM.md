# 07 AI 平台

## 1. AI 定位

AI 是决策辅助，不是第一阶段的自动控制器。

```text
Observation
 -> AI Analysis
 -> Recommendation
 -> Human Approval
 -> Action
```

## 2. AI Provider

支持 OpenAI-compatible API。

允许：

- OpenAI
- 本地 Ollama
- vLLM
- Qwen
- DeepSeek
- Llama
- 其他兼容服务

具体模型通过配置，不写死。

## 3. AI Vision

接口：

```text
VisionProvider
```

任务：

- disease detection
- pest detection
- weed detection
- tree counting
- canopy analysis
- missing plant detection
- nutrient stress
- water stress
- OCR

## 4. AI Agent

能力：

- 今日农情
- 地块分析
- 异常分析
- 病虫害分析
- 设备异常
- 生产分析
- 成本分析
- 溯源解释
- 农事建议

## 5. AI 输出类型

必须区分：

```text
FACT
OBSERVATION
PREDICTION
RECOMMENDATION
ACTION
```

AI 预测必须带 confidence。

## 6. AI 知识库

RAG 数据：

- 油茶种植
- 病虫害
- 肥料
- 农药
- 农机
- 气象
- 土壤
- 质量
- 法规
- 标准

## 7. AI 审计

保存：

- model
- provider
- prompt/version
- input references
- output
- confidence
- timestamp
- user
- approval

> **落地状态（2026-11-23）**：知识库 RAG-lite 已落地（docs/adr/adr-009）——§6 的
> RAG 数据通过 `knowledge_docs/chunks`（粘贴或 .txt/.md 导入）承载，检索为 Go 层
> 关键词评分（CJK 二元组），对话回答携带 sources 引用；向量检索为后续升级。

## 8. 禁止

AI 不得未经授权：

- 起飞
- 喷洒
- 自动驾驶
- 修改生产数据
- 修改溯源数据
- 发布质检结论
