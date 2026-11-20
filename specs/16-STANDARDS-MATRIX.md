# 16 国际标准与开放协议矩阵

本项目采用“标准优先、厂商 Adapter 补充”的策略。

| 领域 | 优先标准/协议 | 用途 |
|---|---|---|
| GIS | OGC API Features | 地理要素服务 |
| GIS | GeoJSON | Web 地理数据 |
| GIS | GeoPackage | 离线空间数据 |
| 遥感 | STAC | 时空资产目录 |
| 遥感 | GeoTIFF/COG | 栅格影像 |
| 遥感 | LAS/LAZ | 点云 |
| IoT | OGC SensorThings | 传感器数据模型 |
| IoT | MQTT | 设备消息 |
| 机器人 | ROS 2 | 机器人中间件 |
| 无人机/机器人 | MAVLink 2 | 开放遥测/任务协议 |
| 农机 | ISO 11783 / ISOBUS | 农业机械互联 |
| 农业数据 | ADAPT | 农业数据互操作 |
| 编码 | GS1-compatible design | 产品/物流标识 |
| API | OpenAPI | HTTP API 文档 |
| RPC | gRPC | 服务通信 |
| 数据 | JSON/GeoJSON | 通用交换 |
| 可观测性 | OpenTelemetry | Trace/Metrics/Logs |

## 标准使用原则

1. 能使用成熟国际标准，不自造协议。
2. 标准不足时建立扩展模型。
3. 厂商协议只出现在 Adapter。
4. 核心数据模型不能被厂商字段污染。
5. 所有扩展必须记录 source/vendor/version。
