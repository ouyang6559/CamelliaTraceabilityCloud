# 12 部署与边缘计算

## 1. 本地开发

Docker Compose。

## 2. 最小运行环境

建议：

```text
4 CPU
8GB RAM
100GB SSD
```

用于 Demo / 小型基地。

## 3. 推荐生产

根据规模扩展：

```text
8-32 CPU
16-128GB RAM
SSD
```

## 4. Cloud

支持：

- AWS
- Azure
- GCP
- 腾讯云
- 阿里云
- 华为云
- 其他 S3-compatible 环境

## 5. Edge

支持：

- Intel N100/N150
- x86 mini PC
- ARM64
- Raspberry Pi
- industrial gateway

## 6. Edge Gateway

负责：

- device protocol conversion
- local MQTT
- local cache
- offline mission state
- local alerts
- sync
- video forwarding

## 7. Cloud / Edge

```text
Cloud
  ^
  |
Edge Gateway
  ^
  |
Farm Devices
```

## 8. 断网

断网情况下至少保持：

- 本地数据采集
- 设备状态
- 农事记录
- GPS
- 照片
- 任务状态
- 本地告警

网络恢复自动同步。

## 9. Kubernetes

不是 MVP 必需。

达到规模后支持：
- Helm
- Kubernetes
- Horizontal Scaling
- GPU workload
