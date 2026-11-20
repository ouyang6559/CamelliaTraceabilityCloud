# ADR-003 Mission 安全流水线（Human-in-the-loop）

- 状态：已接受（2026-11-21）
- 关联：specs/03-DEVICE-PLATFORM.md（安全）、specs/19-ZCODE-MASTER-PROMPT.md（第六原则）

## 决策

高危设备动作（起飞/喷洒/割草/耕作/自动驾驶/远程控制）强制状态机：

```text
draft ──validate──> safety_check ──safety engine──> awaiting_confirmation
      │                   │（任一检查失败）                  │（人工 POST /approve，mission:approve 权限）
      └─> validation_failed└─> validation_failed          ↓
                                                    approved ──start──> executing
                                                    executing ⇄ paused
                                                    executing ──stop/100%──> completed
                                                    任意非终态 ──cancel──> cancelled
```

- 能力校验：设备 capabilities 必须包含 mission_type 对应能力（SPRAY/MOW/…），核心不假设所有设备能力相同。
- 安全检查项：keep_out 围栏穿越（PostGIS ST_Covers）、电量 ≥20%、航线完整性；结果固化到 `safety_check` jsonb。
- `start` 在非 approved 状态返回 409（冒烟测试断言该保护）。
- 执行期设备经 device-gateway 上报进度；100% 自动 completed 并追加 uav_mission/robot_mission TraceEvent。
- AI Provider 无 approve 权限（specs/07.8）；AI 建议必须经 `/ai/jobs/{id}/review` 人工采纳。

## 后果

- 一切执行可审计（mission_events 全量留痕），符合 specs/08.4 高危操作六步链。
