// Package integration defines the extension contract for brand adapters.
// Concrete Xiaomi/eWeLink adapters are intentionally not stubbed as successful
// providers; they must implement real authentication and device I/O first.
package integration

import (
	"context"
	"encoding/json"
)

type Capability struct {
	Name      string          `json:"name"`
	ReadOnly  bool            `json:"readOnly"`
	ValueType string          `json:"valueType,omitempty"`
	Unit      string          `json:"unit,omitempty"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

type Device struct {
	ID           string          `json:"id"`
	ProviderID   string          `json:"providerId"`
	Brand        string          `json:"brand"`
	Model        string          `json:"model"`
	Name         string          `json:"name"`
	Room         string          `json:"room,omitempty"`
	Online       bool            `json:"online"`
	Capabilities []Capability    `json:"capabilities"`
	Raw          json.RawMessage `json:"raw,omitempty"`
}

type Command struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type Event struct {
	DeviceID string          `json:"deviceId"`
	Kind     string          `json:"kind"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

// Provider is the contract each brand integration must implement.
// Connect must never store plain-text passwords; use provider-approved auth.
type Provider interface {
	ID() string
	Name() string
	Connect(ctx context.Context, config json.RawMessage) error
	Discover(ctx context.Context) ([]Device, error)
	Execute(ctx context.Context, deviceID string, command Command) error
	Subscribe(ctx context.Context) (<-chan Event, error)
	Close() error
}
