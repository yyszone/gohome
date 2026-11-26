package app

import (
	"encoding/json"
	"testing"
)

func TestMIoTEnvironmentPropertiesUseUnitAndServiceSemantics(t *testing.T) {
	var spec miotSpecDocument
	fixture := []byte(`{"services":[{"iid":2,"name":"environment","description":"环境传感器","type":"urn:miot-spec-v2:service:environment:0000A001:vendor:1","properties":[{"iid":1,"name":"currentTemperature","description":"当前读数","format":"float","unit":"celsius","access":["read"]},{"iid":2,"name":"value","description":"当前读数","format":"float","unit":"percentage","access":["read"]}]}]}`)
	if err := json.Unmarshal(fixture, &spec); err != nil {
		t.Fatal(err)
	}
	refs := miotReadDeviceRefs(spec)
	found := map[string]miotDevicePropertyRef{}
	for _, ref := range refs {
		found[ref.Field] = ref
	}
	if ref, ok := found["temperature"]; !ok || ref.Unit != "°C" {
		t.Fatalf("temperature ref missing/wrong: %+v", ref)
	}
	if ref, ok := found["humidity"]; !ok || ref.Unit != "%" {
		t.Fatalf("humidity ref missing/wrong: %+v", ref)
	}
}

func TestVacuumMIoTStatusArrayNeverBecomesPowerSwitch(t *testing.T) {
	device := Device{ID: "robot-1", Name: "扫地机器人", Brand: "Roborock", Model: "roborock.vacuum.s5", Category: "扫地机器人", Power: func() *bool { v := true; return &v }(), Attributes: []DeviceAttribute{{Key: "on", Label: "开关状态", Value: "[1,1,1]"}}, Value: "开关状态 [1,1,1]"}
	if !isVacuumDevice(device) {
		t.Fatal("vacuum device not recognized")
	}
	if _, ok := findXiaomiPowerControl(device); ok {
		t.Fatal("vacuum must not expose a main power switch")
	}
	refs := []miotDevicePropertyRef{{SIID: 1, PIID: 1, Field: "on", Label: "开关状态", Format: "bool"}}
	body := []byte(`{"code":0,"result":[{"siid":1,"piid":1,"code":0,"value":[1,1,1,1,1,1,1,0,1,0,1,1,1]}]}`)
	applied := applyMIoTDeviceResultForDID(&device, refs, body, false)
	if applied != 0 {
		t.Fatalf("array power result should be ignored, applied=%d", applied)
	}
	if device.Power != nil {
		t.Fatalf("vacuum power should remain nil: %v", *device.Power)
	}
	if len(device.Attributes) != 0 {
		t.Fatalf("switch-shaped attributes should not remain: %+v", device.Attributes)
	}
	if device.Value != "" {
		t.Fatalf("misleading state summary should be cleared: %q", device.Value)
	}
}

func TestVacuumLegacySnapshotIsSanitized(t *testing.T) {
	a := testApp(t)
	power := true
	device := Device{ID: "robot-snapshot", Name: "Mi Robot Vacuum", Model: "roborock.vacuum.s5", Power: &power, Attributes: []DeviceAttribute{{Key: "on", Label: "开关状态", Value: "1"}, {Key: "battery", Label: "电量", Value: "80", Unit: "%"}}, Value: "开关状态 1 · 电量 80%"}
	if err := a.store.putDevice(device); err != nil {
		t.Fatal(err)
	}
	got := a.store.snapshot().Devices[0]
	if got.Power != nil {
		t.Fatal("legacy vacuum power leaked from snapshot")
	}
	if len(got.Attributes) != 1 || got.Attributes[0].Key != "battery" {
		t.Fatalf("vacuum telemetry filtering failed: %+v", got.Attributes)
	}
	if got.Value != "电量 80%" {
		t.Fatalf("vacuum summary not recomputed: %q", got.Value)
	}
}

func TestVacuumBooleanModeControlsRemainAvailable(t *testing.T) {
	device := Device{
		ID: "robot-controls", Name: "Roborock S5", Brand: "Roborock",
		Model: "roborock.vacuum.s5", Category: "扫地机器人",
		Controls: []DeviceControl{{
			ID: "carpet-boost", Name: "地毯增压", Kind: "property",
			SIID: 4, PIID: 2, Format: "bool",
		}},
	}
	refs := []miotDevicePropertyRef{{
		SIID: 4, PIID: 2, Field: "control", Format: "bool", ControlOnly: true,
	}}
	body := []byte(`{"code":0,"result":[{"siid":4,"piid":2,"code":0,"value":true}]}`)
	if got := applyMIoTDeviceResultForDID(&device, refs, body, false); got != 1 {
		t.Fatalf("boolean vacuum mode property should be read, applied=%d", got)
	}
	if got, ok := device.Controls[0].Value.(bool); !ok || !got {
		t.Fatalf("vacuum mode control state was not retained: %#v", device.Controls[0].Value)
	}
	if device.Power != nil {
		t.Fatal("a vacuum mode property must not set the main power state")
	}
}
