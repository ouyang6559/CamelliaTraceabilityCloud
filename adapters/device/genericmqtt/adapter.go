// Package genericmqtt implements the Device Adapter SDK over plain MQTT
// (specs/03.5 P0 protocol): devices publish telemetry to
// ctc/v1/tenants/{tenant}/devices/{code}/telemetry and receive mission
// commands on ctc/v1/tenants/{tenant}/devices/{code}/commands.
//
// This adapter is the reference integration; the platform-side ingest is in
// apps/api/internal/modules/device. Vendor adapters (MAVLink, ROS 2, DJI, XAG)
// follow the same shape with their own protocol bridges.
package genericmqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/adapters/device"
)

// Manifest returns the adapter registration descriptor.
func Manifest() device.Manifest {
	return device.Manifest{
		Name:    "generic-mqtt",
		Version: "0.1.0",
		Type:    "device",
		Vendor:  "ctc",
		Protocols: []string{"mqtt"},
		Capabilities: []string{
			device.CapTelemetry, device.CapGPS, device.CapMission,
			device.CapInspection, device.CapMapping,
		},
	}
}

// Adapter talks to any MQTT-capable device.
type Adapter struct {
	client mqtt.Client
	log    *slog.Logger

	mu       sync.Mutex
	statuses map[string]*device.DeviceStatus // key: device.Code
	missions map[string]*device.MissionStatus
}

// New connects to the broker.
func New(brokerURL string, log *slog.Logger) (*Adapter, error) {
	opts := mqtt.NewClientOptions().
		AddBroker(brokerURL).
		SetClientID("ctc-adapter-" + fmt.Sprint(time.Now().UnixNano()%100000)).
		SetAutoReconnect(true).
		SetCleanSession(true)
	client := mqtt.NewClient(opts)
	if tok := client.Connect(); !tok.WaitTimeout(10*time.Second) || tok.Error() != nil {
		if tok.Error() != nil {
			return nil, fmt.Errorf("connect mqtt: %w", tok.Error())
		}
		if log != nil {
			log.Warn("mqtt adapter connect not confirmed yet; continuing")
		}
	}
	return &Adapter{
		client:   client,
		log:      log,
		statuses: map[string]*device.DeviceStatus{},
		missions: map[string]*device.MissionStatus{},
	}, nil
}

func topic(tenant, code, suffix string) string {
	return fmt.Sprintf("ctc/v1/tenants/%s/devices/%s/%s", tenant, code, suffix)
}

func (a *Adapter) publish(tenant, code, suffix string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	tok := a.client.Publish(topic(tenant, code, suffix), 0, false, data)
	if !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		if tok.Error() != nil {
			return tok.Error()
		}
		return fmt.Errorf("publish timeout")
	}
	return nil
}

func (a *Adapter) Connect(_ context.Context, d device.Device) error {
	a.mu.Lock()
	a.statuses[d.Code] = &device.DeviceStatus{Online: false}
	a.mu.Unlock()
	a.log.Info("adapter connect", "device", d.Code)
	return nil
}

func (a *Adapter) Disconnect(_ context.Context, d device.Device) error {
	a.mu.Lock()
	if st := a.statuses[d.Code]; st != nil {
		st.Online = false
	}
	a.mu.Unlock()
	return nil
}

func (a *Adapter) GetDeviceInfo(_ context.Context, d device.Device) (*device.DeviceInfo, error) {
	return &device.DeviceInfo{Vendor: "generic", Model: d.DeviceType, Firmware: "unknown", Serial: d.Code}, nil
}

func (a *Adapter) GetCapabilities(_ context.Context, d device.Device) ([]string, error) {
	return d.Capabilities, nil
}

func (a *Adapter) GetStatus(_ context.Context, d device.Device) (*device.DeviceStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st := a.statuses[d.Code]; st != nil {
		return st, nil
	}
	return &device.DeviceStatus{Online: false}, nil
}

func (a *Adapter) GetTelemetry(_ context.Context, _ device.Device) (*device.Telemetry, error) {
	return nil, fmt.Errorf("generic mqtt devices push telemetry; subscribe instead")
}

func (a *Adapter) SendCommand(_ context.Context, d device.Device, cmd device.Command) error {
	return a.publish("cmd-tenant", d.Code, "commands", map[string]any{
		"name": cmd.Name, "params": cmd.Params, "ts": time.Now().UnixMilli(),
	})
}

func (a *Adapter) CreateMission(_ context.Context, d device.Device, mission device.Mission) error {
	if err := a.publish("", d.Code, "missions", mission); err != nil {
		return err
	}
	a.mu.Lock()
	a.missions[mission.ID] = &device.MissionStatus{State: "created", Progress: 0}
	a.mu.Unlock()
	return nil
}

func (a *Adapter) StartMission(_ context.Context, d device.Device, missionID string) error {
	if err := a.publish("", d.Code, "missions/"+missionID+"/start", map[string]any{"ts": time.Now().UnixMilli()}); err != nil {
		return err
	}
	a.mu.Lock()
	a.missions[missionID] = &device.MissionStatus{State: "executing", Progress: 0}
	a.mu.Unlock()
	return nil
}

func (a *Adapter) PauseMission(_ context.Context, d device.Device, missionID string) error {
	a.mu.Lock()
	if ms := a.missions[missionID]; ms != nil {
		ms.State = "paused"
	}
	a.mu.Unlock()
	return a.publish("", d.Code, "missions/"+missionID+"/pause", map[string]any{})
}

func (a *Adapter) ResumeMission(_ context.Context, d device.Device, missionID string) error {
	a.mu.Lock()
	if ms := a.missions[missionID]; ms != nil {
		ms.State = "executing"
	}
	a.mu.Unlock()
	return a.publish("", d.Code, "missions/"+missionID+"/resume", map[string]any{})
}

func (a *Adapter) StopMission(_ context.Context, d device.Device, missionID string) error {
	a.mu.Lock()
	a.missions[missionID] = &device.MissionStatus{State: "completed", Progress: 100}
	a.mu.Unlock()
	return a.publish("", d.Code, "missions/"+missionID+"/stop", map[string]any{})
}

func (a *Adapter) GetMission(_ context.Context, _ device.Device, missionID string) (*device.MissionStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ms := a.missions[missionID]; ms != nil {
		return ms, nil
	}
	return nil, fmt.Errorf("unknown mission %s", missionID)
}

func (a *Adapter) GetLogs(_ context.Context, _ device.Device) ([]device.DeviceLog, error) {
	return nil, fmt.Errorf("not implemented: logs are collected by the gateway")
}
