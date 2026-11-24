// Package esp32 reserves the provider boundary for local ESP32 integrations.
// It deliberately does not report successful commands until an actual transport
// (MQTT, HTTP, or the user's firmware protocol) has been configured.
package esp32

import (
	"context"
	"errors"
)

var ErrTransportNotConfigured = errors.New("ESP32 通信驱动尚未配置；请先配置 MQTT 或设备 HTTP 协议")

type Command struct {
	DeviceID string
	Name     string
	Payload  []byte
}

type Transport interface {
	Publish(context.Context, string, []byte) error
}

type Driver struct {
	Transport Transport
}

func (d Driver) Execute(ctx context.Context, topic string, payload []byte) error {
	if d.Transport == nil {
		return ErrTransportNotConfigured
	}
	if topic == "" {
		return errors.New("ESP32 MQTT topic 不能为空")
	}
	return d.Transport.Publish(ctx, topic, payload)
}
