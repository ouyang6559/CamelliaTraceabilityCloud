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

## 11. NDVI

平台允许扩展：

```text
NDVI
NDRE
GNDVI
EVI
```

算法层与 UI 解耦。
