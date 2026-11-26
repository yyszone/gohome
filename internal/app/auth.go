package app

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Administrator credentials are generated per installation and stored in
// data/gohome.admin.password with filesystem mode 0600.
const (
	defaultAdminUsername = "admin"
	sessionCookieName    = "gohome_session"
)

const (
	sessionCookieLifetime = 10 * 365 * 24 * time.Hour
	loginFailureWindow    = 15 * time.Minute
	loginFailureLimit     = 5
	loginLockDuration     = 10 * time.Minute
)

type loginAttempt struct {
	Count    int
	WindowAt time.Time
	LockedTo time.Time
}

func loadOrCreateSessionKey(dataDir string) ([]byte, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "gohome.session.key")
	if key, err := os.ReadFile(path); err == nil {
		if len(key) != sha256.Size {
			return nil, fmt.Errorf("登录 Cookie 密钥长度无效：%s", path)
		}
		_ = os.Chmod(path, 0600)
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	key := make([]byte, sha256.Size)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("生成登录 Cookie 密钥失败：%w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		// Another process may have created the key concurrently.
		if errors.Is(err, os.ErrExist) {
			key, readErr := os.ReadFile(path)
			if readErr == nil && len(key) == sha256.Size {
				return key, nil
			}
		}
		return nil, err
	}
	if _, err = f.Write(key); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	return key, nil
}

func (a *App) isPublicAuthRequest(r *http.Request) bool {
	if r.Method == http.MethodPost && r.URL.Path == "/api/auth/login" {
		return true
	}
	if r.Method == http.MethodGet && r.URL.Path == "/api/auth/session" {
		return true
	}
	// The login screen and its embedded assets must load before authentication.
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return !strings.HasPrefix(r.URL.Path, "/api/")
	}
	return false
}

func sessionPayload() []byte { return []byte("gohome-session-v1:" + defaultAdminUsername) }

func (a *App) sessionToken() string {
	payload := sessionPayload()
	a.authMu.RLock()
	key := append([]byte(nil), a.authKey...)
	a.authMu.RUnlock()
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *App) hasValidSession(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	a.authMu.RLock()
	key := append([]byte(nil), a.authKey...)
	a.authMu.RUnlock()
	if len(key) != sha256.Size {
		return false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || subtle.ConstantTimeCompare(payload, sessionPayload()) != 1 {
		return false
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	return hmac.Equal(provided, mac.Sum(nil))
}

func (a *App) setSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: a.sessionToken(), Path: "/",
		Expires: time.Now().Add(sessionCookieLifetime), MaxAge: int(sessionCookieLifetime.Seconds()),
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode,
	})
}

func (a *App) authSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": a.authDisabled || a.hasValidSession(r),
		"username":      defaultAdminUsername,
	})
}

func remoteLoginIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func (a *App) loginBlocked(ip string, now time.Time) (bool, time.Duration) {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	attempt := a.loginAttempts[ip]
	if now.Before(attempt.LockedTo) {
		return true, time.Until(attempt.LockedTo)
	}
	if !attempt.LockedTo.IsZero() && !now.Before(attempt.LockedTo) {
		delete(a.loginAttempts, ip)
		return false, 0
	}
	if !attempt.WindowAt.IsZero() && now.Sub(attempt.WindowAt) > loginFailureWindow {
		delete(a.loginAttempts, ip)
	}
	return false, 0
}

func (a *App) recordLoginFailure(ip string, now time.Time) {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	attempt := a.loginAttempts[ip]
	if attempt.WindowAt.IsZero() || now.Sub(attempt.WindowAt) > loginFailureWindow {
		attempt = loginAttempt{WindowAt: now}
	}
	attempt.Count++
	if attempt.Count >= loginFailureLimit {
		attempt.LockedTo = now.Add(loginLockDuration)
	}
	a.loginAttempts[ip] = attempt
}

func (a *App) clearLoginFailures(ip string) {
	a.loginMu.Lock()
	delete(a.loginAttempts, ip)
	a.loginMu.Unlock()
}

func (a *App) authLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "登录请求格式错误"})
		return
	}
	ip := remoteLoginIP(r)
	if blocked, wait := a.loginBlocked(ip, time.Now()); blocked {
		w.Header().Set("Retry-After", fmt.Sprint(max(1, int(wait.Seconds()))))
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "登录尝试过于频繁，请稍后再试"})
		return
	}
	userOK := subtle.ConstantTimeCompare([]byte(in.Username), []byte(defaultAdminUsername)) == 1
	a.authMu.RLock()
	storedPassword := a.adminPassword
	a.authMu.RUnlock()
	passOK := subtle.ConstantTimeCompare([]byte(in.Password), []byte(storedPassword)) == 1
	if !userOK || !passOK {
		a.recordLoginFailure(ip, time.Now())
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "账号或密码错误"})
		return
	}
	a.clearLoginFailures(ip)
	a.setSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "username": defaultAdminUsername, "persistent": true})
}

func (a *App) authLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1,
		Expires: time.Unix(1, 0), HttpOnly: true, Secure: r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
