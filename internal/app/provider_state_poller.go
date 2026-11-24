package app

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Provider state is refreshed separately from the browser's local API polling.
// The web page only reads GoHome's database; this worker asks each brand for a
// fresh cloud snapshot and writes live values back to the store.
const providerStatePollInterval = 30 * time.Second

func (m *integrationManager) runProviderStatePoller(ctx context.Context) {
	if m == nil {
		return
	}
	// A first pass on startup prevents a restart from displaying stale switch
	// states for a full polling interval.
	m.refreshAllProviderStates(ctx)
	ticker := time.NewTicker(providerStatePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.refreshAllProviderStates(ctx)
		}
	}
}

func (m *integrationManager) refreshAllProviderStates(ctx context.Context) {
	if m == nil || m.store == nil || m.vault == nil {
		return
	}
	m.providerPollMu.Lock()
	defer m.providerPollMu.Unlock()

	snapshot := m.store.snapshot()
	for _, account := range snapshot.Accounts {
		if ctx.Err() != nil {
			return
		}
		if !providerAccountCanPoll(account.Status) {
			continue
		}
		accountCtx, cancel := context.WithTimeout(ctx, 55*time.Second)
		changed, err := m.refreshProviderStatesForAccount(accountCtx, account)
		cancel()
		if err != nil {
			m.noteProviderPollIssue(account, err)
			continue
		}
		_ = changed // intentionally avoid writing a log entry for every polling cycle.
	}
}

func providerAccountCanPoll(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "connected", "sync_error":
		// A transient manual sync error must not stop live status refreshes.
		// Expired credentials will fail safely and be rate-limited in the log.
		return true
	default:
		return false
	}
}

func (m *integrationManager) refreshProviderStatesForAccount(ctx context.Context, account Account) (int, error) {
	var secret providerSecret
	if err := m.vault.Get(account.ID, &secret); err != nil {
		return 0, fmt.Errorf("读取账号会话失败：%w", err)
	}
	var devices []Device
	switch strings.ToLower(account.Provider) {
	case "xiaomi":
		var err error
		devices, err = xiaomiFetchDevicesForStateRefresh(ctx, secret)
		if err != nil {
			return 0, fmt.Errorf("小米云端状态读取失败：%w", err)
		}
	case "ewelink":
		refreshed, err := m.ensureEwelinkToken(ctx, account.ID, secret)
		if err != nil {
			return 0, fmt.Errorf("刷新 eWeLink 会话失败：%w", err)
		}
		secret = refreshed
		var keys map[string]string
		devices, keys, err = ewelinkFetchDevicesForStateRefresh(ctx, secret)
		if err != nil {
			return 0, fmt.Errorf("eWeLink 云端状态读取失败：%w", err)
		}
		if len(keys) > 0 && !reflect.DeepEqual(keys, secret.DeviceKeys) {
			secret.DeviceKeys = keys
			if err := m.vault.Put(account.ID, secret); err != nil {
				return 0, fmt.Errorf("保存 eWeLink 设备密钥失败：%w", err)
			}
		}
	default:
		return 0, nil
	}

	changed, err := m.store.refreshDeviceStates(account.ID, devices)
	if err != nil {
		return changed, fmt.Errorf("保存设备状态失败：%w", err)
	}
	if strings.EqualFold(account.Provider, "ewelink") {
		// Feed the same fresh readings into the local energy accumulator. Missing
		// cloud day/week totals are not rendered as fake values by the frontend.
		for _, device := range devices {
			if device.Online && device.Energy != nil && device.Energy.Supported {
				if err := m.store.recordEnergySample(device.ID, device.Energy, time.Now()); err != nil {
					m.noteProviderPollIssue(account, fmt.Errorf("保存 %s 电量采样失败：%w", device.Name, err))
				}
			}
		}
	}
	return changed, nil
}

func (m *integrationManager) noteProviderPollIssue(account Account, err error) {
	if m == nil || m.store == nil || err == nil {
		return
	}
	key := "state-poll|" + account.ID
	now := time.Now()
	m.mu.Lock()
	if m.energyWarnAt == nil {
		m.energyWarnAt = make(map[string]time.Time)
	}
	last := m.energyWarnAt[key]
	if !last.IsZero() && now.Sub(last) < 5*time.Minute {
		m.mu.Unlock()
		return
	}
	m.energyWarnAt[key] = now
	m.mu.Unlock()
	m.store.addLog("warn", fmt.Sprintf("[%s] 后台状态同步失败：%s", providerDisplayName(account.Provider), sanitizeIntegrationLog(err.Error())))
}

func providerDisplayName(provider string) string {
	switch strings.ToLower(provider) {
	case "xiaomi":
		return "小米 / 米家"
	case "ewelink":
		return "易微联 / eWeLink"
	default:
		return provider
	}
}
