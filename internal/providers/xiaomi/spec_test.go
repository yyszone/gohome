package xiaomi

import "testing"

func TestDiscoverControlsUsesWritablePropertiesAndCallableActions(t *testing.T) {
	spec := Specification{Services: []ServiceDefinition{{
		IID: 2,
		Properties: []PropertyDefinition{
			{IID: 1, Name: "on", Description: "Power", Type: "urn:miot-spec-v2:property:on:00000006", Format: "bool", Access: []string{"read", "write"}},
			{IID: 2, Name: "work_mode", Description: "Work mode", Format: "uint8", Access: []string{"read", "write"}, ValueList: []ValueOption{{Value: 0, Description: "自动"}, {Value: 1, Description: "安静"}}},
			{IID: 3, Name: "token", Description: "Access token", Format: "string", Access: []string{"write"}},
			{IID: 4, Name: "temperature", Format: "float", Access: []string{"read"}},
		},
		Actions: []ActionDefinition{
			{IID: 1, Name: "start_clean", Description: "Start cleaning", In: []int{2}},
			{IID: 2, Name: "factory_reset", Description: "Factory reset"},
		},
	}}}

	controls := DiscoverControls(spec)
	if len(controls) != 3 {
		t.Fatalf("expected power, mode and one safe action, got %+v", controls)
	}
	if controls[0].Name != "电源开关" || controls[0].Kind != "property" || controls[0].SIID != 2 || controls[0].PIID != 1 {
		t.Fatalf("power control not discovered: %+v", controls[0])
	}
	if controls[1].Name != "工作模式" || len(controls[1].Options) != 2 {
		t.Fatalf("enum property not discovered: %+v", controls[1])
	}
	if controls[2].Name != "开始清扫" || controls[2].AIID != 1 || len(controls[2].Inputs) != 1 || controls[2].Inputs[0].Name != "Work mode" {
		t.Fatalf("action/input discovery failed: %+v", controls[2])
	}
}

func TestIsPowerSwitchRejectsReadOnlyOrTelemetry(t *testing.T) {
	cases := []PropertyDefinition{
		{IID: 1, Name: "on", Format: "bool", Access: []string{"read"}},
		{IID: 2, Name: "power", Format: "float", Access: []string{"read", "write"}},
		{IID: 3, Name: "reset", Description: "Factory reset", Format: "bool", Access: []string{"write"}},
	}
	for _, item := range cases {
		if IsPowerSwitch(item) {
			t.Fatalf("should not be treated as an on/off switch: %+v", item)
		}
	}
}

func TestIsPowerSwitchAcceptsPowerPropertyURN(t *testing.T) {
	item := PropertyDefinition{IID: 3, Name: "relay_state", Type: "urn:miot-spec-v2:property:on:00000006", Format: "bool", Access: []string{"read", "write"}}
	if !IsPowerSwitch(item) {
		t.Fatalf("expected property URN to identify switch: %+v", item)
	}
}

func TestDiscoverControlsRejectsDestructiveActions(t *testing.T) {
	spec := Specification{Services: []ServiceDefinition{{
		IID: 3,
		Actions: []ActionDefinition{
			{IID: 1, Name: "start_clean", Description: "Start cleaning"},
			{IID: 2, Name: "delete_map", Description: "Delete map"},
			{IID: 3, Name: "factory_reset", Description: "Reset device"},
		},
	}}}
	controls := DiscoverControls(spec)
	if len(controls) != 1 || controls[0].Name != "开始清扫" {
		t.Fatalf("destructive actions should not be exposed: %+v", controls)
	}
}
