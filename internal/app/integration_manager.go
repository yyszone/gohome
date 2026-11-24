package app

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// vault encrypts provider credentials and refresh tokens with AES-256-GCM.
// The key is stored beside the ciphertext with filesystem mode 0600.
type vault struct {
	mu   sync.Mutex
	key  []byte
	path string
	data map[string]json.RawMessage
}

func newVault(dir string) (*vault, error) {
	if err := ensureDir(dir); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(dir, "gohome.vault.key")
	key, err := readOrCreateKey(keyPath)
	if err != nil {
		return nil, err
	}
	v := &vault{key: key, path: filepath.Join(dir, "gohome.vault.json"), data: map[string]json.RawMessage{}}
	b, err := readFile(v.path)
	if err == nil && len(b) > 0 {
		var disk struct {
			Nonce      string `json:"nonce"`
			Ciphertext string `json:"ciphertext"`
		}
		if err := json.Unmarshal(b, &disk); err != nil {
			return nil, fmt.Errorf("读取凭证库失败: %w", err)
		}
		nonce, e := base64.StdEncoding.DecodeString(disk.Nonce)
		if e != nil {
			return nil, e
		}
		ct, e := base64.StdEncoding.DecodeString(disk.Ciphertext)
		if e != nil {
			return nil, e
		}
		plain, e := v.open(nonce, ct)
		if e != nil {
			return nil, fmt.Errorf("解密凭证库失败；请检查 vault.key 是否与数据文件匹配: %w", e)
		}
		if len(plain) > 0 {
			if e = json.Unmarshal(plain, &v.data); e != nil {
				return nil, e
			}
		}
	} else if err != nil && !errors.Is(err, errFileNotExist()) {
		return nil, err
	}
	return v, nil
}

// Small wrappers keep file handling in main.go's stdlib-only project.
func ensureDir(dir string) error { return mkdirAll(dir) }

func readFile(p string) ([]byte, error) { return readFileOS(p) }

func readOrCreateKey(p string) ([]byte, error) {
	b, err := readFileOS(p)
	if err == nil {
		if len(b) != 32 {
			return nil, fmt.Errorf("凭证密钥长度错误")
		}
		return b, nil
	}
	if !isNotExist(err) {
		return nil, err
	}
	b = make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return nil, err
	}
	if err = writeFileOS(p, b, 0600); err != nil {
		return nil, err
	}
	return b, nil
}

func (v *vault) seal(plain []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return nil, nil, err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	n := make([]byte, g.NonceSize())
	if _, err = rand.Read(n); err != nil {
		return nil, nil, err
	}
	return n, g.Seal(nil, n, plain, nil), nil
}

func (v *vault) open(nonce, ct []byte) ([]byte, error) {
	block, e := aes.NewCipher(v.key)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	return g.Open(nil, nonce, ct, nil)
}

func (v *vault) saveLocked() error {
	plain, e := json.Marshal(v.data)
	if e != nil {
		return e
	}
	n, ct, e := v.seal(plain)
	if e != nil {
		return e
	}
	b, e := json.Marshal(struct {
		Nonce      string `json:"nonce"`
		Ciphertext string `json:"ciphertext"`
	}{base64.StdEncoding.EncodeToString(n), base64.StdEncoding.EncodeToString(ct)})
	if e != nil {
		return e
	}
	return writeFileAtomicOS(v.path, b, 0600)
}

func (v *vault) Put(id string, value any) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	b, e := json.Marshal(value)
	if e != nil {
		return e
	}
	v.data[id] = b
	return v.saveLocked()
}

func (v *vault) Get(id string, out any) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	b, ok := v.data[id]
	if !ok {
		return errors.New("账号凭证不存在，请重新登录")
	}
	return json.Unmarshal(b, out)
}

func (v *vault) Delete(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.data, id)
	return v.saveLocked()
}

type providerSecret struct {
	Provider     string            `json:"provider"`
	Region       string            `json:"region"`
	Username     string            `json:"username,omitempty"`
	UserID       string            `json:"userId,omitempty"`
	CUserID      string            `json:"cUserId,omitempty"`
	DeviceID     string            `json:"deviceId,omitempty"`
	ServiceToken string            `json:"serviceToken,omitempty"`
	SSecurity    string            `json:"ssecurity,omitempty"`
	AppID        string            `json:"appId,omitempty"`
	AppSecret    string            `json:"appSecret,omitempty"`
	Email        string            `json:"email,omitempty"`
	PhoneNumber  string            `json:"phoneNumber,omitempty"`
	CountryCode  string            `json:"countryCode,omitempty"`
	AccessToken  string            `json:"accessToken,omitempty"`
	RefreshToken string            `json:"refreshToken,omitempty"`
	UserAPIKey   string            `json:"userApiKey,omitempty"`
	DeviceKeys   map[string]string `json:"deviceKeys,omitempty"`
	PassToken    string            `json:"passToken,omitempty"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}

func newXiaomiIdentityPending(id, username, password, region string, client *http.Client, form url.Values, verifyURL string) *xiaomiPending {
	return &xiaomiPending{
		ID: id, Username: username, Password: password, Region: region,
		Client: client, Form: form, VerifyURL: verifyURL,
		DeviceID: xiaomiCookieValue(client, "deviceId"), CreatedAt: time.Now(),
	}
}

type integrationManager struct {
	store  *Store
	vault  *vault
	mu     sync.Mutex
	xiaomi map[string]*xiaomiPending
}

func newIntegrationManager(s *Store, v *vault) *integrationManager {
	return &integrationManager{store: s, vault: v, xiaomi: map[string]*xiaomiPending{}}
}

// normalizeXiaomiLocation accepts the common formats returned by Xiaomi:
// absolute HTTPS URL, protocol-relative URL, root-relative path, host/path
// without a scheme, and a once-URL-encoded absolute URL. It never changes a
// non-Xiaomi host into a trusted one.
func normalizeXiaomiLocation(raw, base string) (*url.URL, error) {
	raw = strings.TrimSpace(strings.Trim(raw, `"'`))
	raw = strings.ReplaceAll(raw, "&amp;", "&")
	if raw == "" {
		return nil, errors.New("小米返回了空跳转地址")
	}
	if !strings.Contains(raw, "://") && (strings.HasPrefix(strings.ToLower(raw), "https%3a%2f%2f") || strings.HasPrefix(strings.ToLower(raw), "http%3a%2f%2f")) {
		if decoded, err := url.QueryUnescape(raw); err == nil {
			raw = decoded
		}
	}
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	} else if !strings.Contains(raw, "://") && !strings.HasPrefix(raw, "/") {
		first := strings.SplitN(raw, "/", 2)[0]
		if strings.Contains(first, ".") {
			raw = "https://" + raw
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("解析小米跳转地址失败：%w", err)
	}
	if !u.IsAbs() {
		b, parseErr := url.Parse(base)
		if parseErr != nil {
			return nil, parseErr
		}
		u = b.ResolveReference(u)
	}
	return u, nil
}

func responseCookieValue(resp *http.Response, name string) string {
	if resp == nil {
		return ""
	}
	for _, c := range resp.Cookies() {
		if c.Name == name && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

// Follow the xiaomiio STS location exactly as the Xiaomi Miot Auto flow does.
// For sid=xiaomiio the returned location is already signed by Xiaomi; do not
// append clientSign (that variant is used by other service SIDs).
type xiaomiServiceSession struct {
	ServiceToken string
	UserID       string
	CUserID      string
	FinalURL     string
	StatusCode   int
}

func mustURL(s string) *url.URL { u, _ := url.Parse(s); return u }

func (m *integrationManager) deleteXiaomiPending(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p := m.xiaomi[id]; p != nil {
		p.Password = ""
		p.CaptchaImage = ""
		p.CaptchaIck = ""
		delete(m.xiaomi, id)
	}
}

func (m *integrationManager) saveXiaomiAccountAndSync(ctx context.Context, secret providerSecret, label string) (map[string]any, error) {
	id := newID()
	ac := Account{ID: id, Provider: "xiaomi", Label: firstNonEmpty(label, secret.Username), Region: secret.Region, Status: "connected", Note: "小米账号已授权，正在同步云端设备", CreatedAt: time.Now()}
	if err := m.vault.Put(id, secret); err != nil {
		return nil, fmt.Errorf("加密保存小米会话失败：%w", err)
	}
	if err := m.store.addAccount(ac); err != nil {
		_ = m.vault.Delete(id)
		return nil, err
	}
	devices, err := xiaomiFetchDevices(ctx, secret)
	if err != nil {
		m.setAccountStatus(id, "sync_error", "登录成功，但设备同步失败："+err.Error())
		account, _ := m.findAccount(id)
		return map[string]any{"account": account, "connected": true, "deviceCount": 0, "warning": "小米授权成功，但设备清单读取失败：" + err.Error()}, nil
	}
	m.importDevices(id, "xiaomi", devices)
	m.setAccountStatus(id, "connected", fmt.Sprintf("已授权 · 已同步 %d 台设备", len(devices)))
	account, _ := m.findAccount(id)
	return map[string]any{"account": account, "connected": true, "deviceCount": len(devices)}, nil
}

func normalizeEwelinkRegion(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "cn", "as", "us", "eu":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return ""
	}
}

func parseEwelinkDeviceAttributes(params map[string]any) []DeviceAttribute {
	if len(params) == 0 {
		return nil
	}
	attrs := make([]DeviceAttribute, 0, 6)
	add := func(key, label, unit string, value *float64) {
		if value != nil {
			attrs = append(attrs, DeviceAttribute{Key: key, Label: label, Value: strconv.FormatFloat(*value, 'f', -1, 64), Unit: unit})
		}
	}
	add("temperature", "温度", "°C", findMetric(params, "temperature", "temp", "currentTemperature", "current_temperature"))
	add("humidity", "湿度", "%", findMetric(params, "humidity", "relative_humidity"))
	add("battery", "电量", "%", findMetric(params, "battery", "batteryLevel", "battery_level"))
	add("illuminance", "光照", "lx", findMetric(params, "illuminance", "lux"))
	for _, pair := range []struct {
		key, label string
		names      []string
	}{{"motion", "活动检测", []string{"motion", "motionState", "humanDetection"}}, {"contact", "门窗状态", []string{"contact", "doorState", "door"}}, {"alarm", "报警状态", []string{"alarm", "smokeAlarm", "waterLeak"}}} {
		for _, name := range pair.names {
			if value, ok := params[name]; ok {
				if b := parseSwitch(value); b != nil {
					val := "关闭"
					if *b {
						val = "开启"
					}
					attrs = append(attrs, DeviceAttribute{Key: pair.key, Label: pair.label, Value: val})
					break
				}
			}
		}
	}
	return attrs
}

func parseEwelinkChannels(params map[string]any) []DeviceChannel {
	items, ok := params["switches"].([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	channels := make([]DeviceChannel, 0, len(items))
	for index, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		outlet := intValue(m["outlet"], index)
		power := parseSwitch(m["switch"])
		if power == nil {
			continue
		}
		channels = append(channels, DeviceChannel{Outlet: outlet, Name: fmt.Sprintf("通道 %d", outlet+1), Power: power, CanControl: true})
	}
	return channels
}

func findExactMetric(params map[string]any, name string) *float64 {
	for key, value := range params {
		if strings.EqualFold(key, name) {
			return numericMetric(value)
		}
	}
	return nil
}

func parseEwelinkEnergyMetrics(params map[string]any, model string) *EnergyMetrics {
	if len(params) == 0 && !ewelinkModelHasMeter(model) {
		return nil
	}
	e := &EnergyMetrics{Source: "eWeLink", UpdatedAt: time.Now()}
	e.PowerW = findMetric(params, "power", "apower", "active_power", "electric_power", "power_w", "power_1")
	e.VoltageV = findMetric(params, "voltage", "voltage_v", "voltage_1")
	e.CurrentA = findMetric(params, "current", "current_a", "current_1")
	e.TodayKWh = findExactMetric(params, "dayKwh")
	if e.TodayKWh != nil {
		*e.TodayKWh *= 0.01
	} else {
		e.TodayKWh = findMetric(params, "energy_day", "energyday", "daily_energy", "today_energy", "today_kwh")
	}
	e.WeekKWh = findExactMetric(params, "weekKwh")
	if e.WeekKWh != nil {
		*e.WeekKWh *= 0.01
	} else {
		e.WeekKWh = findMetric(params, "energy_week", "energyweek", "weekly_energy", "week_kwh")
	}
	e.MonthKWh = findExactMetric(params, "monthKwh")
	if e.MonthKWh != nil {
		*e.MonthKWh *= 0.01
	} else {
		e.MonthKWh = findMetric(params, "energy_month", "energymonth", "monthly_energy", "month_kwh")
	}
	e.YearKWh = findExactMetric(params, "yearKwh")
	if e.YearKWh != nil {
		*e.YearKWh *= 0.01
	} else {
		e.YearKWh = findMetric(params, "energy_year", "energyyear", "yearly_energy", "year_kwh")
	}
	e.TotalKWh = findMetric(params, "total_energy", "energy_total", "total_kwh", "totalenergy", "total_consumption")
	e.HistoryKWh = findMetric(params, "energy", "energy_1", "energy_2", "energy_history")
	// Some firmwares report voltage in mV and current in mA.
	if e.VoltageV != nil && *e.VoltageV > 1000 {
		*e.VoltageV /= 1000
	}
	if e.CurrentA != nil && *e.CurrentA > 100 {
		*e.CurrentA /= 1000
	}
	// Do not guess that a generic `energy` field is lifetime energy. SonoffLAN
	// documents it as hourly historical cloud usage for several UIIDs.
	e.Supported = e.PowerW != nil || e.VoltageV != nil || e.CurrentA != nil || e.TodayKWh != nil || e.WeekKWh != nil || e.MonthKWh != nil || e.YearKWh != nil || e.TotalKWh != nil || e.HistoryKWh != nil || ewelinkModelHasMeter(model)
	if !e.Supported {
		return nil
	}
	return e
}

func (m *integrationManager) syncAccount(ctx context.Context, id string) (int, error) {
	_, ok := m.findAccount(id)
	if !ok {
		return 0, errors.New("账号不存在")
	}
	var secret providerSecret
	if e := m.vault.Get(id, &secret); e != nil {
		return 0, e
	}
	var devices []Device
	var err error
	if secret.Provider == "xiaomi" {
		devices, err = xiaomiFetchDevices(ctx, secret)
	} else if secret.Provider == "ewelink" {
		secret, err = m.ensureEwelinkToken(ctx, id, secret)
		if err == nil {
			var keys map[string]string
			devices, keys, err = ewelinkFetchDevicesAndKeys(ctx, secret)
			if err == nil {
				secret.DeviceKeys = keys
				err = m.vault.Put(id, secret)
			}
		}
	} else {
		return 0, errors.New("未知品牌")
	}
	if err != nil {
		m.setAccountStatus(id, "sync_error", "设备同步失败："+err.Error())
		return 0, err
	}
	m.importDevices(id, secret.Provider, devices)
	m.setAccountStatus(id, "connected", fmt.Sprintf("已授权 · 已同步 %d 台设备", len(devices)))
	return len(devices), nil
}

func (m *integrationManager) importDevices(accountID, provider string, items []Device) {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	seen := map[string]bool{}
	for i := range items {
		d := items[i]
		d.AccountID = accountID
		d.Demo = false
		d.LastSeen = time.Now()
		seen[d.ID] = true
		found := false
		for j := range m.store.data.Devices {
			if m.store.data.Devices[j].ID == d.ID {
				old := m.store.data.Devices[j]
				d.Room = firstNonEmpty(d.Room, old.Room)
				d.CreatedAt = old.CreatedAt
				if d.Energy == nil && old.Energy != nil {
					energy := *old.Energy
					d.Energy = &energy
				}
				if d.Power == nil && old.Power != nil {
					power := *old.Power
					d.Power = &power
				}
				if len(d.Controls) == 0 && len(old.Controls) > 0 {
					d.Controls = append([]DeviceControl{}, old.Controls...)
				}
				if !d.CanControl && old.CanControl {
					d.CanControl = true
				}
				if len(d.Attributes) == 0 && len(old.Attributes) > 0 {
					d.Attributes = append([]DeviceAttribute{}, old.Attributes...)
					if d.Value == "" {
						d.Value = old.Value
					}
				}
				if d.Category == "其他" && old.Category != "" && old.Category != "其他" {
					d.Category = old.Category
				}
				m.store.data.Devices[j] = d
				found = true
				break
			}
		}
		if !found {
			m.store.data.Devices = append(m.store.data.Devices, d)
		}
	}
	// Remove devices that were previously imported from this account but no longer appear remotely.
	kept := make([]Device, 0, len(m.store.data.Devices))
	for _, d := range m.store.data.Devices {
		if d.AccountID != accountID || seen[d.ID] {
			kept = append(kept, d)
		}
	}
	m.store.data.Devices = kept
	m.store.addLogLocked("success", fmt.Sprintf("品牌账号同步完成：%d 台设备", len(items)))
	_ = m.store.saveLocked()
}

func (m *integrationManager) setAccountStatus(id, status, note string) {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	for i := range m.store.data.Accounts {
		if m.store.data.Accounts[i].ID == id {
			m.store.data.Accounts[i].Status = status
			m.store.data.Accounts[i].Note = note
			_ = m.store.saveLocked()
			return
		}
	}
}

func (m *integrationManager) findAccount(id string) (Account, bool) {
	for _, a := range m.store.snapshot().Accounts {
		if a.ID == id {
			return a, true
		}
	}
	return Account{}, false
}

func (m *integrationManager) removeAccount(id string) error {
	if e := m.store.deleteAccount(id); e != nil {
		return e
	}
	_ = m.vault.Delete(id)
	m.store.mu.Lock()
	kept := make([]Device, 0, len(m.store.data.Devices))
	for _, d := range m.store.data.Devices {
		if d.AccountID != id {
			kept = append(kept, d)
		}
	}
	m.store.data.Devices = kept
	_ = m.store.saveLocked()
	m.store.mu.Unlock()
	return nil
}

func (m *integrationManager) ensureEwelinkToken(ctx context.Context, id string, s providerSecret) (providerSecret, error) {
	if time.Since(s.UpdatedAt) < 25*24*time.Hour {
		return s, nil
	}
	payload := map[string]string{"rt": s.RefreshToken}
	body, e := json.Marshal(payload)
	if e != nil {
		return s, e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, ewelinkHost(s.Region)+"/v2/user/refresh", bytes.NewReader(body))
	if e != nil {
		return s, e
	}
	setEwelinkHeaders(req, s.AppID, s.AppSecret, body, false)
	resp, e := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if e != nil {
		return s, e
	}
	b, e := readBody(resp)
	if e != nil {
		return s, e
	}
	var env struct {
		Error int    `json:"error"`
		Msg   string `json:"msg"`
		Data  struct {
			AT string `json:"at"`
			RT string `json:"rt"`
		} `json:"data"`
	}
	if e = json.Unmarshal(b, &env); e != nil {
		return s, e
	}
	if env.Error != 0 {
		return s, fmt.Errorf("eWeLink Token 刷新失败（%d）：%s，请删除账号后重新登录", env.Error, env.Msg)
	}
	if env.Data.AT == "" {
		return s, errors.New("eWeLink Token 刷新响应没有 access token")
	}
	s.AccessToken = env.Data.AT
	if env.Data.RT != "" {
		s.RefreshToken = env.Data.RT
	}
	s.UpdatedAt = time.Now()
	if e = m.vault.Put(id, s); e != nil {
		return s, e
	}
	return s, nil
}
