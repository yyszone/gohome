package ewelink

import (
	"encoding/json"
	"testing"
)

func TestBuildCommandPayloadSingleRelay(t *testing.T) {
	body, err := BuildCommandPayload("switch-1", "turn_on", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ID     string         `json:"id"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID != "switch-1" || payload.Params["switch"] != "on" {
		t.Fatalf("unexpected single relay payload: %s", body)
	}
}

func TestBuildCommandPayloadMultiRelay(t *testing.T) {
	body, err := BuildCommandPayload("switch-4", "turn_off", 3, true)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Params struct {
			Switches []struct {
				Outlet int    `json:"outlet"`
				Switch string `json:"switch"`
			} `json:"switches"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Params.Switches) != 1 || payload.Params.Switches[0].Outlet != 3 || payload.Params.Switches[0].Switch != "off" {
		t.Fatalf("unexpected multi relay payload: %s", body)
	}
}
