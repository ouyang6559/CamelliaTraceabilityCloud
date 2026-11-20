# 00 项目总览

## 1. 正式名称

中文：**茶源智控溯源云**

英文：**CamelliaTraceabilityCloud**

建议产品/代码简称：

- CTC Cloud
- CamelliaTrace
- `ctc`

## 2. 一句话定位

> CamelliaTraceabilityCloud 是面向油茶及可扩展农业场景的开源农业数字基础设施平台，统一连接土地、农事、IoT、GIS、无人机、无人车、农业机器人、农机、AI、生产加工、质量安全和可信溯源。

## 3. 核心对象

```text
Tenant
  -> Farm
    -> Plot
      -> Crop / Plant
        -> AgriculturalTask
          -> Device / Mission
            -> AgriculturalEvent
              -> Harvest
                -> Batch
                  -> Processing
                    -> Product
                      -> Package
                        -> TraceCode
```

## 4. 世界级目标

平台必须做到：

- 数据可迁移
- API 开放
- 设备可替换
- 第三方平台可替换
- GIS 标准化
- IoT 标准化
- 农机标准化
- 溯源标准化
- AI Provider 可替换
- Cloud / Edge 可部署
- 中英文国际化

## 5. 不做厂商锁定

禁止把以下厂商写进核心 Domain：

- DJI
- XAG / 极飞
- 任一割草机器人厂商
- 任一拖拉机厂商
- 任一区块链平台
- 任一 AI 模型厂商
- 任一云厂商

所有厂商均属于 Adapter。

## 6. 目标用户

- 油茶企业
- 家庭农场
- 农民合作社
- 农业基地
- 农业服务组织
- 农机服务商
- 无人机植保服务商
- 农业技术人员
- 质检机构
- 加工企业
- 品牌企业
- 政府农业监管
- 科研机构

## 7. 产品形态

```text
PC Web
Mobile H5 / PWA
Public Trace Web
Operations Dashboard
Edge Gateway
Device Gateway
Open API
SDK
CLI
```
