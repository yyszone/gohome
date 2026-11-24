// Package ewelink contains eWeLink/Sonoff command payload and channel semantics.
// Consumer-account authentication remains in the integration manager.
package ewelink

import (
	"encoding/json"
	"errors"
	"strings"
)

// BuildCommandPayload creates the cloud status-update payload for single- and
// multi-channel relay devices.
func BuildCommandPayload(deviceID, command string, outlet int, multi bool) ([]byte, error) {
	value := ""
	switch command {
	case "turn_on":
		value = "on"
	case "turn_off":
		value = "off"
	default:
		return nil, errors.New("仅支持开关设备的开启/关闭")
	}
	if strings.TrimSpace(deviceID) == "" {
		return nil, errors.New("eWeLink 设备 ID 为空")
	}
	params := map[string]any{"switch": value}
	if multi {
		if outlet < 0 || outlet > 31 {
			return nil, errors.New("eWeLink 通道编号超出有效范围")
		}
		params = map[string]any{"switches": []map[string]any{{"outlet": outlet, "switch": value}}}
	}
	return json.Marshal(map[string]any{"type": 1, "id": deviceID, "params": params})
}
