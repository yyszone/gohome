package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func intValue(v any, fallback int) int {
	if n, ok := v.(float64); ok {
		return int(n)
	}
	if n, ok := v.(int); ok {
		return n
	}
	return fallback
}

// eWeLink meter values are optional. Keep their original semantic meaning:
// `energy` is historical cloud data, not a lifetime kWh counter.
func numericMetric(v any) *float64 {
	if v == nil {
		return nil
	}
	var n float64
	switch value := v.(type) {
	case float64:
		n = value
	case float32:
		n = float64(value)
	case int:
		n = float64(value)
	case int64:
		n = float64(value)
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return nil
		}
		n = parsed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(value, "kWh")), 64)
		if err != nil {
			return nil
		}
		n = parsed
	default:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(v)), 64)
		if err != nil {
			return nil
		}
		n = parsed
	}
	if n < 0 || n > 1e12 {
		return nil
	}
	return &n
}

func normalizedMetricKey(s string) string {
	return strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(strings.ToLower(s))
}

func findMetric(params map[string]any, names ...string) *float64 {
	if len(params) == 0 {
		return nil
	}
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[normalizedMetricKey(name)] = true
	}
	for key, value := range params {
		if wanted[normalizedMetricKey(key)] {
			if n := numericMetric(value); n != nil {
				return n
			}
		}
	}
	return nil
}

func readBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if e != nil {
		return nil, e
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("平台返回 HTTP %d：%s", resp.StatusCode, truncate(string(b), 400))
	}
	return b, nil
}

func stringValue(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func boolValue(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x == "1" || strings.EqualFold(x, "true") || strings.EqualFold(x, "online")
	}
	return false
}

func parseSwitch(v any) *bool {
	if v == nil {
		return nil
	}
	s := strings.ToLower(fmt.Sprint(v))
	b := s == "on" || s == "true" || s == "1"
	if s == "off" || s == "false" || s == "0" {
		b = false
	} else if !b {
		return nil
	}
	return &b
}

func guessCategory(name, model string) string {
	s := strings.ToLower(name + " " + model)
	switch {
	case strings.Contains(s, "camera"), strings.Contains(s, "video-doorbell"), strings.Contains(s, "cateye"), strings.Contains(s, "摄像"), strings.Contains(s, "门铃"):
		return "摄像头"
	case strings.Contains(s, "speaker"), strings.Contains(s, "wifispeaker"), strings.Contains(s, "小爱"), strings.Contains(s, "音箱"):
		return "音箱"
	case strings.Contains(s, "temperature-humidity-sensor"), strings.Contains(s, "sensor_ht"), strings.Contains(s, "thermometer"), strings.Contains(s, "温度"), strings.Contains(s, "湿度"):
		return "温湿度计"
	case strings.Contains(s, "motion-sensor"), strings.Contains(s, "sensor_smoke"), strings.Contains(s, "flood"), strings.Contains(s, "人体传感器"), strings.Contains(s, "门窗传感器"), strings.Contains(s, "sensor"):
		return "传感器"
	case strings.Contains(s, "light"), strings.Contains(s, "lamp"), strings.Contains(s, "灯"):
		return "灯光"
	case strings.Contains(s, "plug"), strings.Contains(s, "socket"), strings.Contains(s, "outlet"), strings.Contains(s, "power strip"), strings.Contains(s, "插座"), strings.Contains(s, "插排"), strings.Contains(s, "插线板"), strings.Contains(s, "排插"):
		return "插座"
	case strings.Contains(s, "switch"), strings.Contains(s, "开关"):
		return "开关"
	case strings.Contains(s, "air-conditioner"), strings.Contains(s, "aircondition"), strings.Contains(s, "acpartner"), strings.Contains(s, "空调"):
		return "空调"
	case strings.Contains(s, "vacuum"), strings.Contains(s, "扫地机器人"), strings.Contains(s, "扫拖机器人"):
		return "扫地机器人"
	case strings.Contains(s, "fan"), strings.Contains(s, "风扇"):
		return "风扇"
	case strings.Contains(s, "dishwasher"), strings.Contains(s, "洗碗机"):
		return "洗碗机"
	case strings.Contains(s, "fridge"), strings.Contains(s, "refrigerator"), strings.Contains(s, "冰箱"):
		return "冰箱"
	case strings.Contains(s, "humidifier"), strings.Contains(s, "dehumidifier"), strings.Contains(s, "加湿器"), strings.Contains(s, "除湿机"):
		return "加湿器"
	case strings.Contains(s, "air-purifier"), strings.Contains(s, "airpurifier"), strings.Contains(s, "空气净化"):
		return "空气净化器"
	case strings.Contains(s, "lock"), strings.Contains(s, "门锁"):
		return "智能门锁"
	case strings.Contains(s, "curtain"), strings.Contains(s, "窗帘"):
		return "窗帘"
	case strings.Contains(s, "watch"), strings.Contains(s, "band"), strings.Contains(s, "手环"), strings.Contains(s, "手表"):
		return "穿戴设备"
	case strings.Contains(s, "router"), strings.Contains(s, "路由器"):
		return "网络设备"
	case strings.Contains(s, "television"), strings.Contains(s, "tvbox"), strings.Contains(s, "projector"), strings.Contains(s, "显示器"), strings.Contains(s, "电视"):
		return "影音设备"
	default:
		return "其他"
	}
}

func safeID(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
