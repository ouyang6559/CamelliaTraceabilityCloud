// Package dji implements a Device Adapter skeleton for the DJI Cloud API
// (specs/03.6, docs/adr/adr-013) over the documented MQTT topic conventions:
//
//	thing/product/{gateway_sn}/osd             — telemetry (OSD, ~2Hz)
//	thing/product/{gateway_sn}/status          — online/offline updates
//	thing/product/{gateway_sn}/services        — service requests to the device
//	thing/product/{gateway_sn}/services_reply  — device replies
//
// Reference: https://developer.dji.com/doc/cloud-api-tutorial/en/ (public
// docs). Scope is an honest MVP: OSD telemetry parsing, status and locate —
// mission operations (flighttask/DRC) require enterprise credentials and
// hardware validation, so they fail with actionable TODO errors instead of
// fabricated calls (specs/06.11). Endpoint = MQTT broker URL that the DJI
// cloud backend publishes on (typically the deployment's EMQX); Token = MQTT
// password, Metadata["username"] = MQTT username.
package dji

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	sdk "github.com/ouyang6559/CamelliaTraceabilityCloud/adapters/device"
)

// Manifest returns the adapter registration descriptor.
func Manifest() sdk.Manifest {
	return sdk.Manifest{
		Name:         "dji-cloud",
		Version:      "0.1.0",
		Type:         "device",
		Vendor:       "dji",
		Protocols:    []string{"mqtt"},
		Capabilities: []string{sdk.CapTelemetry, sdk.CapGPS, sdk.CapPhoto},
	}
}

// osdTopic renders the documented OSD topic for a gateway serial.
func osdTopic(sn string) string { return "thing/product/" + sn + "/osd" }

// servicesTopic renders the documented services-request topic.
func servicesTopic(sn string) string { return "thing/product/" + sn + "/services" }

// osdPayload mirrors the documented OSD envelope: common envelope fields +
// a data object whose subset we parse defensively (fields vary per model).
type osdPayload struct {
	Timestamp int64 `json:"timestamp"`
	Data      struct {
		Latitude       float64 `json:"latitude"`
		Longitude      float64 `json:"longitude"`
		RelativeHeight float64 `json:"relative_height"`
		ModeCode       int     `json:"mode_code"`
		Battery        struct {
			CapacityPercent int `json:"capacity_percent"`
		} `json:"battery"`
	} `json:"data"`
}

// parseOSD converts one OSD JSON frame into platform telemetry (unit-tested).
func parseOSD(raw []byte) (*sdk.Telemetry, error) {
	var p osdPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	ts := time.Now().UnixMilli()
	if p.Timestamp > 0 {
		ts = p.Timestamp
	}
	telemetry := &sdk.Telemetry{
		TS:      ts,
		Metrics: map[string]float64{},
	}
	if p.Data.Latitude != 0 || p.Data.Longitude != 0 {
		telemetry.Position = &sdk.GeoPoint{
			Lat: p.Data.Latitude,
			Lng: p.Data.Longitude,
			Alt: p.Data.RelativeHeight,
		}
	}
	if p.Data.Battery.CapacityPercent > 0 {
		telemetry.Metrics["battery"] = float64(p.Data.Battery.CapacityPercent)
	}
	telemetry.Payload = map[string]interface{}{"mode_code": p.Data.ModeCode}
	return telemetry, nil
}

// conn is one live MQTT link (one gateway).
type conn struct {
	client mqtt.Client
	Topic  string

	mu       sync.Mutex
	telemetr *sdk.Telemetry
	battery  int
	lastSeen time.Time
}

// Adapter talks to DJI Cloud API gateways over MQTT.
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

// Connect subscribes the gateway's OSD topic (specs/08.3: credentials from
// the secret store — Token as MQTT password, Metadata["username"] as user).
func (a *Adapter) Connect(_ context.Context, d sdk.Device) error {
	if d.Endpoint == "" {
		return fmt.Errorf("device %s requires Endpoint (DJI MQTT broker URL) in metadata", d.Code)
	}
	a.mu.Lock()
	if _, ok := a.conns[d.Code]; ok {
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()

	opts := mqtt.NewClientOptions().
		AddBroker(d.Endpoint).
		SetClientID("ctc-dji-" + d.Code).
		SetUsername(d.Metadata["username"]).
		SetPassword(d.Token).
		SetAutoReconnect(true).
		SetCleanSession(true)
	client := mqtt.NewClient(opts)
	tok := client.Connect()
	if !tok.WaitTimeout(10*time.Second) || tok.Error() != nil {
		if tok.Error() != nil {
			return fmt.Errorf("connect DJI mqtt (%s): %w", d.Endpoint, tok.Error())
		}
		return fmt.Errorf("connect DJI mqtt (%s): broker not confirmed in 10s", d.Endpoint)
	}

	c := &conn{client: client, battery: -1}
	handler := func(_ mqtt.Client, msg mqtt.Message) {
		telemetry, err := parseOSD(msg.Payload())
		if err != nil {
			a.log.Warn("dji osd parse failed", "device", d.Code, "err", err)
			return
		}
		c.mu.Lock()
		c.telemetr = telemetry
		c.battery = int(telemetry.Metrics["battery"])
		c.lastSeen = time.Now()
		c.mu.Unlock()
	}
	if subTok := client.Subscribe(osdTopic(d.Code), 1, handler); !subTok.WaitTimeout(5*time.Second) || subTok.Error() != nil {
		if subTok.Error() != nil {
			return fmt.Errorf("subscribe %s: %w", osdTopic(d.Code), subTok.Error())
		}
		return fmt.Errorf("subscribe %s not confirmed in 5s", osdTopic(d.Code))
	}

	a.mu.Lock()
	a.conns[d.Code] = c
	a.mu.Unlock()
	a.log.Info("dji adapter connected", "device", d.Code, "osd_topic", osdTopic(d.Code))
	return nil
}

// Disconnect closes the link.
func (a *Adapter) Disconnect(_ context.Context, d sdk.Device) error {
	a.mu.Lock()
	c, ok := a.conns[d.Code]
	delete(a.conns, d.Code)
	a.mu.Unlock()
	if ok {
		c.client.Disconnect(500)
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

// GetTelemetry returns the latest parsed OSD snapshot.
func (a *Adapter) GetTelemetry(_ context.Context, d sdk.Device) (*sdk.Telemetry, error) {
	c, err := a.getConn(d)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.telemetr == nil {
		return nil, fmt.Errorf("no OSD received on %s yet", osdTopic(d.Code))
	}
	t := *c.telemetr
	return &t, nil
}

// GetStatus reports liveness from OSD reception.
func (a *Adapter) GetStatus(_ context.Context, d sdk.Device) (*sdk.DeviceStatus, error) {
	c, err := a.getConn(d)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	online := time.Since(c.lastSeen) < 10*time.Second
	return &sdk.DeviceStatus{
		Online:    online,
		BatteryPc: c.battery,
		LastError: onlineStatusText(online),
	}, nil
}

func onlineStatusText(online bool) string {
	if online {
		return ""
	}
	return "no OSD in the last 10s"
}

// GetCapabilities echoes the device's declared capabilities.
func (a *Adapter) GetCapabilities(_ context.Context, d sdk.Device) ([]string, error) {
	return d.Capabilities, nil
}

// GetDeviceInfo returns static identity; live firmware arrives via the
// documented device_osd/state field once validated against hardware.
func (a *Adapter) GetDeviceInfo(_ context.Context, d sdk.Device) (*sdk.DeviceInfo, error) {
	if _, err := a.getConn(d); err != nil {
		return nil, err
	}
	return &sdk.DeviceInfo{Vendor: "dji", Model: d.DeviceType, Serial: d.Code}, nil
}

// sendService publishes a documented services-request envelope and returns
// the topic it was sent on. reply handling lands with hardware validation.
func (a *Adapter) sendService(d sdk.Device, method string, params map[string]any) error {
	c, err := a.getConn(d)
	if err != nil {
		return err
	}
	envelope := map[string]any{
		"tid":       fmt.Sprint(time.Now().UnixNano()),
		"bid":       d.Code,
		"timestamp": time.Now().UnixMilli(),
		"data":      map[string]any{"method": method, "params": params},
	}
	tok := c.client.Publish(servicesTopic(d.Code), 1, false, envelope)
	if !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		if tok.Error() != nil {
			return tok.Error()
		}
		return fmt.Errorf("services publish timeout")
	}
	return nil
}

// SendCommand supports locate (fresh OSD liveness) and return_home via the
// documented services envelope; mission ops stay honest TODOs.
func (a *Adapter) SendCommand(_ context.Context, d sdk.Device, cmd sdk.Command) error {
	switch cmd.Name {
	case "ping":
		_, err := a.GetTelemetry(context.Background(), d)
		return err // OSD liveness
	case "locate":
		t, err := a.GetTelemetry(context.Background(), d)
		if err != nil {
			return err
		}
		a.log.Info("dji locate", "device", d.Code, "position", t.Position)
		return nil
	case "return_home":
		// Documented services method name; live verification pending hardware.
		return a.sendService(d, "return_home", map[string]any{})
	default:
		return fmt.Errorf("unsupported command for dji-cloud adapter: %s", cmd.Name)
	}
}

// CreateMission is an honest TODO: DJI flighttask requires enterprise
// credentials + hardware validation (specs/03.6, specs/06.11).
func (a *Adapter) CreateMission(_ context.Context, d sdk.Device, mission sdk.Mission) error {
	return fmt.Errorf("dji flighttask integration TODO: requires enterprise credentials and live hardware validation (specs/03.6)")
}

func (a *Adapter) StartMission(_ context.Context, d sdk.Device, missionID string) error {
	return fmt.Errorf("dji flighttask integration TODO (specs/03.6)")
}

func (a *Adapter) PauseMission(_ context.Context, d sdk.Device, missionID string) error {
	return fmt.Errorf("dji pause integration TODO (specs/03.6)")
}

func (a *Adapter) ResumeMission(_ context.Context, d sdk.Device, missionID string) error {
	return fmt.Errorf("dji resume integration TODO (specs/03.6)")
}

func (a *Adapter) StopMission(_ context.Context, d sdk.Device, missionID string) error {
	return fmt.Errorf("dji stop integration TODO (specs/03.6)")
}

func (a *Adapter) GetMission(_ context.Context, d sdk.Device, missionID string) (*sdk.MissionStatus, error) {
	return nil, fmt.Errorf("dji mission status integration TODO (specs/03.6)")
}

// GetLogs reports not-implemented honestly.
func (a *Adapter) GetLogs(_ context.Context, d sdk.Device) ([]sdk.DeviceLog, error) {
	return nil, fmt.Errorf("not implemented: DJI logs stream via the cloud API log module")
}
