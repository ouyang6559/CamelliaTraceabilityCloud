# 01 总体架构

## 1. 架构

```text
PC Web / Mobile / Public Trace / API Clients
                    |
                API Gateway
                    |
          CamelliaTraceabilityCloud
                    |
    +---------------+----------------+
    |               |                |
Agriculture       Device          Traceability
    |               |                |
GIS / IoT        Mission           Evidence
    |               |                |
    +---------------+----------------+
                    |
        PostgreSQL + PostGIS
        Redis / NATS / MQTT
        Object Storage
                    |
        +-----------+-----------+
        |           |           |
      Cloud        Edge       External
                              Platforms
```

## 2. 第一阶段架构

采用 Modular Monolith：

```text
Go
go-kratos
Ent
PostgreSQL/PostGIS
Redis
NATS
MQTT
S3-compatible Object Storage
```

不要第一版直接拆成大量微服务。

## 3. 领域模块

```text
identity
tenant
agriculture
gis
iot
device
mission
quality
production
inventory
logistics
trace
evidence
blockchain
integration
workflow
alert
ai
audit
```

## 4. 后续可拆分

达到较大规模后，再拆：

- Device Service
- IoT Service
- AI Service
- GIS / Remote Sensing Service
- Trace Service

## 5. 数据分层

```text
Operational Data
Telemetry Data
Spatial Data
Media Data
Evidence Data
Analytics Data
Blockchain Proof
```

## 6. 事件总线

NATS：

```text
agri.*
device.*
mission.*
iot.*
trace.*
quality.*
ai.*
alert.*
sync.*
```

## 7. 核心设计

本地业务数据优先保存：

```text
Local DB -> Event -> Async External Sync
```

第三方平台不可用时，本地业务仍然正常。
