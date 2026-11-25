package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Provider state refresh mechanics are page-lease driven in page_activity.go.
// The routines below only execute a refresh after a visible authenticated page
// has requested one.

func (m *integrationManager) refreshAllProviderStates(ctx context.Context) {
	if m == nil || m.store == nil || m.vault == nil {
		return
	}
	pollStartedAt := time.Now()
	m.providerPollMu.Lock()
	defer m.providerPollMu.Unlock()

	snapshot := m.store.snapshot()
	for _, account := range snapshot.Accounts {
		if ctx.Err() != nil || m.activePageCount(time.Now()) == 0 {
			return
		}
		if !providerAccountCanPoll(account.Status) {
			continue
		}
		accountCtx, cancel := context.WithTimeout(ctx, 55*time.Second)
		changed, err := m.refreshProviderStatesForAccountAt(accountCtx, account, pollStartedAt)
		accountContextErr := accountCtx.Err()
		cancel()
		if ctx.Err() != nil {
			return // The GoHome process itself is shutting down.
		}
		// A single account request can be canceled or time out without meaning
		// that its credentials expired. Do not stop polling other accounts. The
		// Xiaomi refresh path clears stale power values so the UI never keeps an
		// old ON state after an incomplete read.
		if errors.Is(accountContextErr, context.Canceled) || errors.Is(accountContextErr, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			if strings.EqualFold(account.Provider, "xiaomi") {
				_, _ = m.store.clearAccountPowerStatesSince(account.ID, pollStartedAt)
			}
			issue := err
			if issue == nil {
				issue = accountContextErr
			}
			m.noteProviderPollIssue(account, fmt.Errorf("本轮状态请求被取消或超时（不是登录失效），等待下一轮状态确认：%w", issue))
			continue
		}
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
	return m.refreshProviderStatesForAccountAt(ctx, account, time.Now())
}

func (m *integrationManager) refreshProviderStatesForAccountAt(ctx context.Context, account Account, pollStartedAt time.Time) (int, error) {
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
			// A failed list/property read must not leave an old "已开启" value
			// in the UI. Clear only cached power projections. The encrypted
			// Xiaomi session in the Vault is deliberately never modified here. If
			// this request began before a recent command, the Store preserves that
			// command's optimistic state rather than clearing it.
			if m.store != nil {
				if _, clearErr := m.store.clearAccountPowerStatesSince(account.ID, pollStartedAt); clearErr != nil {
					m.noteProviderPollIssue(account, fmt.Errorf("清除过期小米电源状态失败：%w", clearErr))
				}
			}
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

	if strings.EqualFold(account.Provider, "xiaomi") {
		issues := make([]string, 0, 5)
		for _, device := range devices {
			if strings.TrimSpace(device.StateIssue) == "" {
				continue
			}
			issues = append(issues, fmt.Sprintf("%s(%s): %s", device.Name, device.Model, device.StateIssue))
			if len(issues) >= 5 {
				break
			}
		}
		if len(issues) > 0 {
			m.noteProviderPollIssue(account, fmt.Errorf("MIoT 实时属性未全部读取：%s", strings.Join(issues, "；")))
		}
	}

	changed, err := m.store.refreshDeviceStatesSince(account.ID, devices, pollStartedAt)
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
