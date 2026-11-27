package dji

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	sdk "github.com/ouyang6559/CamelliaTraceabilityCloud/adapters/device"
)

// brokerURL returns the local MQTT broker used for the loopback test
// (mosquitto on the dev machine / CI service). Skips when unreachable so CI
// environments without the service still pass.
func brokerURL(t *testing.T) string {
	t.Helper()
	for _, u := range []string{"tcp://localhost:1883", "tcp://localhost:11883"} {
		client := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(u).SetClientID("dji-probe"))
		tok := client.Connect()
		if tok.WaitTimeout(2*time.Second) && tok.Error() == nil {
			client.Disconnect(0)
			return u
		}
	}
	t.Skip("no local MQTT broker for the DJI loopback test")
	return ""
}

// TestParseOSD pins the OSD envelope parsing per the public DJI Cloud API docs.
func TestParseOSD(t *testing.T) {
	raw := []byte(`{
		"tid": "t1", "bid": "b1", "timestamp": 1700000000000,
		"data": {
			"latitude": 28.1925,
			"longitude": 113.6285,
			"relative_height": 60.5,
			"mode_code": 0,
			"battery": {"capacity_percent": 88}
		}
	}`)
	telemetry, err := parseOSD(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if telemetry.Position == nil ||
		telemetry.Position.Lat != 28.1925 ||
		telemetry.Position.Lng != 113.6285 {
		t.Errorf("position wrong: %+v", telemetry.Position)
	}
	if telemetry.Metrics["battery"] != 88 {
		t.Errorf("battery wrong: %+v", telemetry.Metrics)
	}
	if telemetry.TS != 1700000000000 {
		t.Errorf("timestamp wrong: %d", telemetry.TS)
	}
	if telemetry.Payload["mode_code"] != 0 {
		t.Errorf("mode_code wrong: %+v", telemetry.Payload)
	}
}

// TestTopics pins the documented topic conventions.
func TestTopics(t *testing.T) {
	if osdTopic("DOCK1") != "thing/product/DOCK1/osd" {
		t.Errorf("osd topic wrong: %s", osdTopic("DOCK1"))
	}
	if servicesTopic("DOCK1") != "thing/product/DOCK1/services" {
		t.Errorf("services topic wrong: %s", servicesTopic("DOCK1"))
	}
}

// TestOSDLoopback drives the adapter against a real MQTT broker: publish a
// documented OSD frame on thing/product/{sn}/osd, assert GetTelemetry/GetStatus.
func TestOSDLoopback(t *testing.T) {
	broker := brokerURL(t)
	sn := "DOCK-TEST-1"

	a := New(slog.Default())
	dev := sdk.Device{
		Code:     sn,
		Endpoint: broker,
		Token:    "",
		Metadata: map[string]string{"username": "ctc"},
	}
	ctx := context.Background()
	if err := a.Connect(ctx, dev); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer a.Disconnect(ctx, dev)

	if _, err := a.GetTelemetry(ctx, dev); err == nil {
		t.Fatal("expected no-telemetry error before any OSD arrives")
	}

	// Publish an OSD frame per the documented envelope.
	frame := map[string]any{
		"tid": "t-loop", "bid": sn, "timestamp": time.Now().UnixMilli(),
		"data": map[string]any{
			"latitude": 28.1925, "longitude": 113.6285,
			"relative_height": 60.5, "mode_code": 0,
			"battery": map[string]any{"capacity_percent": 91},
		},
	}
	raw, _ := json.Marshal(frame)
	pub := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID("dji-osd-publisher"))
	tok := pub.Connect()
	if !tok.WaitTimeout(3*time.Second) || tok.Error() != nil {
		t.Fatalf("publisher connect: %v", tok.Error())
	}
	defer pub.Disconnect(0)
	pt := pub.Publish(osdTopic(sn), 1, false, raw)
	if !pt.WaitTimeout(3*time.Second) || pt.Error() != nil {
		t.Fatalf("osd publish: %v", pt.Error())
	}

	deadline := time.Now().Add(5 * time.Second)
	var telemetry *sdk.Telemetry
	var err error
	for time.Now().Before(deadline) {
		telemetry, err = a.GetTelemetry(ctx, dev)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("no telemetry after OSD publish: %v", err)
	}
	if telemetry.Position == nil || telemetry.Position.Lat != 28.1925 {
		t.Errorf("loopback position wrong: %+v", telemetry.Position)
	}
	if telemetry.Metrics["battery"] != 91 {
		t.Errorf("loopback battery wrong: %+v", telemetry.Metrics)
	}
	st, err := a.GetStatus(ctx, dev)
	if err != nil || !st.Online || st.BatteryPc != 91 {
		t.Errorf("status wrong: %+v err=%v", st, err)
	}

	// Mission ops fail honestly (specs/03.6).
	if err := a.CreateMission(ctx, dev, sdk.Mission{ID: "M"}); err == nil ||
		!strings.Contains(err.Error(), "TODO") {
		t.Errorf("mission op must fail with honest TODO, got %v", err)
	}
	if err := a.SendCommand(ctx, dev, sdk.Command{Name: "unknown"}); err == nil {
		t.Error("unknown command must fail")
	}
}
