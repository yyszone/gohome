package xiaomi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFindWritableSwitchOnlyUsesWritableBooleanPowerProperty(t *testing.T) {
	services := []ServiceSpec{{SIID: 2, Properties: []PropertySpec{
		{PIID: 1, Name: "power", Format: "uint8", Access: []string{"read"}},
		{PIID: 2, Name: "temperature", Format: "float", Access: []string{"read", "write"}},
		{PIID: 3, Name: "on", Format: "bool", Access: []string{"read", "write", "notify"}},
	}}}
	ref, ok := FindWritableSwitch(services)
	if !ok || ref.SIID != 2 || ref.PIID != 3 {
		t.Fatalf("unexpected writable switch: %+v, ok=%v", ref, ok)
	}
}

func TestFindWritableSwitchRejectsReadOnlyAndNumericPower(t *testing.T) {
	services := []ServiceSpec{{SIID: 2, Properties: []PropertySpec{
		{PIID: 1, Name: "on", Format: "bool", Access: []string{"read"}},
		{PIID: 2, Name: "power", Format: "float", Access: []string{"read", "write"}},
	}}}
	if _, ok := FindWritableSwitch(services); ok {
		t.Fatal("read-only state or numeric telemetry must not be exposed as a switch")
	}
}

func TestBuildSetPropertyPayload(t *testing.T) {
	body, err := BuildSetPropertyPayload("123456", SwitchProperty{SIID: 2, PIID: 1}, true)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Params []struct {
			DID   string `json:"did"`
			SIID  int    `json:"siid"`
			PIID  int    `json:"piid"`
			Value bool   `json:"value"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Params) != 1 || got.Params[0].DID != "123456" || got.Params[0].SIID != 2 || got.Params[0].PIID != 1 || !got.Params[0].Value {
		t.Fatalf("unexpected MIoT set payload: %s", body)
	}
}

func TestValidateSetPropertyResponse(t *testing.T) {
	if err := ValidateSetPropertyResponse([]byte(`{"code":0,"result":[{"code":0}]}`)); err != nil {
		t.Fatalf("valid response rejected: %v", err)
	}
	if err := ValidateSetPropertyResponse([]byte(`{"code":0,"result":[{"code":-4004,"message":"no permission"}]}`)); err == nil || !strings.Contains(err.Error(), "no permission") {
		t.Fatalf("per-property error not surfaced: %v", err)
	}
	if err := ValidateSetPropertyResponse([]byte(`{"code":-1,"message":"denied"}`)); err == nil {
		t.Fatal("top-level error not surfaced")
	}
}

func TestBuildActionPayload(t *testing.T) {
	got, err := BuildActionPayload("123", 4, 1, []any{18, "{}"})
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Params struct {
			DID  string `json:"did"`
			SIID int    `json:"siid"`
			AIID int    `json:"aiid"`
			In   []any  `json:"in"`
		} `json:"params"`
	}
	if err := json.Unmarshal(got, &root); err != nil {
		t.Fatal(err)
	}
	if root.Params.DID != "123" || root.Params.SIID != 4 || root.Params.AIID != 1 || len(root.Params.In) != 2 {
		t.Fatalf("unexpected MIoT action payload: %s", got)
	}
}

func TestValidateActionResponse(t *testing.T) {
	if err := ValidateActionResponse([]byte(`{"code":0,"result":{"code":0,"out":[]}}`)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateActionResponse([]byte(`{"code":0,"result":{"code":15,"message":"action failed"}}`)); err == nil {
		t.Fatal("expected MIoT action failure")
	}
	if err := ValidateActionResponse([]byte(`{"code":0,"result":[{"code":15,"message":"action failed"}]}`)); err == nil {
		t.Fatal("expected MIoT action item failure")
	}
}

func TestValidateMIoTResponsesRequireResult(t *testing.T) {
	if err := ValidateSetPropertyResponse([]byte(`{"code":0}`)); err == nil {
		t.Fatal("set-property response without result must not be reported as success")
	}
	if err := ValidateActionResponse([]byte(`{"code":0,"result":null}`)); err == nil {
		t.Fatal("action response with null result must not be reported as success")
	}
	if err := ValidateActionResponse([]byte(`{"message":"success"}`)); err == nil {
		t.Fatal("action response without code must not be reported as success")
	}
}
