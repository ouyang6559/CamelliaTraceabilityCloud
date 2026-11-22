# ADR-008 Phase 4 遥感基线：STAC 目录 + 处理任务管线

- 状态：已接受（2026-11-23）
- 关联：specs/04.9-04.11（遥感/STAC/NDVI）、specs/06.10（外部平台要求）、ADR-005

## 决策

### 1. 处理任务管线（specs/04.9）
- `remote_sensing_jobs` 表 + 后台 Worker（2s tick，全租户扫描，同 sync worker 模式）：
  queued → running（processor.Start + external_task_id）→ succeeded/failed
  （processor.Poll 上报 progress/outputs）。
- `Processor` 接口两个实现：
  - **mock**：确定性开发/演示处理器，把输入资产提升为输出资产（不做真实摄影测量），
    让「上传影像 → 任务 → STAC 资产」全链路在无真实 NodeODM 时可演示。
  - **nodeodm**：按 NodeODM 公开 REST API 文档实现（POST /task、GET /task/{id}、
    status.code 10/20/30/40/50 语义）。**Start 恒定失败**直到部署配置了真实
    `CTC_NODEODM_URL`——诚实失败而非伪造（specs/06.11），失败即任务 failed 并留错误。
- 处理器选择：任务级 `processor` 字段 + 部署配置（`CTC_NODEODM_URL`/`CTC_NODEODM_TOKEN`）。

### 2. STAC 资产目录（specs/04.10）
- `stac_items` 表存完整 OGC STAC 1.0.0 Item JSON（按租户隔离，collection = job 类型）。
- Worker 成功后按输出资产各发布一个 STAC Item（assets.href 指向 /files/{id}/content）。
- REST：`GET /stac/collections`、`GET/POST /stac/collections/{cid}/items`——支持把
  外部生产的正射影像（/files 上传）手动导入目录（importItem 带 Point geometry）。
- 局限（记录）：mock 产出的 Item geometry 为 null（真实栅格脚印待 NodeODM 输出
  或无人机 POS 数据），部署可经 importItem 补 Point。

### 3. NDVI（specs/04.11）
- job_type=ndvi 由同一管线承载；真实波段运算需要多光谱 GeoTIFF + GDAL，
  当前由 mock/NodeODM 产出占位资产——算法层与 UI 解耦的约束保持（前端只展示目录）。

## 后果

- 上传影像 → 建任务 → worker 处理 → STAC 目录可查，全链路无真实依赖即可演示与 CI。
- 已知折衷：mock 不做真实摄影测量；NodeODM Start 恒失败（诚实 TODO）；STAC Item
  geometry 待真实栅格；NDVI 计算待多光谱输入。
