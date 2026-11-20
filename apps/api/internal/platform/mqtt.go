// Package platform re-exports the shared MQTT client for internal use.
package platform

import (
	"context"
	"log/slog"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/packages/mqttclient"
)

// MQTTHandler processes a message received on an MQTT topic.
type MQTTHandler = mqttclient.MQTTHandler

// MQTTClient wraps paho with publish and lifetime helpers.
type MQTTClient = mqttclient.MQTTClient

// ConnectMQTT connects and optionally subscribes (re-exported from the shared package).
func ConnectMQTT(ctx context.Context, brokerURL, clientID string, topics []string, handler MQTTHandler, log *slog.Logger) (*MQTTClient, error) {
	return mqttclient.Connect(ctx, brokerURL, clientID, topics, handler, log)
}
