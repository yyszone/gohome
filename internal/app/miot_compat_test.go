package app

import (
	"encoding/json"
	"testing"
)

func decodeMIoTSpecFixture(t *testing.T, raw string) miotSpecDocument {
	t.Helper()
	var spec miotSpecDocument
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		t.Fatalf("decode MIoT fixture: %v", err)
	}
	return spec
}

func TestMIoTModelIndexChoosesHighestVersion(t *testing.T) {
	instances := []miotSpecInstance{
		{Model: "xiaomi.router.rb03", Type: "urn:miot-spec-v2:device:router:0000A036:xiaomi-rb03:1"},
		{Model: "xiaomi.wifispeaker.x08c", Type: "urn:miot-spec-v2:device:speaker:0000A015:xiaomi-x08c:1"},
		{Model: "xiaomi.router.rb03", Type: "urn:miot-spec-v2:device:router:0000A036:xiaomi-rb03:2"},
		{Model: "xiaomi.wifispeaker.x08c", Type: "urn:miot-spec-v2:device:speaker:0000A015:xiaomi-x08c:2"},
	}
	versions := buildMIoTModelTypeVersions(instances)
	if len(versions["xiaomi.router.rb03"]) != 2 || miotSpecTypeVersion(versions["xiaomi.router.rb03"][1]) != 1 {
		t.Fatalf("router RB03 versions should be retained newest-first: %+v", versions["xiaomi.router.rb03"])
	}
	models := buildMIoTModelTypeIndex(instances)
	if got := miotSpecTypeVersion(models["xiaomi.router.rb03"]); got != 2 {
		t.Fatalf("router rb03 chose spec version %d, want latest version 2", got)
	}
	if got := miotSpecTypeVersion(models["xiaomi.wifispeaker.x08c"]); got != 2 {
		t.Fatalf("speaker x08c chose spec version %d, want latest version 2", got)
	}
}

func TestMIoTPropertyResultCoordinatesSupportsIIDAndStringIDs(t *testing.T) {
	var result miotPropertyResult
	if err := json.Unmarshal([]byte(`{"iid":"0.2.9","value":true,"code":0}`), &result); err != nil {
		t.Fatal(err)
	}
	if siid, piid := result.coordinates(); siid != 2 || piid != 9 {
		t.Fatalf("iid coordinates = %d/%d, want 2/9", siid, piid)
	}

	if err := json.Unmarshal([]byte(`{"siid":"3","piid":"4","iid":"0.8.9"}`), &result); err != nil {
		t.Fatal(err)
	}
	if siid, piid := result.coordinates(); siid != 3 || piid != 4 {
		t.Fatalf("string coordinates = %d/%d, want explicit 3/4", siid, piid)
	}
}

func TestMIoTRouterRefsRemainNetworkTelemetry(t *testing.T) {
	spec := decodeMIoTSpecFixture(t, `{
	  "services": [
	    {"iid":2,"name":"router","type":"urn:miot-spec-v2:service:router:0000A036:vendor:1","properties":[
	      {"iid":1,"name":"download-speed","format":"float","access":["read"]},
	      {"iid":2,"name":"upload-speed","format":"float","access":["read"]},
	      {"iid":3,"name":"connected-device-number","format":"uint16","access":["read"]},
	      {"iid":6,"name":"status","format":"uint8","access":["read"]}
	    ]},
	    {"iid":3,"name":"wifi","type":"urn:miot-spec-v2:service:wifi:1","properties":[
	      {"iid":3,"name":"on","format":"bool","access":["read","write"]}
	    ]},
	    {"iid":4,"name":"guest-wifi","type":"urn:miot-spec-v2:service:guest-wifi:1","properties":[
	      {"iid":1,"name":"on","format":"bool","access":["read","write"]}
	    ]}
	  ]
	}`)
	refs := miotReadDeviceRefsForModel(spec, "xiaomi.router.rb03")
	live := miotLiveStateRefs(refs)
	fields := map[string]bool{}
	for _, ref := range live {
		fields[ref.Field] = true
	}
	for _, field := range []string{"router_download_speed", "router_upload_speed", "router_connected_devices", "router_status", "router_wifi", "router_guest_wifi"} {
		if !fields[field] {
			t.Errorf("router live refs missing %q: %+v", field, live)
		}
	}
	if fields["on"] {
		t.Fatal("router Wi-Fi switch leaked into the appliance power field")
	}

	device := Device{RemoteID: "router-did", Brand: "小米", Model: "xiaomi.router.rb03", Name: "路由器"}
	body := []byte(`{"code":0,"result":[{"did":"router-did","siid":99,"piid":99,"iid":"0.2.1","value":128.5,"code":0}]}`)
	if applied := applyMIoTDeviceResultForDID(&device, live, body, false); applied != 1 {
		t.Fatalf("router response with stale explicit IDs was not applied: %d", applied)
	}
	if device.Power != nil {
		t.Fatalf("router telemetry must not be treated as mains power: %v", *device.Power)
	}
	foundDownload := false
	for _, attr := range device.Attributes {
		if attr.Key == "router_download_speed" {
			foundDownload = true
			if attr.Value != "128.5" {
				t.Errorf("router download speed = %q, want 128.5", attr.Value)
			}
		}
	}
	if !foundDownload {
		t.Fatalf("router download telemetry was not populated: %+v", device.Attributes)
	}
}

func TestMIoTVacuumUsesOperatingStatusNotPowerSwitch(t *testing.T) {
	spec := decodeMIoTSpecFixture(t, `{
	  "services": [
	    {"iid":2,"name":"vacuum","description":"Robot Cleaner","type":"urn:miot-spec-v2:service:vacuum:0000A006:vendor:1","properties":[
	      {"iid":1,"name":"status","description":"Status","format":"uint8","access":["read","notify"],"value-list":[
	        {"value":0,"description":"休眠"},{"value":1,"description":"待机"},{"value":5,"description":"清扫中"}
	      ]},
	      {"iid":9,"name":"on","description":"Switch Status","format":"string","access":["read","write","notify"]}
	    ]},
	    {"iid":3,"name":"battery","properties":[
	      {"iid":1,"name":"battery-level","format":"uint8","unit":"percentage","access":["read","notify"]}
	    ]}
	  ]
	}`)
	refs := miotReadDeviceRefsForModel(spec, "xiaomi.vacuum.c103")
	live := miotLiveStateRefs(refs)
	fields := map[string]bool{}
	for _, ref := range live {
		fields[ref.Field] = true
	}
	if !fields["vacuum_status"] || !fields["battery"] {
		t.Fatalf("vacuum status/battery refs missing: %+v", live)
	}
	if fields["on"] {
		t.Fatalf("vacuum switch.on must not become the main power state: %+v", live)
	}

	device := Device{RemoteID: "vacuum-did", Brand: "小米", Model: "xiaomi.vacuum.c103", Name: "米家扫拖机器人3C 增强版", Category: "扫地机器人"}
	body := []byte(`{"code":0,"result":[{"did":"vacuum-did","iid":"0.2.1","value":5,"code":0},{"did":"vacuum-did","iid":"0.3.1","value":82,"code":0}]}`)
	if applied := applyMIoTDeviceResultForDID(&device, live, body, false); applied != 2 {
		t.Fatalf("applied %d vacuum properties, want 2", applied)
	}
	if device.Power != nil {
		t.Fatalf("vacuum must not expose an electrical main power state: %v", *device.Power)
	}
	foundStatus := false
	for _, attr := range device.Attributes {
		if attr.Key == "vacuum_status" {
			foundStatus = true
			if attr.Value != "清扫中" {
				t.Errorf("vacuum status = %q, want 清扫中", attr.Value)
			}
		}
		if attr.Key == "on" || attr.Label == "开关状态" {
			t.Errorf("unexpected vacuum power-shaped attribute: %+v", attr)
		}
	}
	if !foundStatus {
		t.Fatalf("vacuum status attribute not populated: %+v", device.Attributes)
	}
}

func TestMIoTPropertyResultMatchesIIDWhenExplicitIDsAreStale(t *testing.T) {
	var result miotPropertyResult
	if err := json.Unmarshal([]byte(`{"did":"router-did","siid":99,"piid":99,"iid":"0.2.3","value":12,"code":0}`), &result); err != nil {
		t.Fatal(err)
	}
	refs := map[string]string{"2:3": "router-download-speed"}
	ref, siid, piid, ok := matchMIoTPropertyResult(result, refs)
	if !ok || ref != "router-download-speed" || siid != 2 || piid != 3 {
		t.Fatalf("stale explicit IDs should fall back to iid: ref=%q siid=%d piid=%d ok=%t", ref, siid, piid, ok)
	}
}

func TestMIoTSpeakerX08CV2ReadableStateFields(t *testing.T) {
	spec := decodeMIoTSpecFixture(t, `{
	  "services": [
	    {"iid":2,"name":"speaker","description":"Speaker","type":"urn:miot-spec-v2:service:speaker:00007842:vendor:2","properties":[
	      {"iid":1,"name":"volume","format":"uint8","unit":"percentage","access":["read","write","notify"]},
	      {"iid":2,"name":"mute","format":"bool","access":["read","write","notify"]},
	      {"iid":3,"name":"playing-state","format":"uint8","access":["notify"]},
	      {"iid":4,"name":"sleep-mode","format":"bool","access":["read","write"]}
	    ]}
	  ]
	}`)
	refs := miotLiveStateRefs(miotReadDeviceRefsForModel(spec, "xiaomi.wifispeaker.x08c"))
	fields := map[string]bool{}
	for _, ref := range refs {
		fields[ref.Field] = true
	}
	for _, field := range []string{"volume", "muted", "sleep_mode"} {
		if !fields[field] {
			t.Errorf("speaker live refs missing %q: %+v", field, refs)
		}
	}
	if fields["playback"] {
		t.Fatalf("notify-only playing-state should not be polled as a readable property: %+v", refs)
	}
}

func TestMIoTNoReadCapabilityIsNotADeviceFailure(t *testing.T) {
	// This test documents the expected behavior: a notify-only/write-only
	// spec produces no prop/get plan, so it must not become a poll warning.
	spec := decodeMIoTSpecFixture(t, `{"services":[{"iid":2,"name":"router","properties":[{"iid":1,"name":"download-speed","format":"float","access":["notify"]}]}]}`)
	refs := miotLiveStateRefs(miotReadDeviceRefsForModel(spec, "xiaomi.router.lv1"))
	if len(refs) != 0 {
		t.Fatalf("notify-only property unexpectedly became readable: %+v", refs)
	}
}
