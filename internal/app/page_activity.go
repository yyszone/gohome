package app

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// Provider status polling is leased by authenticated, visible browser pages.
// A closed tab stops refreshing automatically when its lease expires.
const (
	pageActivityLease         = 20 * time.Second
	providerStatePollInterval = 10 * time.Second
	providerPollCheckInterval = 1 * time.Second
	maxActivePageLeases       = 32
)

func validPageClientID(id string) bool {
	id = strings.TrimSpace(id)
	if len(id) < 8 || len(id) > 80 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// updatePageActivity renews or releases a browser-page lease. Multiple tabs
// have independent IDs so hiding one tab does not stop another visible tab.
func (m *integrationManager) updatePageActivity(clientID string, active bool) int {
	if m == nil || !validPageClientID(clientID) {
		return 0
	}
	now := time.Now()
	m.activityMu.Lock()
	if m.pageLeases == nil {
		m.pageLeases = make(map[string]time.Time)
	}
	for id, until := range m.pageLeases {
		if !until.After(now) {
			delete(m.pageLeases, id)
		}
	}
	wasActive := len(m.pageLeases) > 0
	if active {
		if _, exists := m.pageLeases[clientID]; !exists && len(m.pageLeases) >= maxActivePageLeases {
			count := len(m.pageLeases)
			m.activityMu.Unlock()
			return count
		}
		m.pageLeases[clientID] = now.Add(pageActivityLease)
	} else {
		delete(m.pageLeases, clientID)
	}
	isActive := len(m.pageLeases) > 0
	if !wasActive && isActive {
		// A newly opened page must trigger an immediate refresh, not wait for
		// the previous session's interval.
		m.lastStatePoll = time.Time{}
	}
	if wasActive && !isActive {
		// Do not cancel a cloud request already in flight. A page hiding while
		// Xiaomi is waiting for device_list used to cancel the HTTP request and
		// surface a misleading "context canceled" sync failure. The current
		// account request is allowed to finish once; no new poll is started
		// without an active page lease.
		m.lastStatePoll = time.Time{}
	}
	count := len(m.pageLeases)
	m.activityMu.Unlock()
	return count
}

func (m *integrationManager) activePageCount(now time.Time) int {
	if m == nil {
		return 0
	}
	m.activityMu.Lock()
	for id, until := range m.pageLeases {
		if !until.After(now) {
			delete(m.pageLeases, id)
		}
	}
	count := len(m.pageLeases)
	if count == 0 {
		// Stop scheduling future polls, but let a request already in flight
		// complete. Its context is owned by the app lifetime, not by the page.
		m.lastStatePoll = time.Time{}
	}
	m.activityMu.Unlock()
	return count
}

func (m *integrationManager) runProviderStatePoller(ctx context.Context) {
	if m == nil {
		return
	}
	ticker := time.NewTicker(providerPollCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.cancelProviderStatePoll()
			return
		case now := <-ticker.C:
			m.startProviderStatePollIfNeeded(ctx, now)
		}
	}
}

func (m *integrationManager) startProviderStatePollIfNeeded(parent context.Context, now time.Time) bool {
	if m == nil {
		return false
	}
	m.activityMu.Lock()
	for id, until := range m.pageLeases {
		if !until.After(now) {
			delete(m.pageLeases, id)
		}
	}
	if len(m.pageLeases) == 0 {
		m.lastStatePoll = time.Time{}
		m.activityMu.Unlock()
		return false
	}
	if m.pollRunning || (!m.lastStatePoll.IsZero() && now.Sub(m.lastStatePoll) < providerStatePollInterval) {
		m.activityMu.Unlock()
		return false
	}
	pollCtx, cancel := context.WithCancel(parent)
	m.pollRunning = true
	m.pollCancel = cancel
	m.lastStatePoll = now
	m.activityMu.Unlock()

	go func() {
		defer func() {
			cancel()
			m.activityMu.Lock()
			m.pollRunning = false
			m.pollCancel = nil
			m.activityMu.Unlock()
		}()
		m.refreshAllProviderStates(pollCtx)
	}()
	return true
}

func (m *integrationManager) cancelProviderStatePoll() {
	if m == nil {
		return
	}
	m.activityMu.Lock()
	cancel := m.pollCancel
	m.activityMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) updateStateSyncActivity(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientID string `json:"clientId"`
		Active   bool   `json:"active"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, "页面活动请求格式无效")
		return
	}
	if !validPageClientID(req.ClientID) {
		fail(w, http.StatusBadRequest, "页面客户端标识无效")
		return
	}
	count := a.integrations.updatePageActivity(req.ClientID, req.Active)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                  true,
		"active":              req.Active,
		"activePages":         count,
		"pollIntervalSeconds": int(providerStatePollInterval.Seconds()),
		"leaseSeconds":        int(pageActivityLease.Seconds()),
	})
}
