// Package device is the Device Adapter SDK (specs/20, specs/03.2): the
// interface third-party vendors implement to integrate a device family without
// touching core business code. Core never depends on vendor implementations.
package device

import (
	"context"
)

// Capability values a device may declare (specs/03.3).
const (
	CapTelemetry     = "TELEMETRY"
	CapGPS           = "GPS"
	CapRTK           = "RTK"
	CapVideo         = "VIDEO"
	CapPhoto         = "PHOTO"
	CapMission       = "MISSION"
	CapAutonomous    = "AUTONOMOUS"
	CapRemoteControl = "REMOTE_CONTROL"
	CapSpray         = "SPRAY"
	CapSpread        = "SPREAD"
	CapMow           = "MOW"
	CapPlow          = "PLOW"
	CapHarrow        = "HARROW"
	CapSeed          = "SEED"
	CapFertilize     = "FERTILIZE"
	CapInspection    = "INSPECTION"
	CapMapping       = "MAPPING"
	CapReturnHome    = "RETURN_HOME"
	CapCharge        = "CHARGE"
	CapDock          = "DOCK"
)

// Device types (specs/03.4).
const (
	TypeUAV            = "UAV"
	TypeUGV            = "UGV"
	TypeTractor        = "TRACTOR"
	TypeMower          = "MOWER"
	TypeWeeder         = "WEEDER"
	TypeSprayer        = "SPRAYER"
	TypeTransportRobot = "TRANSPORT_ROBOT"
	TypeHarvestRobot   = "HARVEST_ROBOT"
	TypeWeatherStation = "WEATHER_STATION"
	TypeSoilSensor     = "SOIL_SENSOR"
	TypeCamera         = "CAMERA"
	TypeGNSS           = "GNSS"
	TypeRTK            = "RTK"
	TypeGateway        = "GATEWAY"
)

// Device is the neutral device descriptor passed to adapters.
type Device struct {
	ID           string
	Code         string
	Name         string
	DeviceType   string
	Capabilities []string
	Endpoint     string // e.g. MQTT broker URL or vendor API base
	Token        string // credential injected from the secret store
	Metadata     map[string]string
}

// DeviceInfo is static identity data reported by the adapter.
type DeviceInfo struct {
	Vendor   string
	Model    string
	Firmware string
	Serial   string
}

// DeviceStatus is dynamic state.
type DeviceStatus struct {
	Online    bool
	BatteryPc int
	Firmware  string
	LastError string
}

// Telemetry is one observation batch.
type Telemetry struct {
	TS       int64                  `json:"ts"` // unix millis
	Metrics  map[string]float64     `json:"metrics"`
	Position *GeoPoint              `json:"position,omitempty"`
	Payload  map[string]interface{} `json:"payload,omitempty"`
}

// GeoPoint is a WGS84 coordinate.
type GeoPoint struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
	Alt float64 `json:"alt,omitempty"`
}

// Mission is the neutral mission description (specs/03.2).
type Mission struct {
	ID        string
	Type      string // spray / mow / map / inspect / transport ...
	Waypoints []Waypoint
	Params    map[string]string
}

// Waypoint is one route point with an optional action.
type Waypoint struct {
	Lat    float64
	Lng    float64
	Alt    float64
	Speed  float64
	Action string // takeoff | wayline | spray_on | spray_off | photo | land
}

// MissionStatus mirrors execution state reported by the adapter.
type MissionStatus struct {
	State    string // executing | paused | completed | failed
	Progress float64
	Detail   string
}

// Command is a generic remote command; safety-critical commands MUST go
// through the mission pipeline, never this path (specs/03.10).
type Command struct {
	Name   string
	Params map[string]string
}

// DeviceLog is one adapter log line.
type DeviceLog struct {
	TS      int64
	Level   string
	Message string
}

// Adapter is the vendor integration contract (specs/03.2). The interface is
// the SDK v0 contract; it will be re-stated as Proto in Phase 2 (ADR-001).
type Adapter interface {
	Connect(ctx context.Context, device Device) error
	Disconnect(ctx context.Context, device Device) error
	GetDeviceInfo(ctx context.Context, device Device) (*DeviceInfo, error)
	GetCapabilities(ctx context.Context, device Device) ([]string, error)
	GetStatus(ctx context.Context, device Device) (*DeviceStatus, error)
	GetTelemetry(ctx context.Context, device Device) (*Telemetry, error)
	SendCommand(ctx context.Context, device Device, command Command) error
	CreateMission(ctx context.Context, device Device, mission Mission) error
	StartMission(ctx context.Context, device Device, missionID string) error
	PauseMission(ctx context.Context, device Device, missionID string) error
	ResumeMission(ctx context.Context, device Device, missionID string) error
	StopMission(ctx context.Context, device Device, missionID string) error
	GetMission(ctx context.Context, device Device, missionID string) (*MissionStatus, error)
	GetLogs(ctx context.Context, device Device) ([]DeviceLog, error)
}

// Manifest describes an adapter for registration/discovery (specs/20).
type Manifest struct {
	Name         string
	Version      string
	Type         string // "device"
	Vendor       string
	Protocols    []string // mqtt | http | mavlink | ros2 | vendor
	Capabilities []string
}
