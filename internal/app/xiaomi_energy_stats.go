package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// Xiaomi energy history is model-specific. These profiles follow the model
// customizations used by the Home Assistant Xiaomi Miot integration. Unknown
// models are deliberately not assigned a guessed statistics key or scale.
type xiaomiEnergyStatsProfile struct {
	Keys     []string
	Scale    float64
	DataType string
}

func xiaomiEnergyStatsProfileForModel(model string) (xiaomiEnergyStatsProfile, bool) {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case m == "chuangmi.plug.212a01":
		return xiaomiEnergyStatsProfile{Keys: []string{"prop.5.1"}, Scale: 0.000001, DataType: "stat_day_v3"}, true
	case strings.HasPrefix(m, "cuco.plug.cp2d"):
		return xiaomiEnergyStatsProfile{Keys: []string{"3.1"}, Scale: 0.01, DataType: "stat_day_v3"}, true
	case strings.HasPrefix(m, "cuco.plug.cp1m"), strings.HasPrefix(m, "cuco.plug.cp1md"), strings.HasPrefix(m, "cuco.plug.cp4"):
		return xiaomiEnergyStatsProfile{Keys: []string{"2.2"}, Scale: 0.001, DataType: "stat_day_v3"}, true
	case m == "cuco.plug.cp2" || strings.HasPrefix(m, "cuco.plug.cp2."):
		return xiaomiEnergyStatsProfile{Keys: []string{"2.2"}, Scale: 0.01, DataType: "stat_day_v3"}, true
	case strings.HasPrefix(m, "cuco.plug.cp5prd"), strings.HasPrefix(m, "cuco.plug.cp5pro"):
		return xiaomiEnergyStatsProfile{Keys: []string{"10.1"}, Scale: 1, DataType: "stat_day_v3"}, true
	case m == "iot.plug.jdls1", m == "iot.switch.padw2p":
		return xiaomiEnergyStatsProfile{Keys: []string{"3.1"}, Scale: 1, DataType: "stat_day_v3"}, true
	case strings.HasPrefix(m, "zimi.plug.zncz01"):
		return xiaomiEnergyStatsProfile{Keys: []string{"3.2"}, Scale: 0.01, DataType: "stat_day_v3"}, true
	case strings.HasPrefix(m, "lumi.acpartner.mcn04"):
		return xiaomiEnergyStatsProfile{Keys: []string{"7.1", "7.3"}, Scale: 1, DataType: "stat_day_v3"}, true
	case strings.HasPrefix(m, "lumi.acpartner."):
		return xiaomiEnergyStatsProfile{Keys: []string{"powerCost"}, Scale: 0.001, DataType: "stat_day"}, true
	case m == "lumi.plug.v1" || strings.HasPrefix(m, "lumi.plug."):
		return xiaomiEnergyStatsProfile{Keys: []string{"powerCost"}, Scale: 1, DataType: "stat_day"}, true
	case strings.HasPrefix(m, "cuco.plug.v3"):
		return xiaomiEnergyStatsProfile{Keys: []string{"11.1"}, Scale: 0.01, DataType: "stat_day_v3"}, true
	case strings.HasPrefix(m, "cuco.plug.wp5m"):
		return xiaomiEnergyStatsProfile{Keys: []string{"3.1"}, Scale: 0.01, DataType: "stat_day_v3"}, true
	case strings.HasPrefix(m, "iot.plug.pw6u1"):
		return xiaomiEnergyStatsProfile{Keys: []string{"3.1"}, Scale: 1, DataType: "stat_day_v3"}, true
	case strings.HasPrefix(m, "qmi.plug.psv3"):
		return xiaomiEnergyStatsProfile{Keys: []string{"3.1"}, Scale: 1, DataType: "stat_day_v3"}, true
	case strings.HasPrefix(m, "qmi.plug.tw02"):
		return xiaomiEnergyStatsProfile{Keys: []string{"4.1"}, Scale: 1, DataType: "stat_day_v3"}, true
	case strings.HasPrefix(m, "qmi.plug.2a1c1"):
		return xiaomiEnergyStatsProfile{Keys: []string{"3.1"}, Scale: 1, DataType: "stat_day_v3"}, true
	case strings.HasPrefix(m, "xiaomi.plug.mcn003"):
		return xiaomiEnergyStatsProfile{Keys: []string{"3.1"}, Scale: 1, DataType: "stat_day_v3"}, true
	default:
		return xiaomiEnergyStatsProfile{}, false
	}
}

type xiaomiStatsRecord struct {
	Time  int64           `json:"time"`
	Value json.RawMessage `json:"value"`
}

// xiaomiStatsValueSum accepts the documented JSON-array string (including
// multi-channel meters) and scalar numeric responses. The caller applies the
// model-specific unit ratio only after summing the raw values.
func xiaomiStatsValueSum(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return 0, false
	}
	if text, ok := decoded.(string); ok {
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			return 0, false
		}
	}
	sum := 0.0
	count := 0
	var add func(any)
	add = func(v any) {
		switch n := v.(type) {
		case float64:
			if !math.IsNaN(n) && !math.IsInf(n, 0) {
				sum += n
				count++
			}
		case json.Number:
			if f, err := n.Float64(); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
				sum += f
				count++
			}
		case []any:
			for _, item := range n {
				add(item)
			}
		}
	}
	add(decoded)
	return sum, count > 0
}

// parseXiaomiDailyEnergyStats aggregates daily records into today's and this
// week's kWh using the server's local timezone (the NAS timezone). A row's
// value may contain several channel readings, which are summed first.
func parseXiaomiDailyEnergyStats(body []byte, weekStart, now time.Time, scale float64) (todayKWh, weekKWh *float64, err error) {
	var env struct {
		Code    int                 `json:"code"`
		Message string              `json:"message"`
		Result  []xiaomiStatsRecord `json:"result"`
	}
	if err = json.Unmarshal(body, &env); err != nil {
		return nil, nil, fmt.Errorf("解析小米用电统计失败：%w", err)
	}
	if env.Code != 0 {
		return nil, nil, fmt.Errorf("小米用电统计返回 %d：%s", env.Code, env.Message)
	}
	if strings.Contains(strings.ToLower(env.Message), "throttle") {
		return nil, nil, nil
	}
	localNow := now.In(time.Local)
	today := localNow.Format("2006-01-02")
	start := weekStart.In(time.Local)
	var todayTotal, weekTotal float64
	hasToday, hasWeek := false, false
	for _, row := range env.Result {
		if row.Time <= 0 {
			continue
		}
		when := time.Unix(row.Time, 0).In(time.Local)
		if when.Before(start) || when.After(localNow.Add(24*time.Hour)) {
			continue
		}
		value, ok := xiaomiStatsValueSum(row.Value)
		if !ok {
			continue
		}
		value *= scale
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		weekTotal += value
		hasWeek = true
		if when.Format("2006-01-02") == today {
			todayTotal += value
			hasToday = true
		}
	}
	if hasToday {
		todayKWh = &todayTotal
	}
	if hasWeek {
		weekKWh = &weekTotal
	}
	return todayKWh, weekKWh, nil
}

func startOfLocalWeek(now time.Time) time.Time {
	local := now.In(time.Local)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	// Go: Sunday=0. Monday is the first day of the week.
	daysSinceMonday := (int(midnight.Weekday()) + 6) % 7
	return midnight.AddDate(0, 0, -daysSinceMonday)
}

func xiaomiFetchEnergyStats(ctx context.Context, secret providerSecret, did, model string, now time.Time) (*float64, *float64, error) {
	profile, ok := xiaomiEnergyStatsProfileForModel(model)
	if !ok || strings.TrimSpace(did) == "" {
		return nil, nil, nil
	}
	weekStart := startOfLocalWeek(now)
	var today, week *float64
	for _, key := range profile.Keys {
		dataTypes := []string{profile.DataType}
		if profile.DataType != "stat_day" {
			dataTypes = append(dataTypes, "stat_day")
		}
		for typeIndex, dataType := range dataTypes {
			payload, _ := json.Marshal(map[string]any{
				"did": did, "key": key, "data_type": dataType,
				"time_start": weekStart.Unix(), "time_end": now.Unix(), "limit": 31,
			})
			body, err := xiaomiEncryptedPost(ctx, secret, "/v2/user/statistics", payload)
			if err != nil {
				if typeIndex+1 < len(dataTypes) {
					continue
				}
				break
			}
			if strings.Contains(strings.ToLower(string(body)), "sds throttle") {
				// Xiaomi documents this as code=0 with no usable result. Do not
				// retry the same meter immediately and amplify server throttling.
				break
			}
			dayValue, weekValue, parseErr := parseXiaomiDailyEnergyStats(body, weekStart, now, profile.Scale)
			if parseErr != nil {
				if typeIndex+1 < len(dataTypes) {
					continue
				}
				break
			}
			if dayValue == nil && weekValue == nil && typeIndex+1 < len(dataTypes) {
				continue // v3 returned no records; try the legacy daily series.
			}
			if dayValue != nil {
				if today == nil {
					today = dayValue
				} else {
					value := *today + *dayValue
					today = &value
				}
			}
			if weekValue != nil {
				if week == nil {
					week = weekValue
				} else {
					value := *week + *weekValue
					week = &value
				}
			}
			break
		}
	}
	return today, week, nil
}
