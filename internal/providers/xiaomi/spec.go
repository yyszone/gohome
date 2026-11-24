// Package xiaomi contains Xiaomi MIoT specification parsing helpers and
// capability discovery. Cloud account authentication is owned by app so the
// login/session flow can evolve independently from device controls.
package xiaomi

import (
	"fmt"
	"strings"
)

type ValueOption struct {
	Value       any    `json:"value"`
	Description string `json:"description"`
}

type PropertyDefinition struct {
	IID         int
	Name        string
	Description string
	Type        string
	Format      string
	Access      []string
	ValueList   []ValueOption
}

type ActionDefinition struct {
	IID         int
	Name        string
	Description string
	Type        string
	In          []int
	Out         []int
}

type ServiceDefinition struct {
	IID        int
	Name       string
	Properties []PropertyDefinition
	Actions    []ActionDefinition
}

type Specification struct {
	Services []ServiceDefinition
}

type ControlOption struct {
	Value any    `json:"value"`
	Label string `json:"label"`
}

type ControlInput struct {
	Name    string          `json:"name"`
	Format  string          `json:"format,omitempty"`
	Options []ControlOption `json:"options,omitempty"`
}

type Control struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	SIID        int             `json:"siid"`
	PIID        int             `json:"piid,omitempty"`
	AIID        int             `json:"aiid,omitempty"`
	Format      string          `json:"format,omitempty"`
	Options     []ControlOption `json:"options,omitempty"`
	Inputs      []ControlInput  `json:"inputs,omitempty"`
}

// DiscoverControls builds an allow-list of writable properties and callable
// actions strictly from the model's MIoT specification. It deliberately does
// not invent capabilities based on a product's display name or category.
func DiscoverControls(spec Specification) []Control {
	controls := make([]Control, 0, 24)
	for _, service := range spec.Services {
		properties := make(map[int]PropertyDefinition, len(service.Properties))
		for _, prop := range service.Properties {
			properties[prop.IID] = prop
			if prop.IID <= 0 || !hasSpecAccess(prop.Access, "write") || isSensitiveProperty(prop) {
				continue
			}
			if len(controls) >= 40 {
				break
			}
			name := ControlLabel(prop.Name, prop.Description)
			if IsPowerSwitch(prop) {
				name = "电源开关"
			}
			controls = append(controls, Control{
				ID:   fmt.Sprintf("prop:%d:%d", service.IID, prop.IID),
				Kind: "property", Name: name, Description: prop.Description,
				SIID: service.IID, PIID: prop.IID, Format: prop.Format,
				Options: buildOptions(prop.ValueList),
			})
		}

		for _, action := range service.Actions {
			if action.IID <= 0 || len(controls) >= 60 || isDangerousAction(action) {
				continue
			}
			inputs := make([]ControlInput, 0, len(action.In))
			for index, piid := range action.In {
				prop, ok := properties[piid]
				if !ok {
					inputs = append(inputs, ControlInput{Name: fmt.Sprintf("参数 %d", index+1)})
					continue
				}
				inputs = append(inputs, ControlInput{
					Name:   firstNonEmpty(prop.Description, prop.Name, fmt.Sprintf("参数 %d", index+1)),
					Format: prop.Format, Options: buildOptions(prop.ValueList),
				})
			}
			controls = append(controls, Control{
				ID:   fmt.Sprintf("action:%d:%d", service.IID, action.IID),
				Kind: "action", Name: ControlLabel(action.Name, action.Description),
				Description: action.Description, SIID: service.IID, AIID: action.IID,
				Inputs: inputs,
			})
		}
	}
	return controls
}

// IsPowerSwitch identifies only a writable boolean on/off property. A numeric
// power measurement is never interpreted as a control target.
func IsPowerSwitch(prop PropertyDefinition) bool {
	if prop.IID <= 0 || !hasSpecAccess(prop.Access, "write") {
		return false
	}
	format := strings.ToLower(strings.TrimSpace(prop.Format))
	if format != "bool" && format != "boolean" {
		return false
	}
	name := normalizeSpec(prop.Name)
	typ := strings.ToLower(strings.TrimSpace(prop.Type))
	description := strings.ToLower(strings.TrimSpace(prop.Description))
	if name == "on" || name == "switch" || name == "power" || name == "powerstate" || name == "switchstate" || name == "switchstatus" || name == "onoff" {
		return true
	}
	if strings.Contains(typ, ":property:on:") || strings.Contains(typ, ":property:switch:") || strings.Contains(typ, ":property:power:") {
		return true
	}
	return strings.Contains(description, "开关状态") || strings.Contains(description, "电源开关")
}

func ControlLabel(name, description string) string {
	n := normalize(name)
	labels := map[string]string{
		"on": "电源开关", "power": "电源开关", "switch": "电源开关", "powerstate": "电源开关", "switchstate": "电源开关", "switchstatus": "电源开关", "onoff": "电源开关",
		"brightness": "亮度", "brightnesslevel": "亮度", "colortemperature": "色温", "colourtemperature": "色温",
		"volume": "音量", "volumelevel": "音量", "ismuted": "静音", "mute": "静音", "mode": "工作模式", "workmode": "工作模式", "fanlevel": "风速", "fanspeed": "风速",
		"startclean": "开始清扫", "startcleaning": "开始清扫", "startcleaningservice": "开始清扫", "startwork": "开始工作", "startsweep": "开始清扫", "clean": "开始清扫",
		"pause": "暂停", "pauseclean": "暂停清扫", "pausecleaning": "暂停清扫", "resume": "继续清扫", "stop": "停止", "stopclean": "停止清扫", "stopcleaning": "停止清扫",
		"returnhome": "返回充电座", "gotohome": "返回充电座", "returntocharger": "返回充电座", "returntocharge": "返回充电座", "dock": "返回充电座", "charge": "开始充电",
		"locate": "寻找设备", "finddevice": "寻找设备", "findrobot": "寻找机器人",
		"startstream": "启动视频流", "startvideo": "启动视频", "startvideostream": "启动视频流", "stopstream": "停止视频流", "stopvideo": "停止视频", "stopvideostream": "停止视频流",
		"motiondetection": "移动侦测", "recording": "录像开关", "recordenable": "录像开关", "privacy": "隐私模式", "privacymode": "隐私模式", "privacyprotection": "隐私保护",
	}
	if label := labels[n]; label != "" {
		return label
	}
	if value := strings.TrimSpace(description); value != "" {
		return value
	}
	if value := strings.TrimSpace(name); value != "" {
		return value
	}
	return "设备操作"
}

func buildOptions(values []ValueOption) []ControlOption {
	if len(values) == 0 {
		return nil
	}
	out := make([]ControlOption, 0, len(values))
	for _, item := range values {
		label := strings.TrimSpace(item.Description)
		if label == "" {
			label = fmt.Sprint(item.Value)
		}
		out = append(out, ControlOption{Value: item.Value, Label: label})
	}
	return out
}

func isSensitiveProperty(prop PropertyDefinition) bool {
	name := normalizeSpec(prop.Name + " " + prop.Description + " " + prop.Type)
	for _, part := range []string{"password", "token", "secret", "credential", "ssid", "privatekey", "factoryreset", "reset"} {
		if strings.Contains(name, part) {
			return true
		}
	}
	return false
}

func isDangerousAction(action ActionDefinition) bool {
	name := normalizeSpec(action.Name + " " + action.Description + " " + action.Type)
	for _, part := range []string{"factoryreset", "formatstorage", "deleteall", "cleardata", "restorefactory", "wipestorage", "erase", "delete", "removeall", "unbind", "unpair", "format", "reset"} {
		if strings.Contains(name, part) {
			return true
		}
	}
	return false
}

func hasSpecAccess(access []string, target string) bool {
	for _, value := range access {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func normalizeSpec(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
