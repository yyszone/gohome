package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreateAdminPasswordIsStableAndPrivate(t *testing.T) {
	dir := t.TempDir()
	first, created, err := loadOrCreateAdminPassword(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first call should create password")
	}
	if len(first) != 48 {
		t.Fatalf("generated password length=%d want=48", len(first))
	}
	second, created, err := loadOrCreateAdminPassword(dir)
	if err != nil {
		t.Fatal(err)
	}
	if created || second != first {
		t.Fatal("second call must reuse the existing password")
	}
	info, err := os.Stat(filepath.Join(dir, adminPasswordFileName))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("password file mode=%04o want=0600", got)
	}
}

func TestChangePasswordRotatesSessionAndSurvivesRestart(t *testing.T) {
	a := testApp(t)
	a.authDisabled = false
	oldPassword := a.adminPassword
	login := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(fmt.Sprintf(`{"username":"admin","password":%q}`, oldPassword)))
	loginReq.RemoteAddr = "192.0.2.50:12345"
	a.ServeHTTP(login, loginReq)
	if login.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected login cookie, got %d", len(cookies))
	}

	newPassword := "123456"
	changeBody := fmt.Sprintf(`{"currentPassword":%q,"newPassword":%q,"confirmPassword":%q}`, oldPassword, newPassword, newPassword)
	changeReq := httptest.NewRequest(http.MethodPost, "/api/auth/password", strings.NewReader(changeBody))
	changeReq.AddCookie(cookies[0])
	changed := httptest.NewRecorder()
	a.ServeHTTP(changed, changeReq)
	if changed.Code != http.StatusOK {
		t.Fatalf("change password status=%d body=%s", changed.Code, changed.Body.String())
	}
	newCookies := changed.Result().Cookies()
	if len(newCookies) == 0 || newCookies[0].Name != sessionCookieName {
		t.Fatalf("new session cookie missing: %+v", newCookies)
	}

	oldRequest := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	oldRequest.AddCookie(cookies[0])
	oldResponse := httptest.NewRecorder()
	a.ServeHTTP(oldResponse, oldRequest)
	if oldResponse.Code != http.StatusUnauthorized {
		t.Fatalf("old cookie status=%d want=401", oldResponse.Code)
	}

	newRequest := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	newRequest.AddCookie(newCookies[0])
	newResponse := httptest.NewRecorder()
	a.ServeHTTP(newResponse, newRequest)
	if newResponse.Code != http.StatusOK {
		t.Fatalf("new cookie status=%d body=%s", newResponse.Code, newResponse.Body.String())
	}

	restarted := newApp(a.store)
	restarted.authDisabled = false
	loginAgain := httptest.NewRecorder()
	loginAgainReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(fmt.Sprintf(`{"username":"admin","password":%q}`, newPassword)))
	loginAgainReq.RemoteAddr = "192.0.2.51:12345"
	restarted.ServeHTTP(loginAgain, loginAgainReq)
	if loginAgain.Code != http.StatusOK {
		t.Fatalf("new password did not persist after restart: %d %s", loginAgain.Code, loginAgain.Body.String())
	}
}

func TestChangePasswordRejectsFewerThanSixCharacters(t *testing.T) {
	a := testApp(t)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/password", strings.NewReader(`{"currentPassword":"irrelevant","newPassword":"12345","confirmPassword":"12345"}`))
	resp := httptest.NewRecorder()
	a.changeAdminPassword(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("five-character password status=%d want=%d body=%s", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
}
