// Package devices contains device presentation and capability normalization.
package devices

import "strings"

// BrandGroup returns the logical integration brand. Manufacturer names such as
// "coolkit" remain on the device itself, but do not split one eWeLink account
// into multiple visual brand sections.
func BrandGroup(protocol, manufacturer string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "xiaomi-cloud":
		return "小米 / 米家"
	case "ewelink-cloud":
		return "易微联 / eWeLink"
	case "esp32-mqtt", "esp32-http":
		return "ESP32"
	}
	if strings.TrimSpace(manufacturer) != "" {
		return strings.TrimSpace(manufacturer)
	}
	return "其他设备"
}
