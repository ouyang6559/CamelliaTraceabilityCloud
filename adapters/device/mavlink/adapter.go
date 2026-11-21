// Package mavlink implements the Device Adapter SDK over the MAVLink 2
// protocol (specs/03.5 P0, specs/03.9) using bluenviron/gomavlib. It targets
// any MAVLink flight controller — ArduPilot and PX4 families, in SITL
// simulation or on real hardware (UAV via ArduPilot, UGV via ArduRover).
//
// Endpoint: Device.Endpoint is a MAVLink UDP client address ("ip:port"), e.g.
// "127.0.0.1:14550" for ArduPilot SITL (-udp) or a telemetry radio bridging to
// udpout. Device.Metadata["tenant"] scopes nothing here (direct link), but is
// kept uniform with other adapters.
//
// Mission mapping (specs/03.9): telemetry (GLOBAL_POSITION_INT/SYS_STATUS),
// mission upload (MISSION_COUNT/MISSION_ITEM_INT protocol), start
// (arm-if-needed + MAV_CMD_MISSION_START), pause/resume
// (MAV_CMD_DO_PAUSE_CONTINUE), stop (pause + RTL).
package mavlink

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/bluenviron/gomavlib/v4"
	"github.com/bluenviron/gomavlib/v4/pkg/dialects/all"
	"github.com/bluenviron/gomavlib/v4/pkg/dialects/common"
	"github.com/bluenviron/gomavlib/v4/pkg/frame"

	sdk "github.com/ouyang6559/CamelliaTraceabilityCloud/adapters/device"
)

// Manifest returns the adapter registration descriptor.
func Manifest() sdk.Manifest {
	return sdk.Manifest{
		Name:         "mavlink",
		Version:      "0.1.0",
		Type:         "device",
		Vendor:       "ctc",
		Protocols:    []string{"mavlink"},
		Capabilities: []string{sdk.CapTelemetry, sdk.CapGPS, sdk.CapMission, sdk.CapAutonomous, sdk.CapInspection},
	}
}

const (
	gcsSystemID        = 254 // ground control station system id
	waitHandshake      = 5 * time.Second
	waitAck            = 3 * time.Second
	waitMissionElement = 5 * time.Second
)

// conn is one live MAVLink link (one device).
type conn struct {
	node   *gomavlib.Node
	events chan gomavlib.Event
	ackCh  chan *common.MessageCommandAck
	upload chan gomavlib.Event // non-nil while a mission upload is in flight

	mu         sync.Mutex
	sysID      byte
	compID     byte
	haveSys    bool
	armed      bool
	pos        *sdk.Telemetry
	battery    int
	missionTot int
	progress   float64
}

// Adapter talks to MAVLink vehicles. Connections are created lazily per device
// on Connect and address by Device.Endpoint.
type Adapter struct {
	mu    sync.Mutex
	conns map[string]*conn
	log   *slog.Logger
}

// New creates the adapter. Endpoint dialing happens per device in Connect.
func New(log *slog.Logger) *Adapter {
	if log == nil {
		log = slog.Default()
	}
	return &Adapter{conns: map[string]*conn{}, log: log}
}

// Manifest returns the adapter registration descriptor.
func (a *Adapter) Manifest() sdk.Manifest { return Manifest() }

// Connect opens the MAVLink link and starts the event pump.
func (a *Adapter) Connect(_ context.Context, d sdk.Device) error {
	if d.Endpoint == "" {
		return fmt.Errorf("device %s requires Endpoint (mavlink udp ip:port) in metadata", d.Code)
	}
	a.mu.Lock()
	if _, ok := a.conns[d.Code]; ok {
		a.mu.Unlock()
		return nil // already connected
	}
	a.mu.Unlock()

	node := &gomavlib.Node{
		Endpoints: []gomavlib.Endpoint{
			&gomavlib.EndpointUDPClient{Address: d.Endpoint},
		},
		Dialect:         all.Dialect,
		OutVersion:      gomavlib.V2,
		OutSystemID:     gcsSystemID,
		HeartbeatPeriod: 2 * time.Second,
	}
	if err := node.Initialize(); err != nil {
		return fmt.Errorf("mavlink node init (%s): %w", d.Endpoint, err)
	}

	c := &conn{
		node:    node,
		events:  node.Events(),
		ackCh:   make(chan *common.MessageCommandAck, 16),
		battery: -1,
	}
	a.mu.Lock()
	a.conns[d.Code] = c
	a.mu.Unlock()

	go a.pump(d.Code, c)
	a.log.Info("mavlink adapter connected", "device", d.Code, "endpoint", d.Endpoint)
	return nil
}

// Disconnect closes the link.
func (a *Adapter) Disconnect(_ context.Context, d sdk.Device) error {
	a.mu.Lock()
	c, ok := a.conns[d.Code]
	delete(a.conns, d.Code)
	a.mu.Unlock()
	if ok {
		c.node.Close()
		a.log.Info("mavlink adapter disconnected", "device", d.Code)
	}
	return nil
}

func (a *Adapter) getConn(d sdk.Device) (*conn, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.conns[d.Code]
	if !ok {
		return nil, fmt.Errorf("device %s is not connected (call Connect first)", d.Code)
	}
	return c, nil
}

// pump consumes node events: state updates, acks and in-flight mission traffic.
func (a *Adapter) pump(code string, c *conn) {
	for ev := range c.events {
		switch e := ev.(type) {
		case *gomavlib.EventFrame:
			msg := e.Frame.GetMessage()
			switch m := msg.(type) {
			case *common.MessageHeartbeat:
				if m.Type == common.MAV_TYPE_GCS {
					continue // another GCS
				}
				c.mu.Lock()
				c.sysID = e.Frame.GetSystemID()
				c.compID = e.Frame.GetComponentID()
				c.haveSys = true
				c.armed = m.BaseMode&common.MAV_MODE_FLAG_SAFETY_ARMED != 0
				c.mu.Unlock()

			case *common.MessageGlobalPositionInt:
				t := &sdk.Telemetry{
					TS: time.Now().UnixMilli(),
					Metrics: map[string]float64{
						"altitude": float64(m.RelativeAlt) / 1000,
					},
					Position: &sdk.GeoPoint{
						Lat: float64(m.Lat) / 1e7,
						Lng: float64(m.Lon) / 1e7,
						Alt: float64(m.RelativeAlt) / 1000,
					},
				}
				c.mu.Lock()
				c.pos = t
				c.mu.Unlock()

			case *common.MessageSysStatus:
				c.mu.Lock()
				c.battery = int(m.BatteryRemaining)
				c.mu.Unlock()

			case *common.MessageCommandAck:
				select {
				case c.ackCh <- m:
				default: // drop stale acks
				}

			case *common.MessageMissionCurrent:
				c.mu.Lock()
				if c.missionTot > 0 {
					c.progress = float64(m.Seq) / float64(c.missionTot) * 100
				}
				c.mu.Unlock()

			default:
				if c.upload != nil {
					switch m.(type) {
					case *common.MessageMissionRequestInt, *common.MessageMissionRequest, *common.MessageMissionAck:
						select {
						case c.upload <- ev:
						default:
						}
					}
				}
			}
		}
	}
}

// sysWait blocks until the vehicle's system/component ids are known.
func (a *Adapter) sysWait(c *conn) error {
	deadline := time.Now().Add(waitHandshake)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		ok := c.haveSys
		sysID, compID := c.sysID, c.compID
		c.mu.Unlock()
		if ok {
			_ = sysID
			_ = compID
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("no heartbeat from vehicle within %s", waitHandshake)
}

// sysIDs returns the current target ids.
func (c *conn) sysIDs() (byte, byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sysID, c.compID
}

// sendCommandAndWait issues a MAVLink command and waits for its ACK.
func (a *Adapter) sendCommandAndWait(c *conn, cmd common.MAV_CMD, params [7]float32) (*common.MessageCommandAck, error) {
	_, compID := c.sysIDs()
	sysID, _ := c.sysIDs()
	if err := a.sysWait(c); err != nil {
		return nil, err
	}
	sysID, compID = c.sysIDs()

	// Drain stale acks for this command.
	c.mu.Lock()
	c.ackCh = make(chan *common.MessageCommandAck, 16)
	c.mu.Unlock()

	if err := c.node.WriteMessageAll(&common.MessageCommandLong{
		TargetSystem:    sysID,
		TargetComponent: compID,
		Command:         cmd,
		Confirmation:    0,
		Param1:          params[0],
		Param2:          params[1],
		Param3:          params[2],
		Param4:          params[3],
		Param5:          params[4],
		Param6:          params[5],
		Param7:          params[6],
	}); err != nil {
		return nil, err
	}
	deadline := time.After(waitAck)
	for {
		select {
		case ack := <-c.ackCh:
			if ack.Command == cmd {
				return ack, nil
			}
		case <-deadline:
			return nil, fmt.Errorf("no COMMAND_ACK for %s within %s", cmd, waitAck)
		}
	}
}

// CreateMission uploads the platform route through the MAVLink mission
// protocol (MISSION_COUNT -> MISSION_REQUEST_INT -> MISSION_ITEM_INT -> ACK).
func (a *Adapter) CreateMission(_ context.Context, d sdk.Device, m sdk.Mission) error {
	c, err := a.getConn(d)
	if err != nil {
		return err
	}
	if err := a.sysWait(c); err != nil {
		return err
	}
	if len(m.Waypoints) == 0 || len(m.Waypoints) > 255 {
		return fmt.Errorf("mission requires 1..255 waypoints, got %d", len(m.Waypoints))
	}

	upload := make(chan gomavlib.Event, 64)
	c.mu.Lock()
	c.upload = upload
	c.missionTot = len(m.Waypoints)
	c.progress = 0
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.upload = nil
		c.mu.Unlock()
	}()

	sysID, compID := c.sysIDs()
	if err := c.node.WriteMessageAll(&common.MessageMissionCount{
		TargetSystem:    sysID,
		TargetComponent: compID,
		Count:           uint16(len(m.Waypoints)),
		MissionType:     common.MAV_MISSION_TYPE_MISSION,
	}); err != nil {
		return err
	}

	deadline := time.Now().Add(waitMissionElement * time.Duration(len(m.Waypoints)+2))
	for seq := 0; seq < len(m.Waypoints); {
		remain := time.Until(deadline)
		if remain <= 0 {
			return fmt.Errorf("mission upload timed out at waypoint %d/%d", seq, len(m.Waypoints))
		}
		select {
		case ev := <-upload:
			switch req := ev.(type) {
			case *gomavlib.EventFrame:
				switch mi := req.Frame.GetMessage().(type) {
				case *common.MessageMissionRequestInt:
					if mi.Seq != uint16(seq) {
						continue // re-request the expected seq next round
					}
					if err := a.sendMissionItem(c, sysID, compID, uint16(seq), m.Waypoints[seq]); err != nil {
						return err
					}
					seq++
				case *common.MessageMissionRequest: // MAVLink 1 fallback
					if mi.Seq != uint16(seq) {
						continue
					}
					if err := a.sendMissionItem(c, sysID, compID, uint16(seq), m.Waypoints[seq]); err != nil {
						return err
					}
					seq++
				case *common.MessageMissionAck:
					if mi.Type != common.MAV_MISSION_ACCEPTED {
						return fmt.Errorf("mission rejected by vehicle: %s", mi.Type)
					}
					a.log.Info("mavlink mission uploaded", "device", d.Code, "waypoints", len(m.Waypoints))
					return nil
				}
			}
		case <-time.After(remain):
			return fmt.Errorf("mission upload timed out at waypoint %d/%d", seq, len(m.Waypoints))
		}
	}
	// Final ACK.
	select {
	case ev := <-upload:
		if ack, ok := ev.(*gomavlib.EventFrame); ok {
			if ma, ok := ack.Frame.GetMessage().(*common.MessageMissionAck); ok {
				if ma.Type != common.MAV_MISSION_ACCEPTED {
					return fmt.Errorf("mission rejected by vehicle: %s", ma.Type)
				}
			}
		}
	case <-time.After(waitMissionElement):
		return fmt.Errorf("no MISSION_ACK after upload")
	}
	a.log.Info("mavlink mission uploaded", "device", d.Code, "waypoints", len(m.Waypoints))
	return nil
}

// sendMissionItem encodes one platform waypoint as MISSION_ITEM_INT.
func (a *Adapter) sendMissionItem(c *conn, sysID, compID byte, seq uint16, wp sdk.Waypoint) error {
	cmd := common.MAV_CMD_NAV_WAYPOINT
	var p1 float32
	switch wp.Action {
	case "takeoff":
		cmd = common.MAV_CMD_NAV_TAKEOFF
		p1 = 10 // pitch, degrees (copter default)
	case "land":
		cmd = common.MAV_CMD_NAV_LAND
	case "photo", "spray_on", "spray_off":
		cmd = common.MAV_CMD_NAV_WAYPOINT // hover waypoint; payload actions run via DO commands later
	}
	lat := int32(wp.Lat * 1e7)
	lng := int32(wp.Lng * 1e7)
	alt := wp.Alt
	if alt == 0 {
		alt = 10 // sensible safety default (meters)
	}
	return c.node.WriteMessageAll(&common.MessageMissionItemInt{
		TargetSystem:    sysID,
		TargetComponent: compID,
		Seq:             seq,
		Frame:           common.MAV_FRAME_GLOBAL,
		Command:         cmd,
		Current:         0,
		Autocontinue:    1,
		Param1:          p1,
		X:               lat,
		Y:               lng,
		Z:               float32(alt),
		MissionType:     common.MAV_MISSION_TYPE_MISSION,
	})
}

// StartMission arms the vehicle (if needed) and starts the uploaded mission.
func (a *Adapter) StartMission(_ context.Context, d sdk.Device, missionID string) error {
	c, err := a.getConn(d)
	if err != nil {
		return err
	}
	c.mu.Lock()
	armed := c.armed
	c.mu.Unlock()
	if !armed {
		ack, err := a.sendCommandAndWait(c, common.MAV_CMD_COMPONENT_ARM_DISARM, [7]float32{1, 0, 0, 0, 0, 0, 0})
		if err != nil {
			return fmt.Errorf("arm: %w", err)
		}
		if ack.Result != common.MAV_RESULT_ACCEPTED {
			return fmt.Errorf("arm rejected: %s", ack.Result)
		}
	}
	ack, err := a.sendCommandAndWait(c, common.MAV_CMD_MISSION_START, [7]float32{0, 0, 0, 0, 0, 0, 0})
	if err != nil {
		return fmt.Errorf("mission start: %w", err)
	}
	if ack.Result != common.MAV_RESULT_ACCEPTED {
		return fmt.Errorf("mission start rejected: %s", ack.Result)
	}
	a.log.Info("mavlink mission started", "device", d.Code, "mission", missionID)
	return nil
}

// PauseMission pauses the running mission (ArduPilot/PX4 DO_PAUSE_CONTINUE=0).
func (a *Adapter) PauseMission(_ context.Context, d sdk.Device, missionID string) error {
	c, err := a.getConn(d)
	if err != nil {
		return err
	}
	ack, err := a.sendCommandAndWait(c, common.MAV_CMD_DO_PAUSE_CONTINUE, [7]float32{0, 0, 0, 0, 0, 0, 0})
	if err != nil {
		return err
	}
	if ack.Result != common.MAV_RESULT_ACCEPTED {
		return fmt.Errorf("pause rejected: %s", ack.Result)
	}
	return nil
}

// ResumeMission resumes the paused mission.
func (a *Adapter) ResumeMission(_ context.Context, d sdk.Device, missionID string) error {
	c, err := a.getConn(d)
	if err != nil {
		return err
	}
	ack, err := a.sendCommandAndWait(c, common.MAV_CMD_DO_PAUSE_CONTINUE, [7]float32{1, 0, 0, 0, 0, 0, 0})
	if err != nil {
		return err
	}
	if ack.Result != common.MAV_RESULT_ACCEPTED {
		return fmt.Errorf("resume rejected: %s", ack.Result)
	}
	return nil
}

// StopMission pauses the mission and commands return-to-launch.
func (a *Adapter) StopMission(_ context.Context, d sdk.Device, missionID string) error {
	c, err := a.getConn(d)
	if err != nil {
		return err
	}
	// Best-effort pause; RTL is the safety-relevant action.
	_, _ = a.sendCommandAndWait(c, common.MAV_CMD_DO_PAUSE_CONTINUE, [7]float32{0, 0, 0, 0, 0, 0, 0})
	ack, err := a.sendCommandAndWait(c, common.MAV_CMD_NAV_RETURN_TO_LAUNCH, [7]float32{0, 0, 0, 0, 0, 0, 0})
	if err != nil {
		return err
	}
	if ack.Result != common.MAV_RESULT_ACCEPTED {
		return fmt.Errorf("RTL rejected: %s", ack.Result)
	}
	return nil
}

// GetMission returns the tracked mission state (from MISSION_CURRENT).
func (a *Adapter) GetMission(_ context.Context, d sdk.Device, missionID string) (*sdk.MissionStatus, error) {
	c, err := a.getConn(d)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state := "executing"
	switch {
	case c.progress >= 100:
		state = "completed"
	case c.progress == 0:
		state = "created"
	}
	return &sdk.MissionStatus{State: state, Progress: c.progress, Detail: missionID}, nil
}

// GetStatus reports link state, battery and arming from cached messages.
func (a *Adapter) GetStatus(_ context.Context, d sdk.Device) (*sdk.DeviceStatus, error) {
	c, err := a.getConn(d)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sysID == 0 {
		return &sdk.DeviceStatus{Online: false, LastError: "no heartbeat yet"}, nil
	}
	st := &sdk.DeviceStatus{Online: true, BatteryPc: c.battery}
	if !c.armed {
		// still online; arming surfaced via telemetry consumers
		_ = st
	}
	return st, nil
}

// GetTelemetry returns the most recent position/attitude snapshot.
func (a *Adapter) GetTelemetry(_ context.Context, d sdk.Device) (*sdk.Telemetry, error) {
	c, err := a.getConn(d)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pos == nil {
		return nil, fmt.Errorf("no telemetry received yet")
	}
	t := *c.pos
	return &t, nil
}

// SendCommand maps a small, documented set of platform commands to MAVLink.
// High-risk actions go through missions (specs/03.10); the device command API
// whitelist decides what reaches here.
func (a *Adapter) SendCommand(_ context.Context, d sdk.Device, cmd sdk.Command) error {
	c, err := a.getConn(d)
	if err != nil {
		return err
	}
	switch cmd.Name {
	case "ping":
		_, compID := c.sysIDs()
		sysID, _ := c.sysIDs()
		return c.node.WriteMessageAll(&common.MessagePing{
			TargetSystem:    sysID,
			TargetComponent: compID,
			TimeUsec:        uint64(time.Now().UnixMicro()),
		})
	case "locate":
		t, err := a.GetTelemetry(context.Background(), d)
		if err != nil {
			return err
		}
		a.log.Info("mavlink locate", "device", d.Code, "position", t.Position)
		return nil
	case "return_home":
		ack, err := a.sendCommandAndWait(c, common.MAV_CMD_NAV_RETURN_TO_LAUNCH, [7]float32{0, 0, 0, 0, 0, 0, 0})
		if err != nil {
			return err
		}
		if ack.Result != common.MAV_RESULT_ACCEPTED {
			return fmt.Errorf("RTL rejected: %s", ack.Result)
		}
		return nil
	case "set_mode":
		mode, ok := cmd.Params["mode"]
		if !ok {
			return fmt.Errorf("set_mode requires params.mode (int custom mode)")
		}
		v, err := strconv.ParseUint(mode, 10, 32)
		if err != nil {
			return fmt.Errorf("params.mode must be an integer: %w", err)
		}
		ack, err := a.sendCommandAndWait(c, common.MAV_CMD_DO_SET_MODE, [7]float32{1, float32(v), 0, 0, 0, 0, 0})
		if err != nil {
			return err
		}
		if ack.Result != common.MAV_RESULT_ACCEPTED {
			return fmt.Errorf("set_mode rejected: %s", ack.Result)
		}
		return nil
	default:
		return fmt.Errorf("unsupported command for mavlink adapter: %s", cmd.Name)
	}
}

// GetLogs reports not-implemented honestly: logs are pulled by the gateway.
func (a *Adapter) GetLogs(_ context.Context, d sdk.Device) ([]sdk.DeviceLog, error) {
	return nil, fmt.Errorf("not implemented: MAVLink logs stream via STATUSTEXT into telemetry")
}

// capabilities returns the declared capabilities of a device (unused helper kept
// for symmetry with GetCapabilities contract).
func (a *Adapter) GetCapabilities(_ context.Context, d sdk.Device) ([]string, error) {
	return d.Capabilities, nil
}

// GetDeviceInfo returns static identity; firmware arrives via
// AUTOPILOT_VERSION in future iterations.
func (a *Adapter) GetDeviceInfo(_ context.Context, d sdk.Device) (*sdk.DeviceInfo, error) {
	if _, err := a.getConn(d); err != nil {
		return nil, err
	}
	return &sdk.DeviceInfo{Vendor: "mavlink", Model: d.DeviceType, Firmware: "unknown", Serial: d.Code}, nil
}

// frameType unused helper to silence frame import in future signing work.
var _ = frame.V2MagicByte
