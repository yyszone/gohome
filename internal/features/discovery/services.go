// Package discovery holds the service classification used by LAN discovery.
package discovery

import "strings"

// NormalizeService makes DNS-SD service labels comparable without changing
// the original advertised service name stored for display.
func NormalizeService(service string) string {
	return strings.ToLower(strings.TrimSpace(service))
}
