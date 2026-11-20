# 18 推荐仓库结构

```text
CamelliaTraceabilityCloud/
├── apps/
│   ├── api/
│   ├── admin-web/
│   ├── mobile-web/
│   ├── trace-web/
│   └── edge-gateway/
├── services/
│   ├── agriculture/
│   ├── device/
│   ├── iot/
│   ├── mission/
│   ├── gis/
│   ├── remote-sensing/
│   ├── production/
│   ├── quality/
│   ├── trace/
│   ├── evidence/
│   ├── blockchain/
│   ├── integration/
│   ├── workflow/
│   ├── alert/
│   └── ai/
├── adapters/
│   ├── dji/
│   ├── xag/
│   ├── mavlink/
│   ├── ros2/
│   ├── isobus/
│   ├── mqtt/
│   ├── himarking/
│   ├── antchain/
│   ├── tencent-zxin/
│   ├── nongzhan/
│   └── lifei/
├── packages/
│   ├── proto/
│   ├── sdk-go/
│   ├── sdk-ts/
│   └── device-sdk/
├── simulator/
│   ├── drone/
│   ├── rover/
│   ├── tractor/
│   ├── mower/
│   └── iot/
├── deployments/
│   ├── docker/
│   └── kubernetes/
├── docs/
├── examples/
├── scripts/
└── tests/
```

第一阶段允许物理目录比逻辑领域少，采用 Modular Monolith；不要为了目录“看起来像微服务”而拆分运行时。
