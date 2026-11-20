package genericmqtt

import (
	"testing"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/adapters/device"
)

// TestTopic pins the MQTT topic contract shared with the API ingest
// (ctc/v1/tenants/{tenant}/devices/{code}/{suffix}).
func TestTopic(t *testing.T) {
	got := Topic("01HX", "DEV-UAV-001", "telemetry")
	want := "ctc/v1/tenants/01HX/devices/DEV-UAV-001/telemetry"
	if got != want {
		t.Errorf("Topic = %q, want %q", got, want)
	}
}

// TestTenantOf ensures the tenant comes from device metadata (no more
// hardcoded "" / "cmd-tenant" — docs/adr/adr-005).
func TestTenantOf(t *testing.T) {
	withMeta := device.Device{Metadata: map[string]string{"tenant": "01HX"}}
	if got := tenantOf(withMeta); got != "01HX" {
		t.Errorf("tenantOf(with meta) = %q, want 01HX", got)
	}
	without := device.Device{}
	if got := tenantOf(without); got != "default" {
		t.Errorf("tenantOf(no meta) = %q, want default", got)
	}
}

func TestManifest(t *testing.T) {
	m := Manifest()
	if m.Name != "generic-mqtt" || m.Version == "" {
		t.Errorf("manifest incomplete: %+v", m)
	}
	found := false
	for _, p := range m.Protocols {
		if p == "mqtt" {
			found = true
		}
	}
	if !found {
		t.Errorf("manifest must declare mqtt protocol: %+v", m.Protocols)
	}
}
