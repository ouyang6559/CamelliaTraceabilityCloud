package mqttclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// MQTTHandler processes a message received on an MQTT topic.
type MQTTHandler func(topic string, payload []byte)

// MQTTClient wraps paho with publish and lifetime helpers.
type MQTTClient struct {
	client mqtt.Client
}

// Connect subscribes to topics (empty slice = publish-only client, e.g. simulator).
func Connect(ctx context.Context, brokerURL, clientID string, topics []string, handler MQTTHandler, log *slog.Logger) (*MQTTClient, error) {
	opts := mqtt.NewClientOptions().
		AddBroker(brokerURL).
		SetClientID(clientID).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(15 * time.Second).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetCleanSession(true)
	if handler != nil {
		opts.SetDefaultPublishHandler(func(_ mqtt.Client, msg mqtt.Message) {
			handler(msg.Topic(), msg.Payload())
		})
	}
	opts.OnConnect = func(c mqtt.Client) {
		if len(topics) > 0 {
			for _, t := range topics {
				// The handler is bound per-subscription: newer paho versions
				// drop messages routed to a nil callback instead of falling
				// back to the default publish handler.
				callback := func(_ mqtt.Client, msg mqtt.Message) {
					handler(msg.Topic(), msg.Payload())
				}
				if tok := c.Subscribe(t, 1, callback); tok.Wait() && tok.Error() != nil {
					log.Error("mqtt subscribe failed", "topic", t, "err", tok.Error())
				} else {
					log.Info("mqtt subscribed", "topic", t)
				}
			}
		}
	}
	client := mqtt.NewClient(opts)
	tok := client.Connect()
	if !tok.WaitTimeout(15 * time.Second) {
		// The broker may come up later; paho keeps retrying in the background.
		log.Warn("mqtt connect not confirmed within timeout, continuing in background")
	} else if tok.Error() != nil {
		return nil, fmt.Errorf("connect mqtt: %w", tok.Error())
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return &MQTTClient{client: client}, nil
}

func (c *MQTTClient) Publish(topic string, qos byte, retained bool, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	tok := c.client.Publish(topic, qos, retained, data)
	return tok.Error()
}

func (c *MQTTClient) PublishRaw(topic string, qos byte, retained bool, payload []byte) error {
	return c.client.Publish(topic, qos, retained, payload).Error()
}

func (c *MQTTClient) Close() { c.client.Disconnect(500) }
