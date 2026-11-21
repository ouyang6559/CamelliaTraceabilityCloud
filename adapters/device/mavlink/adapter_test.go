package mavlink

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/bluenviron/gomavlib/v4"
	"github.com/bluenviron/gomavlib/v4/pkg/dialects/all"
	"github.com/bluenviron/gomavlib/v4/pkg/dialects/common"

	sdk "github.com/ouyang6559/CamelliaTraceabilityCloud/adapters/device"
)

// mockVehicle is a protocol-level ArduPilot stand-in: heartbeats, the mission
// upload protocol (MISSION_COUNT -> REQUEST_INT -> ITEM_INT -> ACK) and
// COMMAND_ACK generation — everything over real MAVLink framing on UDP.
type mockVehicle struct {
	node *gomavlib.Node
	log  *slog.Logger

	mu       sync.Mutex
	items    []common.MessageMissionItemInt
	commands []common.MessageCommandLong
	expected uint16
	accepted bool // false = reject mission upload
	armed    bool
}

type testLogWriter struct{ t *testing.T }

func (w testLogWriter) Write(p []byte) (int, error) {
	w.t.Logf("%s", string(p))
	return len(p), nil
}

func newMockVehicle(t *testing.T, address string) *mockVehicle {
	t.Helper()
	v := &mockVehicle{log: slog.New(slog.NewTextHandler(testLogWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	node := &gomavlib.Node{
		Endpoints: []gomavlib.Endpoint{
			&gomavlib.EndpointUDPServer{Address: address},
		},
		Dialect:                all.Dialect,
		OutVersion:             gomavlib.V2,
		OutSystemID:            1, // vehicle
		HeartbeatDisable:       false,
		HeartbeatPeriod:        200 * time.Millisecond,
		HeartbeatSystemType:    int(common.MAV_TYPE_QUADROTOR),
		HeartbeatAutopilotType: int(common.MAV_AUTOPILOT_ARDUPILOTMEGA),
	}
	if err := node.Initialize(); err != nil {
		t.Fatalf("mock vehicle init: %v", err)
	}
	v.node = node
	go v.run()
	return v
}

func (v *mockVehicle) run() {
	for ev := range v.node.Events() {
		fr, ok := ev.(*gomavlib.EventFrame)
		if !ok {
			continue
		}
		switch m := fr.Frame.GetMessage().(type) {
		case *common.MessageHeartbeat:
			continue // our own heartbeats keep the GCS side alive

		case *common.MessageMissionCount:
			if !v.accepted {
				v.node.WriteMessageTo(fr.Channel, &common.MessageMissionAck{
					TargetSystem:    fr.Frame.GetSystemID(),
					TargetComponent: fr.Frame.GetComponentID(),
					Type:            common.MAV_MISSION_ERROR,
				})
				continue
			}
			v.mu.Lock()
			v.items = nil
			v.expected = m.Count
			v.mu.Unlock()
			v.requestSeq(fr.Channel, fr.Frame.GetSystemID(), fr.Frame.GetComponentID(), 0, m.Count)

		case *common.MessageMissionItemInt:
			v.mu.Lock()
			v.items = append(v.items, *m)
			seq := int(m.Seq) + 1
			expected := int(v.expected)
			v.mu.Unlock()
			if seq >= expected {
				v.node.WriteMessageTo(fr.Channel, &common.MessageMissionAck{
					TargetSystem:    fr.Frame.GetSystemID(),
					TargetComponent: fr.Frame.GetComponentID(),
					Type:            common.MAV_MISSION_ACCEPTED,
				})
			} else {
				v.requestSeq(fr.Channel, fr.Frame.GetSystemID(), fr.Frame.GetComponentID(), seq, v.expected)
			}

		case *common.MessageCommandLong:
			v.mu.Lock()
			v.commands = append(v.commands, *m)
			switch m.Command {
			case common.MAV_CMD_COMPONENT_ARM_DISARM:
				if m.Param1 == 1 {
					v.armed = true
				} else {
					v.armed = false
				}
			}
			v.mu.Unlock()
			v.node.WriteMessageTo(fr.Channel, &common.MessageCommandAck{
				Command: m.Command,
				Result:  common.MAV_RESULT_ACCEPTED,
			})
		}
	}
}

func (v *mockVehicle) requestSeq(ch *gomavlib.Channel, sysID, compID byte, seq int, count uint16) {
	v.node.WriteMessageTo(ch, &common.MessageMissionRequestInt{
		TargetSystem:    sysID,
		TargetComponent: compID,
		Seq:             uint16(seq),
		MissionType:     common.MAV_MISSION_TYPE_MISSION,
	})
	_ = count
}

func (v *mockVehicle) close() { v.node.Close() }

func testWaypoints() []sdk.Waypoint {
	return []sdk.Waypoint{
		{Lat: 28.1905, Lng: 113.616, Alt: 30, Action: "takeoff"},
		{Lat: 28.1980, Lng: 113.624, Alt: 40, Action: "photo"},
		{Lat: 28.1960, Lng: 113.6275, Alt: 30, Action: "land"},
	}
}

// TestMissionUploadAndLifecycle drives the full adapter path against the mock
// vehicle over real MAVLink framing: upload -> arm -> mission start -> pause
// -> resume -> stop (RTL).
func TestMissionUploadAndLifecycle(t *testing.T) {
	vehicle := newMockVehicle(t, "127.0.0.1:14581")
	defer vehicle.close()
	vehicle.accepted = true

	a := New(slog.Default())
	dev := sdk.Device{
		Code:         "UAV-TEST",
		DeviceType:   sdk.TypeUAV,
		Endpoint:     "127.0.0.1:14581",
		Capabilities: []string{sdk.CapMission, sdk.CapGPS},
	}

	ctx := context.Background()
	if err := a.Connect(ctx, dev); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer a.Disconnect(ctx, dev)

	mission := sdk.Mission{ID: "M1", Type: "inspect", Waypoints: testWaypoints()}
	if err := a.CreateMission(ctx, dev, mission); err != nil {
		t.Fatalf("create mission: %v", err)
	}

	vehicle.mu.Lock()
	items := vehicle.items
	vehicle.mu.Unlock()
	if len(items) != 3 {
		t.Fatalf("expected 3 mission items, got %d", len(items))
	}
	first := items[0]
	if first.Command != common.MAV_CMD_NAV_TAKEOFF {
		t.Errorf("first item command = %s, want NAV_TAKEOFF", first.Command)
	}
	if first.X != int32(28.1905*1e7) || first.Y != int32(113.616*1e7) {
		t.Errorf("first item coords = (%d,%d)", first.X, first.Y)
	}
	if first.Frame != common.MAV_FRAME_GLOBAL {
		t.Errorf("frame = %s, want GLOBAL", first.Frame)
	}

	if err := a.StartMission(ctx, dev, "M1"); err != nil {
		t.Fatalf("start mission: %v", err)
	}
	vehicle.mu.Lock()
	commands := vehicle.commands
	armed := vehicle.armed
	vehicle.mu.Unlock()
	if !armed {
		t.Error("vehicle should be armed after StartMission")
	}
	hasArm, hasStart := false, false
	for _, c := range commands {
		if c.Command == common.MAV_CMD_COMPONENT_ARM_DISARM && c.Param1 == 1 {
			hasArm = true
		}
		if c.Command == common.MAV_CMD_MISSION_START {
			hasStart = true
		}
	}
	if !hasArm || !hasStart {
		t.Errorf("expected ARM + MISSION_START commands, got %v", commands)
	}

	if err := a.PauseMission(ctx, dev, "M1"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := a.ResumeMission(ctx, dev, "M1"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if err := a.StopMission(ctx, dev, "M1"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	vehicle.mu.Lock()
	last := vehicle.commands[len(vehicle.commands)-1]
	vehicle.mu.Unlock()
	if last.Command != common.MAV_CMD_NAV_RETURN_TO_LAUNCH {
		t.Errorf("stop should end with RTL, got %s", last.Command)
	}
}

// TestMissionRejected verifies the adapter surfaces vehicle rejection.
func TestMissionRejected(t *testing.T) {
	vehicle := newMockVehicle(t, "127.0.0.1:14582")
	defer vehicle.close()
	vehicle.accepted = false // vehicle refuses the upload

	a := New(slog.Default())
	dev := sdk.Device{Code: "UAV-REJ", DeviceType: sdk.TypeUAV, Endpoint: "127.0.0.1:14582"}
	ctx := context.Background()
	if err := a.Connect(ctx, dev); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer a.Disconnect(ctx, dev)

	mission := sdk.Mission{ID: "M2", Type: "inspect", Waypoints: testWaypoints()}
	if err := a.CreateMission(ctx, dev, mission); err == nil {
		t.Fatal("expected mission rejection error, got nil")
	}
}

// TestSendCommandPing exercises the device command path.
func TestSendCommandPing(t *testing.T) {
	vehicle := newMockVehicle(t, "127.0.0.1:14583")
	defer vehicle.close()

	a := New(slog.Default())
	dev := sdk.Device{Code: "UAV-PING", DeviceType: sdk.TypeUAV, Endpoint: "127.0.0.1:14583"}
	ctx := context.Background()
	if err := a.Connect(ctx, dev); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer a.Disconnect(ctx, dev)

	if err := a.SendCommand(ctx, dev, sdk.Command{Name: "ping"}); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := a.SendCommand(ctx, dev, sdk.Command{Name: "unknown"}); err == nil {
		t.Fatal("unknown command should fail")
	}
}
