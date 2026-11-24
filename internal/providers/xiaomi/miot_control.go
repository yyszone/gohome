// Package xiaomi contains Xiaomi MIoT-specific device capability and control logic.
// Account login and token exchange intentionally remain in the existing integration
// manager; this package only handles MIoT property discovery and property commands.
package xiaomi

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// PropertySpec is the minimal part of a MIoT property specification needed for
// deciding whether a device exposes a writable power switch.
type PropertySpec struct {
	SIID        int
	PIID        int
	Name        string
	Description string
	Type        string
	Format      string
	Access      []string
}

// ServiceSpec groups properties under their MIoT service instance ID.
type ServiceSpec struct {
	SIID       int
	Properties []PropertySpec
}

// SwitchProperty identifies the writable boolean power property.
type SwitchProperty struct {
	SIID int `json:"siid"`
	PIID int `json:"piid"`
}

// FindWritableSwitch finds a real on/off property. A generic numeric power
// measurement is deliberately not treated as a switch.
func FindWritableSwitch(services []ServiceSpec) (SwitchProperty, bool) {
	var fallback SwitchProperty
	foundFallback := false
	for _, service := range services {
		for _, property := range service.Properties {
			if !hasAccess(property.Access, "write") || property.PIID <= 0 {
				continue
			}
			definition := PropertyDefinition{
				IID: property.PIID, Name: property.Name, Description: property.Description,
				Type: property.Type, Format: property.Format, Access: property.Access,
			}
			if IsPowerSwitch(definition) {
				ref := SwitchProperty{SIID: service.SIID, PIID: property.PIID}
				if normalize(property.Name) == "on" {
					return ref, true
				}
				if !foundFallback {
					fallback, foundFallback = ref, true
				}
			}
		}
	}
	return fallback, foundFallback
}

// BuildSetPropertyPayload creates the standard MIoT prop/set request body.
func BuildSetPropertyPayload(did string, property SwitchProperty, enabled bool) ([]byte, error) {
	if strings.TrimSpace(did) == "" {
		return nil, errors.New("小米设备 did 为空")
	}
	if property.SIID <= 0 || property.PIID <= 0 {
		return nil, errors.New("小米设备没有有效的可写开关属性")
	}
	return json.Marshal(map[string]any{
		"params": []map[string]any{{
			"did": did, "siid": property.SIID, "piid": property.PIID, "value": enabled,
		}},
	})
}

// ValidateSetPropertyResponse rejects both top-level and per-property MIoT errors.
func ValidateSetPropertyResponse(body []byte) error {
	var envelope struct {
		Code    *int            `json:"code"`
		Message string          `json:"message"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("解析小米 MIoT 控制响应失败：%w", err)
	}
	if envelope.Code == nil {
		return errors.New("小米 MIoT 控制响应缺少 code 字段")
	}
	if *envelope.Code != 0 {
		return fmt.Errorf("小米 MIoT 控制失败（%d）：%s", *envelope.Code, envelope.Message)
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return errors.New("小米 MIoT 控制响应缺少 result，不能确认设备已执行命令")
	}

	var items []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(envelope.Result, &items); err == nil {
		for _, item := range items {
			if item.Code != 0 {
				return fmt.Errorf("小米 MIoT 属性写入失败（%d）：%s", item.Code, item.Message)
			}
		}
		return nil
	}
	var result struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(envelope.Result, &result); err == nil && result.Code != nil && *result.Code != 0 {
		return fmt.Errorf("小米 MIoT 属性写入失败（%d）：%s", *result.Code, result.Message)
	}
	return nil
}

func hasAccess(access []string, want string) bool {
	for _, value := range access {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

func normalize(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(value)
}

// BuildActionPayload creates the MIoT cloud action request. Action parameters
// are ordered exactly as the action's `in` list in its specification.
func BuildActionPayload(did string, siid, aiid int, params []any) ([]byte, error) {
	if strings.TrimSpace(did) == "" {
		return nil, errors.New("小米设备 did 为空")
	}
	if siid <= 0 || aiid <= 0 {
		return nil, errors.New("小米 MIoT 动作 siid/aiid 无效")
	}
	if params == nil {
		params = []any{}
	}
	return json.Marshal(map[string]any{
		"params": map[string]any{
			"did": did, "siid": siid, "aiid": aiid, "in": params,
		},
	})
}

// ValidateActionResponse checks both common Xiaomi cloud response envelopes:
// {code,result:{code,...}} and {code,result:[{code,...}]}.
func ValidateActionResponse(body []byte) error {
	var envelope struct {
		Code    *int            `json:"code"`
		Message string          `json:"message"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("解析小米 MIoT 动作响应失败：%w", err)
	}
	if envelope.Code == nil {
		return errors.New("小米 MIoT 动作响应缺少 code 字段")
	}
	if *envelope.Code != 0 {
		return fmt.Errorf("小米 MIoT 动作失败（%d）：%s", *envelope.Code, envelope.Message)
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return errors.New("小米 MIoT 动作响应缺少 result，不能确认动作已执行")
	}

	checkResult := func(raw json.RawMessage) error {
		var result struct {
			Code    *int   `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil // MIoT action output may be a scalar value.
		}
		if result.Code != nil && *result.Code != 0 {
			return fmt.Errorf("小米 MIoT 动作执行失败（%d）：%s", *result.Code, result.Message)
		}
		return nil
	}

	var items []json.RawMessage
	if err := json.Unmarshal(envelope.Result, &items); err == nil {
		for _, item := range items {
			if err := checkResult(item); err != nil {
				return err
			}
		}
		return nil
	}
	return checkResult(envelope.Result)
}
