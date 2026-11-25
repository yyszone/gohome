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

// pollWakeLocked returns the coalescing wake channel. Call with activityMu held.
func (m *integrationManager) pollWakeLocked() chan struct{} {
	if m.pollWake == nil {
		m.pollWake = make(chan struct{}, 1)
	}
	return m.pollWake
}

// signalPollerLocked wakes the scheduler without blocking request handlers.
func (m *integrationManager) signalPollerLocked() {
	ch := m.pollWakeLocked()
	select {
	case ch <- struct{}{}:
	default:
	}
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
	m.signalPollerLocked()
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
	for {
		wake, active, wait := m.nextProviderPollWait(time.Now())
		if !active {
			// No visible authenticated overview/device page: sleep without a
			// periodic ticker. The next page lease wakes this goroutine.
			select {
			case <-ctx.Done():
				m.cancelProviderStatePoll()
				return
			case <-wake:
				continue
			}
		}

		if wait < 0 {
			// A provider poll is already in flight. Wait for a lease change or
			// the earliest lease expiry; do not spin while cloud I/O is pending.
			leaseTimer := time.NewTimer(m.earliestPageLeaseWait(time.Now()))
			select {
			case <-ctx.Done():
				if !leaseTimer.Stop() {
					select {
					case <-leaseTimer.C:
					default:
					}
				}
				m.cancelProviderStatePoll()
				return
			case <-wake:
				if !leaseTimer.Stop() {
					select {
					case <-leaseTimer.C:
					default:
					}
				}
				continue
			case <-leaseTimer.C:
				continue
			}
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			m.cancelProviderStatePoll()
			return
		case <-wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			continue
		case now := <-timer.C:
			m.startProviderStatePollIfNeeded(ctx, now)
		}
	}
}

// nextProviderPollWait returns whether a page lease is active and how long
// the scheduler may sleep. It also schedules a wake at the nearest lease
// expiry so a crashed browser cannot keep cloud polling alive indefinitely.
func (m *integrationManager) nextProviderPollWait(now time.Time) (chan struct{}, bool, time.Duration) {
	if m == nil {
		return nil, false, 0
	}
	m.activityMu.Lock()
	defer m.activityMu.Unlock()
	wake := m.pollWakeLocked()
	earliest := time.Time{}
	for id, until := range m.pageLeases {
		if !until.After(now) {
			delete(m.pageLeases, id)
			continue
		}
		if earliest.IsZero() || until.Before(earliest) {
			earliest = until
		}
	}
	if len(m.pageLeases) == 0 {
		m.lastStatePoll = time.Time{}
		return wake, false, 0
	}
	leaseWait := earliest.Sub(now)
	if leaseWait < 0 {
		leaseWait = 0
	}
	if m.pollRunning {
		return wake, true, -1
	}
	pollWait := time.Duration(0)
	if !m.lastStatePoll.IsZero() {
		pollWait = providerStatePollInterval - now.Sub(m.lastStatePoll)
		if pollWait < 0 {
			pollWait = 0
		}
	}
	if leaseWait < pollWait {
		pollWait = leaseWait
	}
	return wake, true, pollWait
}

func (m *integrationManager) earliestPageLeaseWait(now time.Time) time.Duration {
	if m == nil {
		return pageActivityLease
	}
	m.activityMu.Lock()
	defer m.activityMu.Unlock()
	earliest := time.Time{}
	for id, until := range m.pageLeases {
		if !until.After(now) {
			delete(m.pageLeases, id)
			continue
		}
		if earliest.IsZero() || until.Before(earliest) {
			earliest = until
		}
	}
	if earliest.IsZero() {
		return 0
	}
	return earliest.Sub(now)
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
			m.signalPollerLocked()
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
