package app

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const adminPasswordFileName = "gohome.admin.password"

// loadOrCreateAdminPassword returns the first-run password and whether it was
// generated in this call. The password file is intentionally human-readable,
// but restricted to the service account (0600) and excluded from source ZIPs.
func loadOrCreateAdminPassword(dataDir string) (password string, created bool, err error) {
	if err = os.MkdirAll(dataDir, 0700); err != nil {
		return "", false, err
	}
	path := filepath.Join(dataDir, adminPasswordFileName)
	if raw, readErr := os.ReadFile(path); readErr == nil {
		password = strings.TrimSpace(string(raw))
		if len(password) < 12 || len(password) > 128 {
			return "", false, fmt.Errorf("管理员密码文件内容无效：%s（长度需为 12–128 个字符）", path)
		}
		_ = os.Chmod(path, 0600)
		return password, false, nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return "", false, readErr
	}

	random := make([]byte, 24)
	if _, err = rand.Read(random); err != nil {
		return "", false, fmt.Errorf("生成管理员初始密码失败：%w", err)
	}
	password = hex.EncodeToString(random)
	f, openErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if openErr != nil {
		if errors.Is(openErr, os.ErrExist) {
			raw, readErr := os.ReadFile(path)
			if readErr == nil {
				password = strings.TrimSpace(string(raw))
				if len(password) >= 12 && len(password) <= 128 {
					return password, false, nil
				}
			}
		}
		return "", false, openErr
	}
	if _, err = f.WriteString(password + "\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", false, err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", false, err
	}
	if err = f.Close(); err != nil {
		_ = os.Remove(path)
		return "", false, err
	}
	return password, true, nil
}

func (a *App) changeAdminPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
		ConfirmPassword string `json:"confirmPassword"`
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if len(in.NewPassword) < 12 || len(in.NewPassword) > 128 {
		fail(w, http.StatusBadRequest, "新密码长度必须为 12–128 个字符")
		return
	}
	if in.NewPassword != in.ConfirmPassword {
		fail(w, http.StatusBadRequest, "两次输入的新密码不一致")
		return
	}

	a.authMu.Lock()
	currentOK := subtle.ConstantTimeCompare([]byte(in.CurrentPassword), []byte(a.adminPassword)) == 1
	if !currentOK {
		a.authMu.Unlock()
		fail(w, http.StatusUnauthorized, "当前密码不正确")
		return
	}
	oldPassword := a.adminPassword
	if err := writePrivateAtomic(filepath.Join(a.dataDir, adminPasswordFileName), []byte(in.NewPassword+"\n")); err != nil {
		a.authMu.Unlock()
		fail(w, http.StatusInternalServerError, "保存新密码失败："+err.Error())
		return
	}
	newKey := make([]byte, 32)
	if _, err := rand.Read(newKey); err != nil {
		_ = writePrivateAtomic(filepath.Join(a.dataDir, adminPasswordFileName), []byte(oldPassword+"\n"))
		a.authMu.Unlock()
		fail(w, http.StatusInternalServerError, "更新登录会话密钥失败")
		return
	}
	if err := writePrivateAtomic(filepath.Join(a.dataDir, "gohome.session.key"), newKey); err != nil {
		_ = writePrivateAtomic(filepath.Join(a.dataDir, adminPasswordFileName), []byte(oldPassword+"\n"))
		a.authMu.Unlock()
		fail(w, http.StatusInternalServerError, "保存会话密钥失败："+err.Error())
		return
	}
	a.adminPassword = in.NewPassword
	a.authKey = newKey
	a.authMu.Unlock()

	// Password change invalidates every old browser session. Issue a fresh
	// cookie only to the browser that performed the change.
	a.setSessionCookie(w, r)
	a.store.addLog("success", "管理员密码已修改，其他浏览器的旧登录会话已失效")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "密码已更新"})
}

func writePrivateAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gohome-secret-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}
