package app

import "strings"

// isVacuumDevice distinguishes cleaning robots from relay devices. Their
// MIoT state vector and cleaning commands must never be exposed as a main
// electrical on/off switch.
func isVacuumDevice(d Device) bool {
	category := strings.ToLower(strings.TrimSpace(d.Category))
	identity := strings.ToLower(strings.Join([]string{d.Brand, d.Model, d.Name}, " "))
	if strings.Contains(category, "扫地机器人") || strings.Contains(category, "扫拖机器人") {
		return true
	}
	for _, marker := range []string{"vacuum", "roborock", "rockrobo", "扫地机器人", "扫拖机器人", "扫地机"} {
		if strings.Contains(identity, marker) {
			return true
		}
	}
	return false
}

// sanitizeVacuumDisplay removes legacy switch-shaped values from the public
// device snapshots without changing persisted provider capabilities.
func sanitizeVacuumDisplay(d Device) Device {
	if !isVacuumDevice(d) {
		return d
	}
	d.Power = nil
	filtered := make([]DeviceAttribute, 0, len(d.Attributes))
	for _, attr := range d.Attributes {
		if attr.Key == "on" || attr.Label == "开关状态" || attr.Label == "电源开关" {
			continue
		}
		filtered = append(filtered, attr)
	}
	d.Attributes = filtered
	d.Value = summarizeDeviceAttributes(&d)
	return d
}
