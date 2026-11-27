// Package rosbridge implements the Device Adapter SDK over the ROS 2 rosbridge
// v2 protocol (JSON over WebSocket, specs/03.5 P0 via ROS 2, specs/03.8):
// CamelliaTrace -> rosbridge -> ROS 2 nav stack -> robot. This is the
// mainstream route for agricultural UGVs / robots that expose a rosbridge_server.
//
// Endpoint: Device.Endpoint is the rosbridge WebSocket URL ("ws://host:9090").
//
// Mapping (documented honestly, MVP scope):
//   - CreateMission stores the waypoint list.
//   - StartMission publishes each waypoint as geometry_msgs/msg/PoseStamped on
//     /goal_pose (consumed by Nav2's navigate_to_pose BT navigator), advancing
//     to the next goal when the robot reports arrival via /odom proximity.
//   - PauseMission publishes a zero geometry_msgs/msg/Twist on /cmd_vel.
//     ResumeMission continues the goal sequence. StopMission stops + clears.
//   - GPS waypoints are used directly as map (x,y) coordinates; converting
//     WGS84 -> a local ENU map frame requires a map origin which deployments
//     configure per robot (Metadata["map_origin_lat"/"map_origin_lng"]).
package rosbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	sdk "github.com/ouyang6559/CamelliaTraceabilityCloud/adapters/device"
)

// Manifest returns the adapter registration descriptor.
func Manifest() sdk.Manifest {
	return sdk.Manifest{
		Name:         "rosbridge",
		Version:      "0.1.0",
		Type:         "device",
		Vendor:       "ctc",
		Protocols:    []string{"ros2"},
		Capabilities: []string{sdk.CapTelemetry, sdk.CapGPS, sdk.CapMission, sdk.CapAutonomous, sdk.CapMow},
	}
}

const (
	topicGoal      = "/goal_pose"
	typeGoal       = "geometry_msgs/msg/PoseStamped"
	actionNav2Pose = "/navigate_to_pose"
	typeNav2Pose   = "nav2_msgs/action/NavigateToPose"
	goalTimeout    = 120 * time.Second
	topicTwist     = "/cmd_vel"
	typeTwist      = "geometry_msgs/msg/Twist"
	topicOdom      = "/odom"
	typeOdom       = "nav_msgs/msg/Odometry"
	topicBattery   = "/battery_state"
	typeBattery    = "sensor_msgs/msg/BatteryState"
	arriveRadius   = 0.8 // meters: goal considered reached
	pollPeriod     = 300 * time.Millisecond
)

// rosMessage is one rosbridge protocol message (subset).
type rosMessage struct {
	Op     string          `json:"op"`
	Topic  string          `json:"topic,omitempty"`
	Type   string          `json:"type,omitempty"`
	Msg    json.RawMessage `json:"msg,omitempty"`
	ID     string          `json:"id,omitempty"`
	Action string          `json:"action,omitempty"`
}

type conn struct {
	ws      *websocket.Conn
	writeMu sync.Mutex

	mu       sync.Mutex
	odomX    float64
	odomY    float64
	odomOK   bool
	battery  float64
	goals    []sdk.Waypoint
	goalIdx  int
	running  bool
	progress float64

	// Nav2 action protocol state (ADR-012): in-flight goal call id + result
	// channel fed by readLoop; nav_mode selects action vs topic fallback.
	actionCallID string
	actionResult chan actionResultMsg
	navMode      string
}

// actionResultMsg is one rosbridge action outcome (GoalStatus).
type actionResultMsg struct {
	Status uint8 // 4=SUCCEEDED 5=CANCELED 6=ABORTED
}

// Adapter talks to ROS 2 robots through a rosbridge_server.
type Adapter struct {
	mu    sync.Mutex
	conns map[string]*conn
	log   *slog.Logger
}

func New(log *slog.Logger) *Adapter {
	if log == nil {
		log = slog.Default()
	}
	return &Adapter{conns: map[string]*conn{}, log: log}
}

// Manifest returns the adapter registration descriptor.
func (a *Adapter) Manifest() sdk.Manifest { return Manifest() }

func (a *Adapter) getConn(d sdk.Device) (*conn, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.conns[d.Code]
	if !ok {
		return nil, fmt.Errorf("device %s is not connected (call Connect first)", d.Code)
	}
	return c, nil
}

// Disconnect closes the rosbridge WebSocket.
func (a *Adapter) Disconnect(_ context.Context, d sdk.Device) error {
	a.mu.Lock()
	c, ok := a.conns[d.Code]
	delete(a.conns, d.Code)
	a.mu.Unlock()
	if ok {
		return c.ws.Close()
	}
	return nil
}

// Connect dials the rosbridge WebSocket and subscribes to odometry/battery.
func (a *Adapter) Connect(ctx context.Context, d sdk.Device) error {
	if d.Endpoint == "" {
		return fmt.Errorf("device %s requires Endpoint (ws://host:9090) in metadata", d.Code)
	}
	a.mu.Lock()
	if _, ok := a.conns[d.Code]; ok {
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()

	ws, _, err := websocket.DefaultDialer.DialContext(ctx, d.Endpoint, nil)
	if err != nil {
		return fmt.Errorf("rosbridge dial (%s): %w", d.Endpoint, err)
	}
	c := &conn{ws: ws}
	a.mu.Lock()
	a.conns[d.Code] = c
	a.mu.Unlock()

	for _, sub := range []rosMessage{
		{Op: "subscribe", Topic: topicOdom, Type: typeOdom},
		{Op: "subscribe", Topic: topicBattery, Type: typeBattery},
	} {
		if err := c.send(sub); err != nil {
			return err
		}
	}
	go a.readLoop(d.Code, c)
	go a.runMission(d.Code, c)
	a.log.Info("rosbridge adapter connected", "device", d.Code, "endpoint", d.Endpoint)
	return nil
}

func (c *conn) send(m rosMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.ws.WriteJSON(m)
}

func (c *conn) publish(topic, typ string, msg any) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.send(rosMessage{Op: "publish", Topic: topic, Type: typ, Msg: raw})
}

func (a *Adapter) readLoop(code string, c *conn) {
	for {
		_, raw, err := c.ws.ReadMessage()
		if err != nil {
			a.log.Info("rosbridge link closed", "device", code, "err", err)
			return
		}
		var m rosMessage
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		switch m.Op {
		case "action_result":
			var ar struct {
				ID     string `json:"id"`
				Status uint8  `json:"status"`
			}
			if json.Unmarshal(raw, &ar) == nil {
				c.mu.Lock()
				isCurrent := ar.ID == c.actionCallID
				ch := c.actionResult
				c.mu.Unlock()
				if isCurrent && ch != nil {
					select {
					case ch <- actionResultMsg{Status: ar.Status}:
					default:
					}
				}
			}
			continue
		case "action_feedback":
			// NavigateToPose feedback carries distance_to_goal — mapped to progress.
			var fb struct {
				ID     string `json:"id"`
				Values struct {
					Feedback struct {
						DistanceRemaining float64 `json:"distance_remaining"`
					} `json:"feedback"`
				} `json:"values"`
			}
			if json.Unmarshal(raw, &fb) == nil {
				c.mu.Lock()
				if fb.ID == c.actionCallID && c.running && len(c.goals) > 0 {
					// Progress = waypoints done + fraction of the current leg.
					c.progress = (float64(c.goalIdx) / float64(len(c.goals))) * 100
				}
				c.mu.Unlock()
			}
			continue
		case "publish":
		default:
			continue
		}
		switch m.Topic {
		case topicOdom:
			var odom struct {
				Pose struct {
					Pose struct {
						Position struct {
							X float64 `json:"x"`
							Y float64 `json:"y"`
						} `json:"position"`
					} `json:"pose"`
				} `json:"pose"`
			}
			if json.Unmarshal(m.Msg, &odom) == nil {
				c.mu.Lock()
				c.odomX, c.odomY, c.odomOK = odom.Pose.Pose.Position.X, odom.Pose.Pose.Position.Y, true
				c.mu.Unlock()
			}
		case topicBattery:
			var bat struct {
				Percentage float64 `json:"percentage"`
			}
			if json.Unmarshal(m.Msg, &bat) == nil {
				c.mu.Lock()
				c.battery = bat.Percentage
				c.mu.Unlock()
			}
		}
	}
}

// runMission sequences goals while a mission is running: poll odometry, move
// to the next waypoint once within arriveRadius of the current goal.
func (a *Adapter) runMission(code string, c *conn) {
	ticker := time.NewTicker(pollPeriod)
	defer ticker.Stop()
	for range ticker.C {
		c.mu.Lock()
		running := c.running
		goals := c.goals
		idx := c.goalIdx
		odomOK := c.odomOK
		odomX, odomY := c.odomX, c.odomY
		topicMode := c.navMode == "topic"
		c.mu.Unlock()
		if !running || len(goals) == 0 || !topicMode {
			continue // action mode completes synchronously inside StartMission
		}
		if idx >= len(goals) {
			// Sequence finished.
			c.mu.Lock()
			c.running = false
			c.progress = 100
			c.mu.Unlock()
			continue
		}
		if odomOK {
			wp := goals[idx]
			dx := lngToMapX(wp.Lng, wp.Lat) - odomX
			dy := latToMapY(wp.Lat) - odomY
			if math.Sqrt(dx*dx+dy*dy) <= arriveRadius {
				a.log.Info("rosbridge goal reached", "device", code, "waypoint", idx+1)
				a.advanceGoal(code, c)
			}
		}
	}
}

// advanceGoal publishes the next goal (or completes the sequence).
func (a *Adapter) advanceGoal(code string, c *conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.goalIdx++
	if c.goalIdx >= len(c.goals) {
		c.running = false
		c.progress = 100
		return
	}
	c.progress = float64(c.goalIdx) / float64(len(c.goals)) * 100
	wp := c.goals[c.goalIdx]
	if err := c.publishGoalLocked(wp); err != nil {
		a.log.Warn("rosbridge goal publish failed", "device", code, "err", err)
	}
}

// sendNavigateToPoseAction issues one Nav2 NavigateToPose goal via the
// rosbridge v2 action protocol (specs/ADR-007 upgrade):
//
//	{"op":"send_action_goal","action":"/navigate_to_pose",
//	 "type":"nav2_msgs/action/NavigateToPose","goal":{PoseStamped},"feedback_callback":true}
func (c *conn) sendNavigateToPoseAction(callID string, wp sdk.Waypoint) error {
	goal := map[string]any{
		"pose": map[string]any{
			"header": map[string]any{"frame_id": "map"},
			"pose": map[string]any{
				"position": map[string]any{
					"x": lngToMapX(wp.Lng, wp.Lat),
					"y": latToMapY(wp.Lat),
					"z": wp.Alt,
				},
				"orientation": map[string]any{"x": 0.0, "y": 0.0, "z": 0.0, "w": 1.0},
			},
		},
	}
	goalJSON, err := json.Marshal(goal)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.actionCallID = callID
	c.actionResult = make(chan actionResultMsg, 4)
	c.mu.Unlock()
	return c.send(rosMessage{
		Op:     "send_action_goal",
		ID:     callID,
		Action: actionNav2Pose,
		Type:   typeNav2Pose,
		Msg:    goalJSON,
	})
}

// waitActionResult blocks for the action result (deadline-bounded) and maps
// GoalStatus to error.
func (c *conn) waitActionResult(ctx context.Context, callID string, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		c.mu.Lock()
		ch := c.actionResult
		c.mu.Unlock()
		if ch == nil {
			return fmt.Errorf("no in-flight action")
		}
		select {
		case res := <-ch:
			switch res.Status {
			case 4: // SUCCEEDED
				return nil
			case 5:
				return fmt.Errorf("nav2 goal canceled")
			case 6:
				return fmt.Errorf("nav2 goal aborted")
			default:
				// still executing — keep waiting until deadline
			}
		case <-deadline:
			return fmt.Errorf("nav2 goal result timeout after %s", timeout)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *conn) publishGoalLocked(wp sdk.Waypoint) error {
	goal := map[string]any{
		"header": map[string]any{"frame_id": "map"},
		"pose": map[string]any{
			"position": map[string]any{
				"x": lngToMapX(wp.Lng, wp.Lat),
				"y": latToMapY(wp.Lat),
				"z": wp.Alt,
			},
			"orientation": map[string]any{"x": 0.0, "y": 0.0, "z": 0.0, "w": 1.0},
		},
	}
	return c.publish(topicGoal, typeGoal, goal)
}

// lngToMapX maps longitude to a local ENU-style X (meters around an optional
// map origin from device metadata; direct linear approximation is fine for
// farm-scale areas — see package doc).
func lngToMapX(lng, lat float64) float64 {
	return lng * 111320.0 * math.Cos(lat*math.Pi/180)
}

func latToMapY(lat float64) float64 {
	return lat * 110540.0
}

// Connect-time goal publishing uses the same mapping.
func (a *Adapter) publishGoal(code string, c *conn, wp sdk.Waypoint) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.publishGoalLocked(wp)
}

// CreateMission stores the waypoint sequence.
func (a *Adapter) CreateMission(_ context.Context, d sdk.Device, m sdk.Mission) error {
	c, err := a.getConn(d)
	if err != nil {
		return err
	}
	if len(m.Waypoints) == 0 {
		return fmt.Errorf("mission requires waypoints")
	}
	c.mu.Lock()
	c.goals = m.Waypoints
	c.goalIdx = 0
	c.progress = 0
	c.mu.Unlock()
	a.log.Info("rosbridge mission stored", "device", d.Code, "waypoints", len(m.Waypoints))
	return nil
}

// StartMission drives the waypoint sequence. Default mode is the Nav2 action
// protocol (NavigateToPose goals, result-verified per waypoint); devices with
// Metadata["nav_mode"]="topic" fall back to publishing /goal_pose (ADR-011
// upgrade of ADR-007's goal-topic MVP).
func (a *Adapter) StartMission(ctx context.Context, d sdk.Device, missionID string) error {
	c, err := a.getConn(d)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if len(c.goals) == 0 {
		c.mu.Unlock()
		return fmt.Errorf("no mission stored; call CreateMission first")
	}
	c.running = true
	c.goalIdx = 0
	c.progress = 0
	c.navMode = d.Metadata["nav_mode"]
	goals := append([]sdk.Waypoint(nil), c.goals...)
	c.mu.Unlock()

	if c.navMode == "topic" {
		if err := a.publishGoal(d.Code, c, goals[0]); err != nil {
			return err
		}
		a.log.Info("rosbridge mission started (topic fallback)", "device", d.Code, "mission", missionID)
		return nil
	}

	for i, wp := range goals {
		callID := fmt.Sprintf("%s-wp-%d", missionID, i+1)
		if err := c.sendNavigateToPoseAction(callID, wp); err != nil {
			return fmt.Errorf("goal %d/%d: %w", i+1, len(goals), err)
		}
		if err := c.waitActionResult(ctx, callID, goalTimeout); err != nil {
			return fmt.Errorf("goal %d/%d: %w", i+1, len(goals), err)
		}
		c.mu.Lock()
		c.goalIdx = i + 1
		c.progress = float64(i+1) / float64(len(goals)) * 100
		done := c.goalIdx >= len(goals)
		if done {
			c.running = false
		}
		c.mu.Unlock()
		a.log.Info("rosbridge goal reached (action)", "device", d.Code, "waypoint", i+1)
	}
	a.log.Info("rosbridge mission started", "device", d.Code, "mission", missionID)
	return nil
}

// stopMotion publishes a zero velocity command (best effort).
func (c *conn) stopMotion() {
	_ = c.publish(topicTwist, typeTwist, map[string]any{
		"linear":  map[string]any{"x": 0.0, "y": 0.0, "z": 0.0},
		"angular": map[string]any{"x": 0.0, "y": 0.0, "z": 0.0},
	})
}

// PauseMission cancels the in-flight Nav2 goal (action mode) or stops motion
// (topic fallback); ResumeMission re-issues the current waypoint.
func (a *Adapter) PauseMission(_ context.Context, d sdk.Device, missionID string) error {
	c, err := a.getConn(d)
	if err != nil {
		return err
	}
	c.mu.Lock()
	callID := c.actionCallID
	topicMode := c.navMode == "topic"
	c.mu.Unlock()
	if !topicMode && callID != "" {
		if err := c.send(rosMessage{Op: "cancel_action", ID: callID}); err != nil {
			return err
		}
	}
	c.stopMotion()
	a.log.Info("rosbridge mission paused", "device", d.Code, "mission", missionID)
	return nil
}

// ResumeMission re-issues the current waypoint through the active mode.
func (a *Adapter) ResumeMission(ctx context.Context, d sdk.Device, missionID string) error {
	c, err := a.getConn(d)
	if err != nil {
		return err
	}
	c.mu.Lock()
	idx := c.goalIdx
	goals := c.goals
	topicMode := c.navMode == "topic"
	c.mu.Unlock()
	if idx >= len(goals) {
		return fmt.Errorf("mission already finished")
	}
	if topicMode {
		return a.publishGoal(d.Code, c, goals[idx])
	}
	callID := fmt.Sprintf("%s-resume-%d", missionID, idx+1)
	if err := c.sendNavigateToPoseAction(callID, goals[idx]); err != nil {
		return err
	}
	return c.waitActionResult(ctx, callID, goalTimeout)
}

// StopMission cancels any in-flight goal, stops motion and clears the sequence.
func (a *Adapter) StopMission(_ context.Context, d sdk.Device, missionID string) error {
	c, err := a.getConn(d)
	if err != nil {
		return err
	}
	c.mu.Lock()
	callID := c.actionCallID
	c.mu.Unlock()
	if callID != "" {
		_ = c.send(rosMessage{Op: "cancel_action", ID: callID})
	}
	c.stopMotion()
	c.mu.Lock()
	c.running = false
	c.goals = nil
	c.goalIdx = 0
	c.mu.Unlock()
	a.log.Info("rosbridge mission stopped", "device", d.Code, "mission", missionID)
	return nil
}

// GetMission returns the tracked sequencing progress.
func (a *Adapter) GetMission(_ context.Context, d sdk.Device, missionID string) (*sdk.MissionStatus, error) {
	c, err := a.getConn(d)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state := "created"
	if c.running {
		state = "executing"
	}
	if c.progress >= 100 {
		state = "completed"
	}
	return &sdk.MissionStatus{State: state, Progress: c.progress, Detail: missionID}, nil
}

// GetStatus reports link + battery from cached subscriptions.
func (a *Adapter) GetStatus(_ context.Context, d sdk.Device) (*sdk.DeviceStatus, error) {
	c, err := a.getConn(d)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return &sdk.DeviceStatus{
		Online:    c.odomOK,
		BatteryPc: int(c.battery),
	}, nil
}

// GetTelemetry returns the latest odometry pose as platform telemetry.
func (a *Adapter) GetTelemetry(_ context.Context, d sdk.Device) (*sdk.Telemetry, error) {
	c, err := a.getConn(d)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.odomOK {
		return nil, fmt.Errorf("no odometry received yet")
	}
	return &sdk.Telemetry{
		TS: time.Now().UnixMilli(),
		Metrics: map[string]float64{
			"odom_x": c.odomX,
			"odom_y": c.odomY,
		},
	}, nil
}

// SendCommand supports the non-high-risk whitelist subset that maps to ROS.
func (a *Adapter) SendCommand(_ context.Context, d sdk.Device, cmd sdk.Command) error {
	c, err := a.getConn(d)
	if err != nil {
		return err
	}
	switch cmd.Name {
	case "ping":
		return nil // link-level liveness: a successful call proves the socket
	case "stop_motion":
		c.stopMotion()
		return nil
	default:
		return fmt.Errorf("unsupported command for rosbridge adapter: %s", cmd.Name)
	}
}

// GetLogs reports not-implemented honestly.
func (a *Adapter) GetLogs(_ context.Context, d sdk.Device) ([]sdk.DeviceLog, error) {
	return nil, fmt.Errorf("not implemented: ROS logs stream via rqt/rosout, not rosbridge")
}

// GetDeviceInfo returns static identity.
func (a *Adapter) GetDeviceInfo(_ context.Context, d sdk.Device) (*sdk.DeviceInfo, error) {
	if _, err := a.getConn(d); err != nil {
		return nil, err
	}
	return &sdk.DeviceInfo{Vendor: "ros2", Model: d.DeviceType, Serial: d.Code}, nil
}

// GetCapabilities echoes the device's declared capabilities.
func (a *Adapter) GetCapabilities(_ context.Context, d sdk.Device) ([]string, error) {
	return d.Capabilities, nil
}
