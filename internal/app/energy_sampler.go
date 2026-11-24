package app

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	energyMaxSampleGap = 15 * time.Minute
	energyHistoryDays  = 14
	energyWarnInterval = 15 * time.Minute
)

func deletedDeviceKey(accountID, deviceID string) string {
	return strings.TrimSpace(accountID) + "|" + strings.TrimSpace(deviceID)
}

// sampleEwelinkEnergyForAccount performs an initial baseline/status read after
// an account sync. Routine refreshes are handled by provider_state_poller.go.
func (m *integrationManager) sampleEwelinkEnergyForAccount(ctx context.Context, onlyAccountID string) {
	if m == nil || m.store == nil || m.vault == nil {
		return
	}
	// Avoid overlapping initial baseline samples for the same account/device set.
	m.energySampleMu.Lock()
	defer m.energySampleMu.Unlock()

	snapshot := m.store.snapshot()
	devices := snapshot.Devices
	secrets := make(map[string]providerSecret)
	secretLoaded := make(map[string]bool)
	client := &http.Client{Timeout: 12 * time.Second}
	var lastStatusRequest time.Time

	for _, device := range devices {
		if ctx.Err() != nil {
			return
		}
		if device.Protocol != "ewelink-cloud" || !device.Online || device.AccountID == "" || device.RemoteID == "" || device.Energy == nil || !device.Energy.Supported {
			continue
		}
		if onlyAccountID != "" && device.AccountID != onlyAccountID {
			continue
		}

		secret, exists := secrets[device.AccountID]
		if !exists && !secretLoaded[device.AccountID] {
			secretLoaded[device.AccountID] = true
			if err := m.vault.Get(device.AccountID, &secret); err != nil {
				m.noteEnergySamplerIssue(device, fmt.Errorf("读取账号凭证失败：%w", err))
				continue
			}
			var err error
			secret, err = m.ensureEwelinkToken(ctx, device.AccountID, secret)
			if err != nil {
				m.noteEnergySamplerIssue(device, fmt.Errorf("刷新 eWeLink 会话失败：%w", err))
				continue
			}
			secrets[device.AccountID] = secret
		} else if !exists {
			continue
		}

		requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if waitErr := waitEwelinkStatusInterval(requestCtx, &lastStatusRequest); waitErr != nil {
			cancel()
			return
		}
		params, err := ewelinkGetDeviceStatus(requestCtx, secret, device.RemoteID, client, ewelinkHost(secret.Region))
		cancel()
		if err != nil {
			now := time.Now()
			m.seedEnergyBaselineFromCachedSnapshot(device, snapshot, now)
			m.noteEnergySamplerIssue(device, fmt.Errorf("读取设备状态失败：%w", err))
			_ = m.store.recordEnergySample(device.ID, &EnergyMetrics{Supported: true, Source: "eWeLink（状态读取失败，使用短时估算）"}, now)
			continue
		}
		if len(params) == 0 {
			now := time.Now()
			m.seedEnergyBaselineFromCachedSnapshot(device, snapshot, now)
			m.noteEnergySamplerIssue(device, fmt.Errorf("状态接口没有返回 params；设备当前缓存功率=%s", cachedPowerText(device)))
			_ = m.store.recordEnergySample(device.ID, &EnergyMetrics{Supported: true, Source: "eWeLink（状态未返回数据，使用短时估算）"}, now)
			continue
		}
		observed := parseEwelinkEnergyMetrics(params, device.Model)
		if observed == nil {
			m.noteEnergySamplerIssue(device, fmt.Errorf("状态响应中没有识别到计量字段；返回参数键：%s", energyParameterNames(params)))
			// The device was previously recognized as a meter; keep its local
			// history moving using only a still-fresh prior measurement.
			observed = &EnergyMetrics{Supported: true, Source: "eWeLink（本次状态未返回计量字段）"}
		}
		if observed.PowerW == nil && observed.TodayKWh == nil && observed.WeekKWh == nil && observed.MonthKWh == nil {
			m.noteEnergySamplerIssue(device, fmt.Errorf("状态接口未返回 power/dailyKwh/monthlyKwh 等可累计字段；返回参数键：%s；同步缓存功率=%s", energyParameterNames(params), cachedPowerText(device)))
			m.seedEnergyBaselineFromCachedSnapshot(device, snapshot, time.Now())
			// Voltage/current are still useful readings. The accumulator may extend
			// the most recent power sample for at most 15 minutes; it never treats
			// an old cached value as a new live sample.
			if err := m.store.recordEnergySample(device.ID, observed, time.Now()); err != nil {
				m.noteEnergySamplerIssue(device, fmt.Errorf("保存电量采样失败：%w", err))
			}
			continue
		}
		if err := m.store.recordEnergySample(device.ID, observed, time.Now()); err != nil {
			m.noteEnergySamplerIssue(device, fmt.Errorf("保存电量采样失败：%w", err))
		}
	}
}

func cachedPowerText(device Device) string {
	if device.Energy == nil || device.Energy.PowerW == nil {
		return "无"
	}
	return fmt.Sprintf("%.1fW", *device.Energy.PowerW)
}

// seedEnergyBaselineFromCachedSnapshot uses the just-synced device reading once
// when the status endpoint omits meter fields. It is a baseline only, not a
// repeating substitute for live power telemetry.
func (m *integrationManager) seedEnergyBaselineFromCachedSnapshot(device Device, snapshot diskState, now time.Time) {
	history := snapshot.EnergyHistory[device.ID]
	if !history.LastSample.IsZero() || device.Energy == nil || device.Energy.PowerW == nil || device.Energy.UpdatedAt.IsZero() {
		return
	}
	age := now.Sub(device.Energy.UpdatedAt)
	if age < 0 || age > 10*time.Minute {
		return
	}
	seed := &EnergyMetrics{
		Supported:   true,
		PowerW:      cloneFloat(device.Energy.PowerW),
		Source:      "eWeLink 同步快照（仅建立采样基线）",
		TodaySource: "本机基线来自最近同步快照 · 从此刻开始",
	}
	if err := m.store.recordEnergySample(device.ID, seed, now); err != nil {
		m.noteEnergySamplerIssue(device, fmt.Errorf("建立初始计量基线失败：%w", err))
	}
}

// energyParameterNames reports only known meter-related key names, never values.
// It gives users useful diagnostics without leaking device payloads or credentials.
func energyParameterNames(params map[string]any) string {
	known := map[string]bool{
		"power": true, "apower": true, "activepower": true, "electricpower": true,
		"powerw": true, "power1": true, "voltage": true, "voltagev": true,
		"current": true, "currenta": true, "dailykwh": true, "daykwh": true,
		"todaykwh": true, "monthlykwh": true, "monthkwh": true, "weekkwh": true,
		"weeklykwh": true, "energyday": true, "energyweek": true, "energymonth": true,
		"energy": true, "energy1": true, "energy2": true,
	}
	keys := make([]string, 0, len(params))
	for key := range params {
		if known[normalizedMetricKey(key)] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "未发现已知计量字段（仅记录字段名，不记录值）"
	}
	return strings.Join(keys, ", ")
}

// noteEnergySamplerIssue records actionable errors without flooding the log.
// Messages are redacted before they are persisted.
func (m *integrationManager) noteEnergySamplerIssue(device Device, err error) {
	if m == nil || m.store == nil || err == nil {
		return
	}
	now := time.Now()
	key := device.AccountID + "|" + device.ID
	m.mu.Lock()
	if m.energyWarnAt == nil {
		m.energyWarnAt = make(map[string]time.Time)
	}
	last := m.energyWarnAt[key]
	if !last.IsZero() && now.Sub(last) < energyWarnInterval {
		m.mu.Unlock()
		return
	}
	m.energyWarnAt[key] = now
	m.mu.Unlock()

	name := firstNonEmpty(device.Name, device.ID)
	m.store.addLog("warn", fmt.Sprintf("[电量采集] %s：%s", name, sanitizeIntegrationLog(err.Error())))
}

// recordEnergySample merges cloud metrics and stores a small daily history.
// Power integration is only performed when two real samples are no more than
// 15 minutes apart, preventing invented energy during outages or app downtime.
func (s *Store) recordEnergySample(deviceID string, observed *EnergyMetrics, now time.Time) error {
	if observed == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Devices {
		device := &s.data.Devices[i]
		if device.ID != deviceID {
			continue
		}
		if device.Energy == nil {
			device.Energy = &EnergyMetrics{Supported: true, Source: "eWeLink"}
		}
		if s.data.EnergyHistory == nil {
			s.data.EnergyHistory = make(map[string]EnergyAccumulator)
		}

		history := s.data.EnergyHistory[deviceID]
		if history.Days == nil {
			history.Days = make(map[string]float64)
		}
		localNow := now.In(time.Local)
		dayKey := localNow.Format("2006-01-02")
		// Migrate a same-day cloud counter already imported during account sync
		// into the local day map before a sparse status response can replace it.
		if _, exists := history.Days[dayKey]; !exists && device.Energy.TodayKWh != nil && finiteNonNegative(*device.Energy.TodayKWh) && !device.Energy.UpdatedAt.IsZero() && device.Energy.UpdatedAt.In(time.Local).Format("2006-01-02") == dayKey {
			history.Days[dayKey] = *device.Energy.TodayKWh
		}

		todaySource := ""
		if observed.TodayKWh != nil && finiteNonNegative(*observed.TodayKWh) {
			// dailyKwh/dayKwh is cumulative since local midnight.
			if old, ok := history.Days[dayKey]; !ok || *observed.TodayKWh > old {
				history.Days[dayKey] = *observed.TodayKWh
			}
			history.LastIntegratedAt = now // the cloud counter already includes usage up to now
			todaySource = "eWeLink 云端日累计"
		} else if observed.MonthKWh != nil && finiteNonNegative(*observed.MonthKWh) {
			// For firmwares with only a month counter, derive a daily increment
			// only from two samples within the same local date and month.
			if _, exists := history.Days[dayKey]; !exists {
				history.Days[dayKey] = 0
			}
			if history.LastMonthKWh != nil && !history.LastMonthAt.IsZero() {
				lastLocal := history.LastMonthAt.In(time.Local)
				sameDay := lastLocal.Format("2006-01-02") == dayKey
				sameMonth := lastLocal.Format("2006-01") == localNow.Format("2006-01")
				delta := *observed.MonthKWh - *history.LastMonthKWh
				if sameDay && sameMonth && finiteNonNegative(delta) {
					history.Days[dayKey] += delta
				}
			}
			history.LastIntegratedAt = now // avoid double-counting the month-counter delta with power integration
			todaySource = "本机月累计差值 · 从首次采集开始"
		} else {
			// Cloud polling can omit power values even when the last sync response
			// contained one. Extend that last real measurement for at most 15 min,
			// explicitly labeling the result as an estimate. Never extrapolate
			// across a day boundary or after the reading has gone stale.
			currentPower := cloneFloat(observed.PowerW)
			usingPreviousPower := false
			if currentPower == nil && history.LastPowerW != nil {
				age := now.Sub(history.LastSample)
				if !history.LastSample.IsZero() && age >= 0 && age <= energyMaxSampleGap && history.LastSample.In(time.Local).Format("2006-01-02") == dayKey {
					currentPower = cloneFloat(history.LastPowerW)
					usingPreviousPower = true
				}
			}
			if currentPower != nil && finiteNonNegative(*currentPower) {
				if _, exists := history.Days[dayKey]; !exists {
					history.Days[dayKey] = 0
				}
				if history.LastPowerW != nil && !history.LastSample.IsZero() {
					baseAt := history.LastIntegratedAt
					if baseAt.IsZero() {
						baseAt = history.LastSample
					}
					elapsed := now.Sub(baseAt)
					fresh := now.Sub(history.LastSample) >= 0 && now.Sub(history.LastSample) <= energyMaxSampleGap
					sameDay := history.LastSample.In(time.Local).Format("2006-01-02") == dayKey
					if fresh && sameDay && elapsed > 0 && elapsed <= energyMaxSampleGap {
						averageW := (*history.LastPowerW + *currentPower) / 2
						addedKWh := averageW * elapsed.Hours() / 1000
						if finiteNonNegative(addedKWh) {
							history.Days[dayKey] += addedKWh
						}
					}
				}
				history.LastIntegratedAt = now
				if usingPreviousPower {
					todaySource = "本机功率估算 · 延用最近一次有效读数（最多15分钟）"
				} else {
					todaySource = "本机功率积分估算 · 从首次采集开始"
				}
			} else if _, exists := history.Days[dayKey]; exists {
				todaySource = "本机累计采集 · 等待下一次有效功率读数"
			}
		}

		if observed.MonthKWh != nil && finiteNonNegative(*observed.MonthKWh) {
			history.LastMonthKWh = cloneFloat(observed.MonthKWh)
			history.LastMonthAt = now
		}
		if observed.PowerW != nil && finiteNonNegative(*observed.PowerW) {
			history.LastPowerW = cloneFloat(observed.PowerW)
			history.LastSample = now // actual observation time, not extrapolation time
			history.LastIntegratedAt = now
		}
		trimEnergyDays(history.Days, localNow.AddDate(0, 0, -energyHistoryDays))
		s.data.EnergyHistory[deviceID] = history

		energy := device.Energy
		energy.Supported = true
		if observed.Source != "" {
			energy.Source = observed.Source
		}
		if observed.PowerW != nil {
			energy.PowerW = cloneFloat(observed.PowerW)
		}
		if observed.VoltageV != nil {
			energy.VoltageV = cloneFloat(observed.VoltageV)
		}
		if observed.CurrentA != nil {
			energy.CurrentA = cloneFloat(observed.CurrentA)
		}
		if observed.MonthKWh != nil {
			energy.MonthKWh = cloneFloat(observed.MonthKWh)
		}
		if observed.YearKWh != nil {
			energy.YearKWh = cloneFloat(observed.YearKWh)
		}
		if observed.TotalKWh != nil {
			energy.TotalKWh = cloneFloat(observed.TotalKWh)
		}
		if observed.HistoryKWh != nil {
			energy.HistoryKWh = cloneFloat(observed.HistoryKWh)
		}

		if observed.TodayKWh != nil {
			energy.TodayKWh = cloneFloat(observed.TodayKWh)
			energy.TodaySource = firstNonEmpty(observed.TodaySource, "eWeLink 云端日统计")
		} else if today, ok := history.Days[dayKey]; ok {
			energy.TodayKWh = floatPointer(today)
			energy.TodaySource = firstNonEmpty(todaySource, observed.TodaySource, "本机累计采集 · 从首次采集开始")
		} else if energy.TodayKWh != nil && !energy.UpdatedAt.IsZero() && energy.UpdatedAt.In(time.Local).Format("2006-01-02") == dayKey {
			energy.TodaySource = firstNonEmpty(energy.TodaySource, observed.TodaySource, "保留同日最近一次云端读数")
		} else {
			energy.TodayKWh = nil
			energy.TodaySource = firstNonEmpty(observed.TodaySource, "等待日统计、月累计或有效功率")
		}

		if observed.WeekKWh != nil {
			energy.WeekKWh = cloneFloat(observed.WeekKWh)
			energy.WeekSource = firstNonEmpty(observed.WeekSource, "eWeLink 云端周统计")
		} else {
			weekStart := startOfLocalWeek(localNow).Format("2006-01-02")
			var total float64
			count := 0
			firstDay := ""
			for day, value := range history.Days {
				if day >= weekStart && day <= dayKey {
					total += value
					count++
					if firstDay == "" || day < firstDay {
						firstDay = day
					}
				}
			}
			hasRecentCloudWeek := energy.WeekKWh != nil && strings.Contains(strings.ToLower(energy.WeekSource), "云端") && !energy.UpdatedAt.IsZero() && startOfLocalWeek(energy.UpdatedAt.In(time.Local)).Equal(startOfLocalWeek(localNow))
			if hasRecentCloudWeek {
				// Preserve an imported/current-week cloud counter rather than replacing
				// it with a partial local sum that only covers days since GoHome started.
				energy.WeekSource = firstNonEmpty(energy.WeekSource, observed.WeekSource, "保留本周最近一次云端统计")
			} else if count > 0 {
				energy.WeekKWh = floatPointer(total)
				energy.WeekSource = fmt.Sprintf("本机每日累计 · 数据自 %s 开始", firstDay)
			} else if energy.WeekKWh != nil && !energy.UpdatedAt.IsZero() && startOfLocalWeek(energy.UpdatedAt.In(time.Local)).Equal(startOfLocalWeek(localNow)) {
				energy.WeekSource = firstNonEmpty(energy.WeekSource, observed.WeekSource, "保留本周最近一次统计读数")
			} else {
				energy.WeekKWh = nil
				energy.WeekSource = firstNonEmpty(observed.WeekSource, "等待本周有效电量采样")
			}
		}
		energy.UpdatedAt = now
		device.Energy = energy
		if err := s.saveLocked(); err != nil {
			return err
		}
		s.liveRevision++
		return nil
	}
	return nil
}

func trimEnergyDays(days map[string]float64, cutoff time.Time) {
	cutoffKey := cutoff.Format("2006-01-02")
	for day := range days {
		if day < cutoffKey {
			delete(days, day)
		}
	}
}

func finiteNonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func floatPointer(value float64) *float64 { return &value }
