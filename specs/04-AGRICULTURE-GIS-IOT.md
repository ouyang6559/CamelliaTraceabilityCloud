# 04 农业、GIS、IoT 与遥感

## 1. GIS

推荐：

- PostgreSQL/PostGIS
- MapLibre GL JS
- GeoJSON
- GeoPackage
- OGC API Features
- PMTiles

核心空间对象：

- Farm boundary
- Plot boundary
- Device location
- Mission route
- Pest location
- Sampling point
- Geofence
- No-Go Zone

## 2. 坐标

默认支持 WGS84 / EPSG:4326。

空间数据必须保存：

```text
srid
crs
accuracy
source
captured_at
```

## 3. IoT

协议：

- MQTT
- HTTP
- WebSocket
- CoAP
- Modbus
- CAN
- LoRaWAN

数据模型参考 OGC SensorThings。

## 4. SensorThings 映射

```text
Device
Sensor
ObservedProperty
Datastream
Observation
Location
FeatureOfInterest
```

## 5. 气象

至少：

- temperature
- humidity
- rainfall
- wind speed
- wind direction
- pressure
- light

> **落地状态（2026-11-28）**：气象接入已落地（docs/adr/adr-010）——`/weather`
> API（当前 + 多日预报），Open-Meteo 公开接口实现 + mock，内存 TTL 缓存；
> AI 对话可注入实时气象上下文（use_weather）。本地气象站设备遥测与之互补。

## 6. 土壤

至少：

- moisture
- temperature
- pH
- EC
- NPK

## 7. 遥感

支持：

- RGB
- multispectral
- thermal
- LiDAR
- orthophoto
- DSM
- DTM
- point cloud
- 3D

## 8. 文件格式

```text
GeoTIFF
COG
GeoJSON
GeoPackage
GeoParquet
STAC
LAS
LAZ
3D Tiles
```

## 9. 无人机影像处理

第一阶段优先集成 WebODM/OpenDroneMap，不自己重写完整摄影测量算法。

```text
UAV Image
 -> Processing Queue
 -> WebODM/NodeODM
 -> Orthophoto/DSM/DTM/PointCloud
 -> STAC
 -> GIS
```

## 10. STAC

遥感资产必须可表达为：

```text
Catalog
Collection
Item
Asset
```

> **落地状态（2026-11-23）**：STAC-lite 目录已落地（`stac_items` 表 + `/stac` API，
> OGC STAC 1.0.0 Item JSON），处理管线见 docs/adr/adr-008——mock 处理器全链路可演示，
> NodeODM 客户端按公开 API 实现待真实实例联调；NDVI 待多光谱输入。

## 11. NDVI

平台允许扩展：

```text
NDVI
NDRE
GNDVI
EVI
```

算法层与 UI 解耦。
