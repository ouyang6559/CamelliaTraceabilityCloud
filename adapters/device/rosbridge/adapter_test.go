package rosbridge

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	sdk "github.com/ouyang6559/CamelliaTraceabilityCloud/adapters/device"
)

// mockRosbridge is a protocol-level rosbridge_server stand-in: it records
// every operation (advertise/subscribe/publish) and feeds odometry back so
// the adapter's goal-sequencing loop can run for real.
type mockRosbridge struct {
	t  *testing.T
	mu sync.Mutex

	subscribed map[string]string // topic -> type
	published  []rosMessage
}

func (m *mockRosbridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	up := websocket.Upgrader{}
	ws, err := up.Upgrade(w, r, nil)
	if err != nil {
		m.t.Logf("upgrade failed: %v", err)
		return
	}
	defer ws.Close()

	// Feed odometry so the sequencing loop sees arrival.
	go func() {
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			m.mu.Lock()
			goals := len(m.published)
			m.mu.Unlock()
			odom := map[string]any{
				"pose": map[string]any{
					"pose": map[string]any{
						"position": map[string]any{"x": 0, "y": 0},
					},
				},
			}
			// Simulate arrival at each published goal one tick after publish.
			if goals > 0 {
				m.mu.Lock()
				last := m.published[len(m.published)-1]
				m.mu.Unlock()
				var msg struct {
					Topic string `json:"topic"`
				}
				_ = json.Unmarshal(last.Msg, &struct{}{})
				_ = msg
				if last.Topic == topicGoal {
					var pose struct {
						Pose struct {
							Position struct {
								X float64 `json:"x"`
								Y float64 `json:"y"`
							} `json:"position"`
						} `json:"pose"`
					}
					if json.Unmarshal(last.Msg, &pose) == nil {
						odom["pose"].(map[string]any)["pose"].(map[string]any)["position"].(map[string]any)["x"] = pose.Pose.Position.X
						odom["pose"].(map[string]any)["pose"].(map[string]any)["position"].(map[string]any)["y"] = pose.Pose.Position.Y
					}
				}
			}
			raw, _ := json.Marshal(odom)
			if err := ws.WriteJSON(rosMessage{Op: "publish", Topic: topicOdom, Msg: raw}); err != nil {
				return
			}
		}
	}()

	for {
		_, raw, err := ws.ReadMessage()
		if err != nil {
			return
		}
		var op rosMessage
		if json.Unmarshal(raw, &op) != nil {
			continue
		}
		m.mu.Lock()
		switch op.Op {
		case "advertise":
			m.subscribed[op.Topic] = op.Type
		case "subscribe":
			m.subscribed[op.Topic] = op.Type
		case "publish":
			m.published = append(m.published, op)
		}
		m.mu.Unlock()
	}
}

func goalCount(m *mockRosbridge) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, p := range m.published {
		if p.Topic == topicGoal {
			n++
		}
	}
	return n
}

func publishedTwists(m *mockRosbridge) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, p := range m.published {
		if p.Topic == topicTwist {
			n++
		}
	}
	return n
}

// TestMissionGoalSequence drives Connect -> CreateMission -> StartMission and
// verifies the real rosbridge JSON protocol on the wire: advertise ops, goal
// PoseStamped publications and arrival-driven sequencing to the next waypoint.
func TestMissionGoalSequence(t *testing.T) {
	mock := &mockRosbridge{t: t, subscribed: map[string]string{}}
	server := httptest.NewServer(mock)
	defer server.Close()
	// ws URLs from httptest are http:// — rewrite to ws://
	wsURL := "ws" + server.URL[4:]

	a := New(slog.Default())
	dev := sdk.Device{
		Code:         "UGV-TEST",
		DeviceType:   sdk.TypeUGV,
		Endpoint:     wsURL,
		Capabilities: []string{sdk.CapMission, sdk.CapMow},
	}
	ctx := context.Background()
	if err := a.Connect(ctx, dev); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer a.Disconnect(ctx, dev)

	// Wait for the mock to see the subscriptions.
	deadline := time.Now().Add(3 * time.Second)
	for {
		mock.mu.Lock()
		n := len(mock.subscribed)
		mock.mu.Unlock()
		if n >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	mission := sdk.Mission{ID: "M1", Type: "mow", Waypoints: []sdk.Waypoint{
		{Lat: 28.1900, Lng: 113.6100, Alt: 0},
		{Lat: 28.1910, Lng: 113.6110, Alt: 0},
	}}
	if err := a.CreateMission(ctx, dev, mission); err != nil {
		t.Fatalf("create mission: %v", err)
	}
	if err := a.StartMission(ctx, dev, "M1"); err != nil {
		t.Fatalf("start mission: %v", err)
	}

	// The mock echoes odometry at each published goal -> the adapter should
	// sequence both waypoints and finish (progress 100).
	deadline = time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		st, err := a.GetMission(ctx, dev, "M1")
		if err == nil && st.State == "completed" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	st, err := a.GetMission(ctx, dev, "M1")
	if err != nil {
		t.Fatalf("get mission: %v", err)
	}
	if st.State != "completed" || st.Progress != 100 {
		t.Fatalf("mission not completed: %+v", st)
	}

	if got := goalCount(mock); got != 2 {
		t.Errorf("expected 2 goal publications, got %d", got)
	}
	mock.mu.Lock()
	_, hasGoalSub := mock.subscribed[topicOdom]
	_, hasBatterySub := mock.subscribed[topicBattery]
	mock.mu.Unlock()
	if !hasGoalSub || !hasBatterySub {
		t.Errorf("missing subscribe ops: %+v", mock.subscribed)
	}

	// Pause publishes a zero velocity command.
	if err := a.PauseMission(ctx, dev, "M1"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	// The publish is async from the mock reader's perspective — poll briefly.
	twists := 0
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if twists = publishedTwists(mock); twists > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if twists == 0 {
		t.Error("pause should publish a zero Twist on /cmd_vel")
	}
	if err := a.StopMission(ctx, dev, "M1"); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

// TestCommandWhitelist verifies unsupported commands fail honestly.
func TestCommandWhitelist(t *testing.T) {
	mock := &mockRosbridge{t: t, subscribed: map[string]string{}}
	server := httptest.NewServer(mock)
	defer server.Close()
	wsURL := "ws" + server.URL[4:]

	a := New(slog.Default())
	dev := sdk.Device{Code: "UGV-CMD", DeviceType: sdk.TypeUGV, Endpoint: wsURL}
	ctx := context.Background()
	if err := a.Connect(ctx, dev); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer a.Disconnect(ctx, dev)

	if err := a.SendCommand(ctx, dev, sdk.Command{Name: "ping"}); err != nil {
		t.Errorf("ping should succeed: %v", err)
	}
	if err := a.SendCommand(ctx, dev, sdk.Command{Name: "takeoff"}); err == nil {
		t.Error("unknown command should fail")
	}
}
