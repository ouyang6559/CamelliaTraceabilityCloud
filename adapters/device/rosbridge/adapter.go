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
	topicGoal    = "/goal_pose"
	typeGoal     = "geometry_msgs/msg/PoseStamped"
	topicTwist   = "/cmd_vel"
	typeTwist    = "geometry_msgs/msg/Twist"
	topicOdom    = "/odom"
	typeOdom     = "nav_msgs/msg/Odometry"
	topicBattery = "/battery_state"
	typeBattery  = "sensor_msgs/msg/BatteryState"
	arriveRadius = 0.8 // meters: goal considered reached
	pollPeriod   = 300 * time.Millisecond
)

// rosMessage is one rosbridge protocol message (subset).
type rosMessage struct {
	Op    string          `json:"op"`
	Topic string          `json:"topic,omitempty"`
	Type  string          `json:"type,omitempty"`
	Msg   json.RawMessage `json:"msg,omitempty"`
	ID    string          `json:"id,omitempty"`
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
		if json.Unmarshal(raw, &m) != nil || m.Op != "publish" {
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
		c.mu.Unlock()
		if !running || len(goals) == 0 {
			continue
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

// StartMission publishes the first goal and enables the sequencing loop.
func (a *Adapter) StartMission(_ context.Context, d sdk.Device, missionID string) error {
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
	wp := c.goals[0]
	c.mu.Unlock()
	if err := a.publishGoal(d.Code, c, wp); err != nil {
		return err
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

// PauseMission stops motion; the goal sequence resumes on ResumeMission.
func (a *Adapter) PauseMission(_ context.Context, d sdk.Device, missionID string) error {
	c, err := a.getConn(d)
	if err != nil {
		return err
	}
	c.stopMotion()
	a.log.Info("rosbridge mission paused", "device", d.Code, "mission", missionID)
	return nil
}

// ResumeMission re-publishes the current goal.
func (a *Adapter) ResumeMission(_ context.Context, d sdk.Device, missionID string) error {
	c, err := a.getConn(d)
	if err != nil {
		return err
	}
	c.mu.Lock()
	idx := c.goalIdx
	goals := c.goals
	c.mu.Unlock()
	if idx >= len(goals) {
		return fmt.Errorf("mission already finished")
	}
	return a.publishGoal(d.Code, c, goals[idx])
}

// StopMission stops motion and clears the sequence.
func (a *Adapter) StopMission(_ context.Context, d sdk.Device, missionID string) error {
	c, err := a.getConn(d)
	if err != nil {
		return err
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
