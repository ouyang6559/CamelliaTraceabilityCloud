# 11 PC、移动端与消费者端

## 1. PC

推荐：

```text
React
TypeScript
Vite
Ant Design
TanStack Query
Zustand
ECharts
MapLibre GL JS
```

## 2. PC 菜单

```text
工作台

农业生产
  基地
  地块
  作物
  品种
  油茶档案
  农事计划
  农事任务
  投入品
  病虫害
  采收

智能设备
  设备总览
  无人机
  无人车
  农机
  割草机器人
  耕地机器人
  IoT
  设备地图
  任务
  日志

智能农业
  GIS
  遥感
  NDVI
  气象
  土壤
  AI

生产加工
  原料批次
  加工批次
  产品
  仓储
  物流
  质量检测

可信溯源
  批次
  TraceEvent
  一物一码
  区块链
  第三方平台
  同步任务

系统
  用户
  角色
  组织
  租户
  审计
```

## 3. Mobile

使用 H5/PWA。

重点：

- 今日任务
- 扫码
- GPS
- 拍照
- 巡田
- 病虫害上报
- 设备
- 采收
- 入库
- 异常

> **落地状态（2026-11-28）**：`/mobile/tasks` 今日任务执行页已落地——
> 今日任务列表（开始/完成）、GPS 采集（浏览器 geolocation，附带完成记录 →
> TraceEvent）；响应式窄屏优化。扫码（一物一码查询）复用 trace-web；
> 病虫害上报页 `/mobile/report`（相机拍照 + GPS + 字典）已落地；
> **PWA 离线采集闭环已落地**（sw.js app shell 缓存 + IndexedDB 队列 + online
> 自动重放，specs/11.4 Offline Capture -> Sync Queue -> Server）。

## 4. 离线

使用 IndexedDB / 本地缓存。

支持：

```text
Offline Capture
 -> Sync Queue
 -> Server
```

## 5. 消费者端

无需登录。

扫码：

```text
QR
 -> Product
 -> Origin
 -> Harvest
 -> Processing
 -> Inspection
 -> Logistics
 -> Verification
```

不要展示内部管理字段。

## 6. 响应式

PC、平板、手机都必须可用。

## 7. 国际化

zh-CN / en-US。
