package app

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	devicefeature "gohome/internal/features/devices"
	logfeature "gohome/internal/features/logging"
)

// GoHome is a lightweight home-device management service with real provider login flows.

//go:embed web/*
var webFiles embed.FS

// version is overridden by the Makefile for release builds.
var version = "0.2.11"

type DeviceChannel struct {
	Outlet     int    `json:"outlet"`
	Name       string `json:"name,omitempty"`
	Power      *bool  `json:"power,omitempty"`
	CanControl bool   `json:"canControl"`
}

type DeviceAttribute struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
}

// DeviceControl is a capability discovered from the device's MIoT specification.
// It is persisted with the device so the UI and remote API use the exact same SIID/PIID/AIID.
type DeviceControl struct {
	ID          string                `json:"id"`
	Kind        string                `json:"kind"` // property or action
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	SIID        int                   `json:"siid"`
	PIID        int                   `json:"piid,omitempty"`
	AIID        int                   `json:"aiid,omitempty"`
	Format      string                `json:"format,omitempty"`
	Value       any                   `json:"value,omitempty"`
	Options     []DeviceControlOption `json:"options,omitempty"`
	Inputs      []DeviceControlInput  `json:"inputs,omitempty"`
}

type DeviceControlOption struct {
	Value any    `json:"value"`
	Label string `json:"label"`
}

type DeviceControlInput struct {
	Name    string                `json:"name"`
	Format  string                `json:"format,omitempty"`
	Options []DeviceControlOption `json:"options,omitempty"`
}

type EnergyMetrics struct {
	Supported  bool      `json:"supported"`
	PowerW     *float64  `json:"powerW,omitempty"`
	VoltageV   *float64  `json:"voltageV,omitempty"`
	CurrentA   *float64  `json:"currentA,omitempty"`
	TodayKWh   *float64  `json:"todayKWh,omitempty"`
	WeekKWh    *float64  `json:"weekKWh,omitempty"`
	MonthKWh   *float64  `json:"monthKWh,omitempty"`
	YearKWh    *float64  `json:"yearKWh,omitempty"`
	TotalKWh   *float64  `json:"totalKWh,omitempty"`
	HistoryKWh *float64  `json:"historyKWh,omitempty"`
	Source     string    `json:"source,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt,omitempty"`
}

type Device struct {
	ID            string            `json:"id"`
	AccountID     string            `json:"accountId,omitempty"`
	RemoteID      string            `json:"remoteId,omitempty"`
	Name          string            `json:"name"`
	Brand         string            `json:"brand"`
	Model         string            `json:"model"`
	Room          string            `json:"room"`
	Category      string            `json:"category"`
	Protocol      string            `json:"protocol"`
	Online        bool              `json:"online"`
	Power         *bool             `json:"power,omitempty"`
	Channels      []DeviceChannel   `json:"channels,omitempty"`
	CanControl    bool              `json:"canControl"`
	Controls      []DeviceControl   `json:"controls,omitempty"`
	Energy        *EnergyMetrics    `json:"energy,omitempty"`
	Attributes    []DeviceAttribute `json:"attributes,omitempty"`
	Value         string            `json:"value,omitempty"`
	LastSeen      time.Time         `json:"lastSeen"`
	Demo          bool              `json:"demo"`
	ProviderGroup string            `json:"providerGroup,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
}

type Account struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	Label     string    `json:"label"`
	Region    string    `json:"region,omitempty"`
	Status    string    `json:"status"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"createdAt"`
}

type Discovery struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Service   string    `json:"service"`
	Host      string    `json:"host,omitempty"`
	Address   string    `json:"address,omitempty"`
	Port      int       `json:"port,omitempty"`
	Interface string    `json:"interface,omitempty"`
	FoundAt   time.Time `json:"foundAt"`
}

type LogEntry struct {
	ID      string    `json:"id"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

type diskState struct {
	Devices    []Device    `json:"devices"`
	Accounts   []Account   `json:"accounts"`
	Discovery  []Discovery `json:"discovery"`
	Logs       []LogEntry  `json:"logs"`
	FirstRunAt time.Time   `json:"firstRunAt"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	data diskState
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func newStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "gohome.json")}
	b, err := os.ReadFile(s.path)
	if err == nil {
		if err := json.Unmarshal(b, &s.data); err != nil {
			return nil, fmt.Errorf("读取数据文件失败: %w", err)
		}
		if s.data.Devices == nil {
			s.data.Devices = []Device{}
		}
		if s.data.Accounts == nil {
			s.data.Accounts = []Account{}
		}
		if s.data.Discovery == nil {
			s.data.Discovery = []Discovery{}
		}
		if s.data.Logs == nil {
			s.data.Logs = []LogEntry{}
		}
		// Migration: remove the four fake demo devices from older releases.
		// Also enable the real eWeLink switch controls that earlier versions
		// imported as read-only despite having a valid switch state.
		filtered := make([]Device, 0, len(s.data.Devices))
		removedDemo := 0
		changed := false
		for _, d := range s.data.Devices {
			if d.Demo || d.Protocol == "demo" || strings.HasPrefix(d.ID, "demo-") {
				removedDemo++
				changed = true
				continue
			}
			if d.Protocol == "ewelink-cloud" && d.AccountID != "" && d.RemoteID != "" && (d.Power != nil || len(d.Channels) > 0) && !d.CanControl {
				d.CanControl = true
				changed = true
			}
			filtered = append(filtered, d)
		}
		if removedDemo > 0 {
			s.addLogLocked("info", fmt.Sprintf("升级清理：已移除 %d 台旧版演示设备", removedDemo))
		}
		if changed {
			s.data.Devices = filtered
			if err := s.saveLocked(); err != nil {
				return nil, err
			}
		}
		return s, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	now := time.Now()
	s.data = diskState{
		FirstRunAt: now,
		Devices:    []Device{},
		Accounts:   []Account{},
		Discovery:  []Discovery{},
		Logs:       []LogEntry{},
	}
	s.addLogLocked("info", "GoHome 初始化完成，等待真实品牌设备接入")
	return s, s.saveLocked()
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) addLogLocked(level, message string) {
	s.data.Logs = append([]LogEntry{{ID: newID(), Level: level, Message: message, At: time.Now()}}, s.data.Logs...)
	if len(s.data.Logs) > 100 {
		s.data.Logs = s.data.Logs[:100]
	}
}

func (s *Store) addLog(level, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addLogLocked(level, message)
	_ = s.saveLocked()
}

func cloneFloat(v *float64) *float64 {
	if v == nil {
		return nil
	}
	copyValue := *v
	return &copyValue
}

func (s *Store) snapshot() diskState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := s.data
	v.Devices = append([]Device{}, s.data.Devices...)
	for i := range v.Devices {
		if v.Devices[i].Power != nil {
			power := *v.Devices[i].Power
			v.Devices[i].Power = &power
		}
		if v.Devices[i].Energy != nil {
			energy := *v.Devices[i].Energy
			energy.PowerW = cloneFloat(energy.PowerW)
			energy.VoltageV = cloneFloat(energy.VoltageV)
			energy.CurrentA = cloneFloat(energy.CurrentA)
			energy.TodayKWh = cloneFloat(energy.TodayKWh)
			energy.WeekKWh = cloneFloat(energy.WeekKWh)
			energy.MonthKWh = cloneFloat(energy.MonthKWh)
			energy.YearKWh = cloneFloat(energy.YearKWh)
			energy.TotalKWh = cloneFloat(energy.TotalKWh)
			energy.HistoryKWh = cloneFloat(energy.HistoryKWh)
			v.Devices[i].Energy = &energy
		}
		v.Devices[i].Attributes = append([]DeviceAttribute{}, v.Devices[i].Attributes...)
		v.Devices[i].Controls = append([]DeviceControl{}, v.Devices[i].Controls...)
		for j := range v.Devices[i].Controls {
			control := &v.Devices[i].Controls[j]
			control.Options = append([]DeviceControlOption{}, control.Options...)
			control.Inputs = append([]DeviceControlInput{}, control.Inputs...)
			for k := range control.Inputs {
				control.Inputs[k].Options = append([]DeviceControlOption{}, control.Inputs[k].Options...)
			}
		}
		v.Devices[i].Channels = append([]DeviceChannel{}, v.Devices[i].Channels...)
		for j := range v.Devices[i].Channels {
			if v.Devices[i].Channels[j].Power != nil {
				power := *v.Devices[i].Channels[j].Power
				v.Devices[i].Channels[j].Power = &power
			}
		}
	}
	v.Accounts = append([]Account{}, s.data.Accounts...)
	v.Discovery = append([]Discovery{}, s.data.Discovery...)
	v.Logs = append([]LogEntry{}, s.data.Logs...)
	return v
}

func (s *Store) putDevice(d Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Devices {
		if s.data.Devices[i].ID == d.ID {
			s.data.Devices[i] = d
			s.addLogLocked("info", "设备已更新："+d.Name)
			return s.saveLocked()
		}
	}
	s.data.Devices = append(s.data.Devices, d)
	s.addLogLocked("success", "已添加设备："+d.Name)
	return s.saveLocked()
}

func (s *Store) deleteDevice(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, d := range s.data.Devices {
		if d.ID == id {
			s.data.Devices = append(s.data.Devices[:i], s.data.Devices[i+1:]...)
			s.addLogLocked("info", "已删除设备："+d.Name)
			return s.saveLocked()
		}
	}
	return os.ErrNotExist
}

func (s *Store) commandDevice(id, command string) (Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Devices {
		d := &s.data.Devices[i]
		if d.ID != id {
			continue
		}
		if d.Protocol != "demo" || !d.Demo {
			return Device{}, errors.New("该设备的真实通信驱动尚未接入；当前版本不会伪装成控制成功")
		}
		if d.Power == nil {
			return Device{}, errors.New("该设备没有开关能力")
		}
		v := *d.Power
		switch command {
		case "turn_on":
			v = true
		case "turn_off":
			v = false
		case "toggle":
			v = !v
		default:
			return Device{}, errors.New("不支持的命令")
		}
		d.Power = &v
		d.LastSeen = time.Now()
		d.Online = true
		s.addLogLocked("success", fmt.Sprintf("演示设备 %s 已%s", d.Name, map[bool]string{true: "开启", false: "关闭"}[v]))
		if err := s.saveLocked(); err != nil {
			return Device{}, err
		}
		return *d, nil
	}
	return Device{}, os.ErrNotExist
}

func (s *Store) addAccount(a Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Accounts = append(s.data.Accounts, a)
	s.addLogLocked("success", "品牌账号已添加："+a.Label)
	return s.saveLocked()
}

func (s *Store) deleteAccount(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, a := range s.data.Accounts {
		if a.ID == id {
			s.data.Accounts = append(s.data.Accounts[:i], s.data.Accounts[i+1:]...)
			s.addLogLocked("info", "已删除接入记录："+a.Label)
			return s.saveLocked()
		}
	}
	return os.ErrNotExist
}

func (s *Store) setDiscovery(items []Discovery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Discovery = items
	if len(items) == 0 {
		s.addLogLocked("info", "局域网 mDNS 扫描完成：暂未发现服务")
	} else {
		s.addLogLocked("success", fmt.Sprintf("局域网 mDNS 扫描完成：发现 %d 项服务", len(items)))
	}
	return s.saveLocked()
}

type App struct {
	store        *Store
	mux          *http.ServeMux
	user         string
	pass         string
	integrations *integrationManager
}

func newApp(s *Store) *App {
	v, err := newVault(filepath.Dir(s.path))
	if err != nil {
		log.Fatalf("初始化凭证加密库失败: %v", err)
	}
	a := &App{store: s, mux: http.NewServeMux(), user: os.Getenv("GOHOME_USER"), pass: os.Getenv("GOHOME_PASSWORD")}
	a.integrations = newIntegrationManager(s, v)
	a.routes()
	return a
}

func (a *App) routes() {
	a.mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "app": "GoHome", "version": version, "time": time.Now()})
	})
	a.mux.HandleFunc("GET /api/overview", a.overview)
	a.mux.HandleFunc("GET /api/devices", a.listDevices)
	a.mux.HandleFunc("GET /api/v1/devices", a.listDevices)
	a.mux.HandleFunc("GET /api/v1/devices/{id}", a.getDevice)
	a.mux.HandleFunc("GET /api/v1/openapi.json", a.apiDocs)
	a.mux.HandleFunc("POST /api/devices", a.createDevice)
	a.mux.HandleFunc("GET /api/devices/{id}", a.getDevice)
	a.mux.HandleFunc("PATCH /api/devices/{id}", a.updateDevice)
	a.mux.HandleFunc("DELETE /api/devices/{id}", a.removeDevice)
	a.mux.HandleFunc("POST /api/devices/{id}/command", a.commandDevice)
	a.mux.HandleFunc("POST /api/v1/devices/{id}/command", a.commandDevice)
	a.mux.HandleFunc("POST /api/v1/devices/{id}/property", a.setMIoTProperty)
	a.mux.HandleFunc("POST /api/v1/devices/{id}/action", a.callMIoTAction)
	a.mux.HandleFunc("GET /api/accounts", a.listAccounts)
	a.mux.HandleFunc("POST /api/accounts", a.createAccount) // 旧版占位接口已禁用
	a.mux.HandleFunc("DELETE /api/accounts/{id}", a.removeAccount)
	a.mux.HandleFunc("POST /api/integrations/xiaomi/login", a.xiaomiLogin)
	a.mux.HandleFunc("POST /api/integrations/xiaomi/captcha", a.xiaomiCaptcha)
	a.mux.HandleFunc("POST /api/integrations/xiaomi/verify/send", a.xiaomiVerifySend)
	a.mux.HandleFunc("POST /api/integrations/xiaomi/verify", a.xiaomiVerify)
	a.mux.HandleFunc("POST /api/integrations/ewelink/login", a.ewelinkLogin)
	a.mux.HandleFunc("POST /api/accounts/{id}/sync", a.syncAccount)
	a.mux.HandleFunc("GET /api/discovery", a.listDiscovery)
	a.mux.HandleFunc("POST /api/discovery/scan", a.scanDiscovery)
	a.mux.HandleFunc("GET /api/logs", a.listLogs)
	static, _ := fs.Sub(webFiles, "web")
	a.mux.Handle("GET /", http.FileServer(http.FS(static)))
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if a.user != "" || a.pass != "" {
		u, p, ok := r.BasicAuth()
		if !ok || u != a.user || p != a.pass {
			w.Header().Set("WWW-Authenticate", `Basic realm="GoHome"`)
			http.Error(w, "需要登录", http.StatusUnauthorized)
			return
		}
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	a.mux.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// sanitizeIntegrationLog removes credentials and one-time Xiaomi context values
// before an error is persisted to the local activity log.
func sanitizeIntegrationLog(message string) string {
	return truncate(logfeature.Redact(message), 1200)
}

func (a *App) logIntegrationError(provider, stage string, err error) {
	if err == nil {
		return
	}
	a.store.addLog("error", fmt.Sprintf("[%s] %s失败：%s", provider, stage, sanitizeIntegrationLog(err.Error())))
}

func (a *App) logIntegrationResult(provider, stage string, result map[string]any) {
	if result == nil {
		return
	}
	message := firstNonEmpty(stringValue(result["warning"]), "")
	if message == "" && result["verifyPending"] == true {
		message = stringValue(result["message"])
	}
	if message != "" {
		a.store.addLog("warn", fmt.Sprintf("[%s] %s：%s", provider, stage, sanitizeIntegrationLog(message)))
	}
}

func (a *App) apiDocs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":           "GoHome API",
		"version":        "1.0",
		"authentication": "HTTP Basic Auth when GOHOME_USER and GOHOME_PASSWORD are configured",
		"endpoints": []map[string]any{
			{"method": "GET", "path": "/api/v1/devices", "description": "List synchronized devices and discovered controls"},
			{"method": "GET", "path": "/api/v1/devices/{id}", "description": "Get one device, including writable MIoT properties and callable actions"},
			{"method": "POST", "path": "/api/v1/devices/{id}/command", "body": map[string]any{"command": "turn_on | turn_off | toggle", "channel": "optional outlet number for multi-relay devices"}, "description": "Control a supported switch"},
			{"method": "POST", "path": "/api/v1/devices/{id}/property", "body": map[string]any{"siid": 2, "piid": 1, "value": true}, "description": "Set a writable MIoT property discovered from the model specification"},
			{"method": "POST", "path": "/api/v1/devices/{id}/action", "body": map[string]any{"siid": 4, "aiid": 1, "params": []any{}}, "description": "Invoke a MIoT action discovered from the model specification"},
			{"method": "GET", "path": "/api/health", "description": "Health check"},
		},
	})
}

func (a *App) overview(w http.ResponseWriter, r *http.Request) {
	d := a.store.snapshot()
	online, on := 0, 0
	for _, x := range d.Devices {
		if x.Online {
			online++
		}
		if len(x.Channels) > 0 {
			for _, channel := range x.Channels {
				if channel.Power != nil && *channel.Power {
					on++
				}
			}
		} else if x.Power != nil && *x.Power {
			on++
		}
	}
	writeJSON(w, 200, map[string]any{"deviceCount": len(d.Devices), "onlineCount": online, "onCount": on, "accountCount": len(d.Accounts), "discoveryCount": len(d.Discovery), "devices": d.Devices[:min(5, len(d.Devices))], "logs": d.Logs[:min(5, len(d.Logs))]})
}
func (a *App) listDevices(w http.ResponseWriter, r *http.Request) {
	d := a.store.snapshot().Devices
	for i := range d {
		d[i].ProviderGroup = devicefeature.BrandGroup(d[i].Protocol, d[i].Brand)
	}
	sort.Slice(d, func(i, j int) bool { return d[i].CreatedAt.Before(d[j].CreatedAt) })
	writeJSON(w, 200, d)
}
func (a *App) getDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	for _, d := range a.store.snapshot().Devices {
		if d.ID == id {
			writeJSON(w, 200, d)
			return
		}
	}
	fail(w, 404, "设备不存在")
}
func (a *App) createDevice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Brand    string `json:"brand"`
		Model    string `json:"model"`
		Room     string `json:"room"`
		Category string `json:"category"`
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, "请求格式错误")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		fail(w, 400, "设备名称不能为空")
		return
	}
	if in.Brand == "" {
		in.Brand = "其他"
	}
	if in.Room == "" {
		in.Room = "未分区"
	}
	if in.Category == "" {
		in.Category = "其他"
	}
	now := time.Now()
	d := Device{ID: newID(), Name: in.Name, Brand: in.Brand, Model: in.Model, Room: in.Room, Category: in.Category, Protocol: "manual", Online: false, LastSeen: now, Demo: false, CreatedAt: now}
	if err := a.store.putDevice(d); err != nil {
		fail(w, 500, "保存失败："+err.Error())
		return
	}
	writeJSON(w, 201, d)
}
func (a *App) updateDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in struct {
		Name string `json:"name"`
		Room string `json:"room"`
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, "请求格式错误")
		return
	}
	s := a.store.snapshot()
	for _, d := range s.Devices {
		if d.ID == id {
			if strings.TrimSpace(in.Name) != "" {
				d.Name = strings.TrimSpace(in.Name)
			}
			if strings.TrimSpace(in.Room) != "" {
				d.Room = strings.TrimSpace(in.Room)
			}
			if err := a.store.putDevice(d); err != nil {
				fail(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, d)
			return
		}
	}
	fail(w, 404, "设备不存在")
}
func (a *App) removeDevice(w http.ResponseWriter, r *http.Request) {
	if err := a.store.deleteDevice(r.PathValue("id")); err != nil {
		fail(w, 404, "设备不存在")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) commandDevice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Command string `json:"command"`
		Channel *int   `json:"channel,omitempty"`
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, "请求格式错误")
		return
	}
	id := r.PathValue("id")
	var device *Device
	for _, d := range a.store.snapshot().Devices {
		if d.ID == id {
			copy := d
			device = &copy
			break
		}
	}
	if device == nil {
		fail(w, 404, "设备不存在")
		return
	}
	if device.Protocol == "xiaomi-cloud" && device.AccountID != "" {
		if !device.CanControl {
			fail(w, http.StatusNotImplemented, "该小米设备没有发现可写属性或可调用动作（原有开关属性：没有识别到可写开关属性）；请重新同步 MIoT 规格")
			return
		}
		if in.Channel != nil {
			fail(w, http.StatusBadRequest, "小米 MIoT 设备不支持 eWeLink 通道参数")
			return
		}
		var secret providerSecret
		if err := a.integrations.vault.Get(device.AccountID, &secret); err != nil {
			fail(w, http.StatusConflict, err.Error())
			return
		}
		command := in.Command
		if command == "toggle" {
			if device.Power == nil {
				fail(w, http.StatusConflict, "小米设备当前开关状态未知，请使用明确的 turn_on 或 turn_off 命令")
				return
			}
			if *device.Power {
				command = "turn_off"
			} else {
				command = "turn_on"
			}
		}
		if command != "turn_on" && command != "turn_off" {
			fail(w, http.StatusBadRequest, "小米设备仅支持 turn_on、turn_off、toggle")
			return
		}
		var controlErr error
		if powerControl, ok := findXiaomiPowerControl(*device); ok {
			controlErr = xiaomiSetMIoTProperty(r.Context(), secret, device.RemoteID, powerControl.SIID, powerControl.PIID, command == "turn_on")
		} else if len(device.Controls) == 0 {
			// Compatibility path for device records created before capabilities were persisted.
			controlErr = xiaomiCommandDevice(r.Context(), secret, device.RemoteID, device.Model, command)
		} else {
			controlErr = errors.New("该小米设备没有可写的电源开关；请使用设备已发现的属性或动作控制 API")
		}
		if controlErr != nil {
			a.store.addLog("error", "[小米] 设备控制失败："+sanitizeIntegrationLog(controlErr.Error()))
			fail(w, http.StatusBadGateway, controlErr.Error())
			return
		}
		value := command == "turn_on"
		device.Power = &value
		device.Online = true
		device.LastSeen = time.Now()
		if err := a.store.putDevice(*device); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, device)
		return
	}
	if device.Protocol == "ewelink-cloud" && device.AccountID != "" {
		if !device.CanControl {
			fail(w, http.StatusNotImplemented, "该设备尚未识别出可控制的开关属性；请重新同步设备，或检查设备型号是否支持 eWeLink 开关控制")
			return
		}
		var secret providerSecret
		if err := a.integrations.vault.Get(device.AccountID, &secret); err != nil {
			fail(w, 409, err.Error())
			return
		}
		var err error
		secret, err = a.integrations.ensureEwelinkToken(r.Context(), device.AccountID, secret)
		if err != nil {
			fail(w, 502, err.Error())
			return
		}
		command := in.Command
		channel := 0
		current := device.Power
		multiChannel := len(device.Channels) > 0
		if len(device.Channels) > 0 {
			if in.Channel != nil {
				channel = *in.Channel
			} else if len(device.Channels) > 1 {
				fail(w, 400, "该设备有多个独立开关，请指定 channel（outlet 编号）")
				return
			} else {
				channel = device.Channels[0].Outlet
			}
			found := false
			for _, ch := range device.Channels {
				if ch.Outlet == channel {
					current = ch.Power
					found = true
					break
				}
			}
			if !found {
				fail(w, 404, "指定的开关通道不存在")
				return
			}
		} else if in.Channel != nil && *in.Channel != 0 {
			fail(w, 400, "单路设备的 channel 只能是 0")
			return
		}
		if command == "toggle" {
			if current == nil {
				fail(w, 409, "设备当前开关状态未知，无法安全执行 toggle；请先同步设备状态")
				return
			}
			if *current {
				command = "turn_off"
			} else {
				command = "turn_on"
			}
		}
		if command != "turn_on" && command != "turn_off" {
			fail(w, 400, "不支持的命令")
			return
		}
		if err := ewelinkCommandChannel(r.Context(), secret, device.RemoteID, command, channel, multiChannel); err != nil {
			fail(w, 502, err.Error())
			return
		}
		v := command == "turn_on"
		if len(device.Channels) > 0 {
			for i := range device.Channels {
				if device.Channels[i].Outlet == channel {
					device.Channels[i].Power = &v
				}
			}
		}
		if channel == 0 || len(device.Channels) == 0 {
			device.Power = &v
		}
		device.Online = true
		device.LastSeen = time.Now()
		if err := a.store.putDevice(*device); err != nil {
			fail(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, device)
		return
	}
	d, err := a.store.commandDevice(id, in.Command)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fail(w, 404, err.Error())
		} else {
			fail(w, 409, err.Error())
		}
		return
	}
	writeJSON(w, 200, d)
}
func findXiaomiPowerControl(device Device) (DeviceControl, bool) {
	for _, control := range device.Controls {
		if control.Kind == "property" && control.Format == "bool" && control.SIID > 0 && control.PIID > 0 && control.Name == "电源开关" {
			return control, true
		}
	}
	return DeviceControl{}, false
}

func (a *App) setMIoTProperty(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SIID  int             `json:"siid"`
		PIID  int             `json:"piid"`
		Value json.RawMessage `json:"value"`
	}
	if err := readJSON(r, &in); err != nil || in.SIID <= 0 || in.PIID <= 0 || len(in.Value) == 0 {
		fail(w, http.StatusBadRequest, "请求需要有效的 siid、piid 和 value")
		return
	}
	var device *Device
	for _, candidate := range a.store.snapshot().Devices {
		if candidate.ID == r.PathValue("id") {
			copy := candidate
			device = &copy
			break
		}
	}
	if device == nil {
		fail(w, http.StatusNotFound, "设备不存在")
		return
	}
	if device.Protocol != "xiaomi-cloud" || device.AccountID == "" {
		fail(w, http.StatusNotImplemented, "当前属性写入 API 仅支持已同步 MIoT 规格的小米设备")
		return
	}
	var control *DeviceControl
	for index := range device.Controls {
		item := &device.Controls[index]
		if item.Kind == "property" && item.SIID == in.SIID && item.PIID == in.PIID {
			control = item
			break
		}
	}
	if control == nil {
		fail(w, http.StatusForbidden, "该属性不是设备规格中已发现的可写属性；请先重新同步设备")
		return
	}
	var value any
	if err := json.Unmarshal(in.Value, &value); err != nil {
		fail(w, http.StatusBadRequest, "value 必须是有效 JSON 值")
		return
	}
	if err := validateMIoTValue(control.Format, value, control.Options); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	var secret providerSecret
	if err := a.integrations.vault.Get(device.AccountID, &secret); err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	if err := xiaomiSetMIoTProperty(r.Context(), secret, device.RemoteID, in.SIID, in.PIID, value); err != nil {
		a.store.addLog("error", "[小米] MIoT 属性写入失败："+sanitizeIntegrationLog(err.Error()))
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	control.Value = value
	if control.Name == "电源开关" {
		if enabled, ok := value.(bool); ok {
			device.Power = &enabled
		}
	}
	device.LastSeen = time.Now()
	if err := a.store.putDevice(*device); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.store.addLog("success", fmt.Sprintf("[小米] %s 属性已更新（SIID=%d PIID=%d）", device.Name, in.SIID, in.PIID))
	writeJSON(w, http.StatusOK, device)
}

func (a *App) callMIoTAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SIID   int   `json:"siid"`
		AIID   int   `json:"aiid"`
		Params []any `json:"params"`
	}
	if err := readJSON(r, &in); err != nil || in.SIID <= 0 || in.AIID <= 0 {
		fail(w, http.StatusBadRequest, "请求需要有效的 siid、aiid 和 params 数组")
		return
	}
	var device *Device
	for _, candidate := range a.store.snapshot().Devices {
		if candidate.ID == r.PathValue("id") {
			copy := candidate
			device = &copy
			break
		}
	}
	if device == nil {
		fail(w, http.StatusNotFound, "设备不存在")
		return
	}
	if device.Protocol != "xiaomi-cloud" || device.AccountID == "" {
		fail(w, http.StatusNotImplemented, "MIoT 动作 API 仅支持已同步规格的小米设备")
		return
	}
	var control *DeviceControl
	for index := range device.Controls {
		item := &device.Controls[index]
		if item.Kind == "action" && item.SIID == in.SIID && item.AIID == in.AIID {
			control = item
			break
		}
	}
	if control == nil {
		fail(w, http.StatusForbidden, "该动作未在设备 MIoT 规格中发现；请先重新同步设备")
		return
	}
	if len(in.Params) != len(control.Inputs) {
		fail(w, http.StatusBadRequest, fmt.Sprintf("动作 %s 需要 %d 个参数，收到 %d 个", control.Name, len(control.Inputs), len(in.Params)))
		return
	}
	for index, value := range in.Params {
		if err := validateMIoTValue(control.Inputs[index].Format, value, control.Inputs[index].Options); err != nil {
			fail(w, http.StatusBadRequest, fmt.Sprintf("动作参数 %d（%s）：%s", index+1, control.Inputs[index].Name, err.Error()))
			return
		}
	}
	var secret providerSecret
	if err := a.integrations.vault.Get(device.AccountID, &secret); err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	if err := xiaomiCallMIoTAction(r.Context(), secret, device.RemoteID, in.SIID, in.AIID, in.Params); err != nil {
		a.store.addLog("error", "[小米] MIoT 动作执行失败："+sanitizeIntegrationLog(err.Error()))
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	device.LastSeen = time.Now()
	if err := a.store.putDevice(*device); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.store.addLog("success", fmt.Sprintf("[小米] %s 已执行动作：%s（SIID=%d AIID=%d）", device.Name, control.Name, in.SIID, in.AIID))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "device": device, "action": control.Name})
}

func validateMIoTValue(format string, value any, options []DeviceControlOption) error {
	if len(options) > 0 {
		for _, option := range options {
			if fmt.Sprint(option.Value) == fmt.Sprint(value) {
				return nil
			}
		}
		return errors.New("value 不在设备规格允许的选项中")
	}
	format = strings.ToLower(strings.TrimSpace(format))
	switch format {
	case "bool", "boolean":
		if _, ok := value.(bool); !ok {
			return errors.New("该属性需要 true 或 false")
		}
	case "uint8", "uint16", "uint32", "int8", "int16", "int32", "int64", "uint64", "float", "double":
		n, ok := value.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return errors.New("该属性需要有效数字")
		}
		if strings.HasPrefix(format, "u") && (n < 0 || math.Trunc(n) != n) {
			return errors.New("该属性需要非负整数")
		}
		if !strings.Contains(format, "float") && !strings.Contains(format, "double") && math.Trunc(n) != n {
			return errors.New("该属性需要整数")
		}
	case "string":
		if _, ok := value.(string); !ok {
			return errors.New("该属性需要字符串")
		}
	default:
		switch value.(type) {
		case bool, float64, string:
		default:
			return errors.New("只允许布尔值、数字或字符串作为 MIoT 参数")
		}
	}
	return nil
}

func (a *App) listAccounts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.store.snapshot().Accounts)
}
func (a *App) createAccount(w http.ResponseWriter, r *http.Request) {
	fail(w, http.StatusGone, "旧版‘记录账号’接口已停用。请到品牌接入页面使用小米登录或 eWeLink 登录。")
}

func (a *App) xiaomiLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Region   string `json:"region"`
		Label    string `json:"label"`
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, "请求格式错误")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
	defer cancel()
	result, err := a.integrations.xiaomiLogin(ctx, in.Username, in.Password, in.Region, in.Label)
	if err != nil {
		a.logIntegrationError("小米", "账号登录", err)
		fail(w, 502, err.Error())
		return
	}
	a.logIntegrationResult("小米", "账号登录/设备同步", result)
	writeJSON(w, 200, result)
}
func (a *App) xiaomiCaptcha(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ChallengeID string `json:"challengeId"`
		Code        string `json:"code"`
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, "请求格式错误")
		return
	}
	if strings.TrimSpace(in.Code) == "" {
		fail(w, 400, "请输入验证码")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
	defer cancel()
	result, err := a.integrations.xiaomiCaptcha(ctx, in.ChallengeID, in.Code)
	if err != nil {
		a.logIntegrationError("小米", "图形验证码", err)
		fail(w, 502, err.Error())
		return
	}
	a.logIntegrationResult("小米", "图形验证码/设备同步", result)
	writeJSON(w, 200, result)
}
func (a *App) xiaomiVerifySend(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ChallengeID string `json:"challengeId"`
		Captcha     string `json:"captcha"`
		Resend      bool   `json:"resend"`
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, "请求格式错误")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	result, err := a.integrations.xiaomiSendVerifyCode(ctx, in.ChallengeID, in.Captcha, in.Resend)
	if err != nil {
		a.logIntegrationError("小米", "发送安全验证码", err)
		fail(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, result)
}
func (a *App) xiaomiVerify(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ChallengeID string `json:"challengeId"`
		Ticket      string `json:"ticket"`
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, "请求格式错误")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 55*time.Second)
	defer cancel()
	result, err := a.integrations.xiaomiVerifyTicket(ctx, in.ChallengeID, in.Ticket)
	if err != nil {
		a.logIntegrationError("小米", "短信/邮箱验证", err)
		fail(w, 502, err.Error())
		return
	}
	a.logIntegrationResult("小米", "短信/邮箱验证/设备同步", result)
	writeJSON(w, 200, result)
}

func (a *App) ewelinkLogin(w http.ResponseWriter, r *http.Request) {
	var in ewelinkLoginInput
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, "请求格式错误")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
	defer cancel()
	result, err := a.integrations.ewelinkLogin(ctx, in)
	if err != nil {
		a.logIntegrationError("eWeLink", "账号登录", err)
		fail(w, 502, err.Error())
		return
	}
	a.logIntegrationResult("eWeLink", "账号登录/设备同步", result)
	writeJSON(w, 200, result)
}
func (a *App) syncAccount(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	count, err := a.integrations.syncAccount(ctx, r.PathValue("id"))
	if err != nil {
		a.logIntegrationError("品牌接入", "设备同步", err)
		fail(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "deviceCount": count})
}

func (a *App) storeAdd(ac Account) error { return a.store.addAccount(ac) }
func (a *App) removeAccount(w http.ResponseWriter, r *http.Request) {
	if err := a.integrations.removeAccount(r.PathValue("id")); err != nil {
		fail(w, 404, "接入账号不存在")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) listDiscovery(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.store.snapshot().Discovery)
}
func (a *App) scanDiscovery(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2200*time.Millisecond)
	defer cancel()
	items, err := scanMDNS(ctx)
	if err != nil {
		a.store.addLog("warn", "mDNS 扫描部分失败："+err.Error())
		fail(w, 500, "扫描失败："+err.Error())
		return
	}
	if err := a.store.setDiscovery(items); err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "count": len(items), "message": "扫描完成。mDNS 只能发现主动广播服务的设备，未发现不代表设备不存在。"})
}
func (a *App) listLogs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.store.snapshot().Logs)
}

func Run() {
	dataDir := os.Getenv("GOHOME_DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}
	store, err := newStore(dataDir)
	if err != nil {
		log.Fatalf("初始化存储失败: %v", err)
	}
	addr := os.Getenv("GOHOME_ADDR")
	if addr == "" {
		addr = ":17880"
	}
	app := newApp(store)
	srv := &http.Server{Addr: addr, Handler: app, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("GoHome v%s 启动：%s", version, addr)
	if app.user != "" || app.pass != "" {
		log.Printf("HTTP Basic Auth 已启用，用户名：%s", app.user)
	} else {
		log.Printf("警告：未设置 GOHOME_USER/GOHOME_PASSWORD；请勿将服务暴露到公网")
	}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
