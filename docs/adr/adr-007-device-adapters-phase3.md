# ADR-007 Phase 3 设备适配器：MAVLink 与 ROS 2 rosbridge

- 状态：已接受（2026-11-22）
- 关联：ADR-005（真机执行链路）、specs/03.5-03.9、specs/15 TASK-030/031

## 决策

### 1. MAVLink 适配器（`adapters/device/mavlink/`，adapter_type=`mavlink`）
- 基于 bluenviron/gomavlib v4（gomavlib v4 master，MAVLink 2.0，`all` dialect）。
- Endpoint：设备 metadata `endpoint` = UDP client 地址 `ip:port`
  （ArduPilot SITL `-udp`、数传电台或 PX4 都适用）。
- 已实现（specs/03.9 全覆盖）：
  - 遥测：GLOBAL_POSITION_INT → GPS 位置/相对高度；SYS_STATUS → 电池；
    HEARTBEAT → 载具 sys/comp id 与解锁状态
  - 任务上传：MISSION_COUNT → MISSION_REQUEST_INT → MISSION_ITEM_INT → MISSION_ACK
    完整握手，航点 Action 映射 takeoff→MAV_CMD_NAV_TAKEOFF、land→NAV_LAND、
    其余→NAV_WAYPOINT（Frame=GLOBAL，lat/lng×1e7）
  - 启动：未解锁则 MAV_CMD_COMPONENT_ARM_DISARM(1) → MAV_CMD_MISSION_START
  - 暂停/恢复：MAV_CMD_DO_PAUSE_CONTINUE(0/1)；停止：pause + MAV_CMD_NAV_RETURN_TO_LAUNCH
  - 命令：ping（PING 消息）、locate、return_home（RTL）、set_mode（custom mode）
- 所有 COMMAND_ACK 等待 + 结果校验；任务上传超时按航点数动态计算。

### 2. ROS 2 rosbridge 适配器（`adapters/device/rosbridge/`，adapter_type=`rosbridge`）

> **升级（2026-11-28，ADR-012 关联）**：任务执行从 `/goal_pose` 话题升级为
> **rosbridge v2 action 协议**——`send_action_goal`（/navigate_to_pose，
> nav2_msgs/action/NavigateToPose）→ `action_feedback`（distance_remaining 进度）
> → `action_result`（GoalStatus 校验，逐航点完成确认）；暂停 = `cancel_action`
> + 零速度；设备 metadata `nav_mode="topic"` 保留话题回退路径（无 Nav2 的简单底盘）。
> 回环测试覆盖两种模式。
- 基于 gorilla/websocket，实现 rosbridge v2 JSON 协议（subscribe/publish）。
- Endpoint：设备 metadata `endpoint` = `ws://host:9090`（rosbridge_server）。
- 已实现：
  - 订阅 /odom（nav_msgs/Odometry）、/battery_state（sensor_msgs/BatteryState）
  - 任务 = 航点序列，逐个发布 geometry_msgs/msg/PoseStamped 到 /goal_pose
    （Nav2 navigate_to_pose BT 导航器消费），/odom 到点判定（0.8 m）后推进
  - 暂停 = 零 geometry_msgs/msg/Twist 到 /cmd_vel；恢复 = 重发当前 goal；
    停止 = 停车 + 清序列
- 已知 MVP 限制（记录于包文档）：WGS84 航点按 farm 尺度线性近似映射到 map
  坐标（部署可用 metadata 配置 map origin 后替换为精确 ENU 转换）。

### 3. 测试策略（无需真机）
- MAVLink：mock vehicle（gomavlib 节点，实现任务上传协议与 COMMAND_ACK）在
  UDP 上与适配器回环——真实 MAVLink 2.0 帧交互，覆盖上传 3 航点（takeoff/
  waypoint/land）、vehicle 拒绝路径、arm+mission start、pause/resume/RTL、ping。
- rosbridge：mock rosbridge_server（WebSocket），断线/订阅/publish 协议断言 +
  goal 到点自动推进到 100% 完成路径 + pause 的零速度 Twist。
- 挂起语义、命令白名单等已在 ADR-003/005 冒烟覆盖。

### 4. XAG / DJI（明确不做，specs/06.11）
- XAG：`adapters/xag/README.md` 记录 Interface + 集成 TODO，待商业授权。
- DJI 上云API：凭证位已留（CTC_DJI_APP_*），适配器待官方企业账号与设备；
  基于公开文档 developer.dji.com 实现，不编造。

## 后果

- 接入真实设备 = 设备 metadata 填 endpoint + adapter_type（mavlink/rosbridge），
  核心零改动；open-arrows 任务安全流水线（校验→围栏→人工批准）对两种协议一致生效。
- 后续：MAVLink SITL 全链路演示（docker 起 ardupilot SITL 容器）、Nav2 action
  协议升级、DJI 上云API 适配器（凭凭证）。
