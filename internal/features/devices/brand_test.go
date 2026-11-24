package devices

import "testing"

func TestBrandGroupUsesIntegrationNotManufacturer(t *testing.T) {
	if got := BrandGroup("ewelink-cloud", "coolkit"); got != "易微联 / eWeLink" {
		t.Fatalf("eWeLink manufacturer incorrectly split into brand section: %q", got)
	}
	if got := BrandGroup("xiaomi-cloud", "小米"); got != "小米 / 米家" {
		t.Fatalf("unexpected Xiaomi group: %q", got)
	}
}
