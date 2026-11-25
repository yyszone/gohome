package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	ewelinkdriver "gohome/internal/providers/ewelink"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// SonoffLAN-compatible consumer login. This is the public app identifier used
// by the MIT-licensed AlexxIT/SonoffLAN integration, not a per-user developer app.
// See THIRD_PARTY_NOTICES.md for attribution and license.
const ewelinkAppID = "R8Oq3y0eSZSYdKccHlrQzT1ACCOUT9Gv"

const ewelinkSigningKeyBase64 = "MXZlNVFrOUdYZlVoS0FuMXN2bkt3cEFseFhrTWFycnU="

func ewelinkSigningKey() []byte {
	key, _ := base64.StdEncoding.DecodeString(ewelinkSigningKeyBase64)
	return key
}

func buildEwelinkLoginPayload(in ewelinkLoginInput) ([]byte, string, error) {
	in.Username = strings.TrimSpace(in.Username)
	if in.Username == "" || in.Password == "" {
		return nil, "", errors.New("请输入 eWeLink 邮箱/手机号和密码")
	}
	countryCode := firstNonEmpty(in.CountryCode, "+86")
	payload := map[string]string{"password": in.Password, "countryCode": countryCode}
	if strings.Contains(in.Username, "@") {
		payload["email"] = in.Username
	} else {
		phone := in.Username
		if !strings.HasPrefix(phone, "+") {
			phone = countryCode + strings.TrimLeft(phone, "0")
		}
		payload["phoneNumber"] = phone
	}
	body, err := json.Marshal(payload)
	return body, countryCode, err
}

func (m *integrationManager) ewelinkLogin(ctx context.Context, in ewelinkLoginInput) (map[string]any, error) {
	in.Username = strings.TrimSpace(in.Username)
	body, countryCode, err := buildEwelinkLoginPayload(in)
	if err != nil {
		return nil, err
	}
	region := ewelinkRegionForCountry(countryCode, in.Region)
	host := ewelinkHost(region)
	client := &http.Client{Timeout: 25 * time.Second}
	login := func(apiHost string) ([]byte, error) {
		req, e := http.NewRequestWithContext(ctx, http.MethodPost, apiHost+"/v2/user/login", bytes.NewReader(body))
		if e != nil {
			return nil, e
		}
		setEwelinkHeaders(req, ewelinkAppID, "", body, false)
		resp, e := client.Do(req)
		if e != nil {
			return nil, e
		}
		return readBody(resp)
	}
	raw, err := login(host)
	if err != nil {
		return nil, fmt.Errorf("连接 eWeLink %s 区域失败：%w", region, err)
	}
	var env struct {
		Error int    `json:"error"`
		Msg   string `json:"msg"`
		Data  struct {
			AT     string `json:"at"`
			RT     string `json:"rt"`
			Region string `json:"region"`
			User   struct {
				Email  string `json:"email"`
				Phone  string `json:"phoneNumber"`
				Apikey string `json:"apikey"`
			} `json:"user"`
		} `json:"data"`
	}
	if err = json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("解析 eWeLink 登录响应失败：%w", err)
	}
	if env.Error == 10004 && env.Data.Region != "" {
		region = normalizeEwelinkRegion(env.Data.Region)
		host = ewelinkHost(region)
		raw, err = login(host)
		if err != nil {
			return nil, fmt.Errorf("eWeLink 区域切换后重试失败：%w", err)
		}
		if err = json.Unmarshal(raw, &env); err != nil {
			return nil, err
		}
	}
	if env.Error != 0 {
		return nil, fmt.Errorf("eWeLink 登录失败（%d）：%s", env.Error, env.Msg)
	}
	if env.Data.AT == "" || env.Data.RT == "" {
		return nil, errors.New("eWeLink 登录响应缺少 access/refresh token")
	}
	identity := firstNonEmpty(env.Data.User.Email, env.Data.User.Phone, in.Username)
	id := newID()
	secret := providerSecret{Provider: "ewelink", Region: region, AppID: ewelinkAppID, Email: in.Username, CountryCode: countryCode, AccessToken: env.Data.AT, RefreshToken: env.Data.RT, UserAPIKey: env.Data.User.Apikey, UpdatedAt: time.Now(), DeviceKeys: map[string]string{}}
	if err = m.vault.Put(id, secret); err != nil {
		return nil, fmt.Errorf("加密保存 eWeLink 令牌失败：%w", err)
	}
	ac := Account{ID: id, Provider: "ewelink", Label: firstNonEmpty(in.Label, identity), Region: region, Status: "connected", Note: "eWeLink 账号已登录，正在同步云端设备", CreatedAt: time.Now()}
	if err = m.store.addAccount(ac); err != nil {
		_ = m.vault.Delete(id)
		return nil, err
	}
	devices, keys, err := ewelinkFetchDevicesAndKeys(ctx, secret)
	if err != nil {
		m.setAccountStatus(id, "sync_error", "登录成功，但设备同步失败："+err.Error())
		account, _ := m.findAccount(id)
		return map[string]any{"account": account, "connected": true, "deviceCount": 0, "warning": "eWeLink 登录成功，但读取设备列表失败：" + err.Error()}, nil
	}
	secret.DeviceKeys = keys
	if err = m.vault.Put(id, secret); err != nil {
		m.setAccountStatus(id, "sync_error", "设备已读取，但加密保存设备密钥失败："+err.Error())
		return nil, err
	}
	m.importDevices(id, "ewelink", devices)
	m.setAccountStatus(id, "connected", fmt.Sprintf("已登录 · 已同步 %d 台设备", len(devices)))
	account, _ := m.findAccount(id)
	return map[string]any{"account": account, "connected": true, "deviceCount": len(devices)}, nil
}

type ewelinkLoginInput struct {
	Label       string `json:"label"`
	Region      string `json:"region"`
	Username    string `json:"username"`
	CountryCode string `json:"countryCode"`
	Password    string `json:"password"`
}

// SonoffLAN chooses the initial eWeLink API cluster from the account's country
// calling code. An explicitly selected region is still accepted as an override.
func ewelinkRegionForCountry(countryCode, requested string) string {
	if region := normalizeEwelinkRegion(requested); region != "" {
		return region
	}
	switch strings.TrimSpace(countryCode) {
	case "+86":
		return "cn"
	case "+1", "+61", "+55", "+52", "+54", "+56", "+57":
		return "us"
	case "+44", "+49", "+33", "+34", "+39", "+31", "+32", "+46", "+47", "+45", "+41", "+48", "+351", "+353":
		return "eu"
	case "+65", "+81", "+82", "+852", "+853", "+60", "+62", "+63", "+66", "+84", "+91", "+886", "+64":
		return "as"
	default:
		return "as"
	}
}

func ewelinkHost(region string) string {
	switch normalizeEwelinkRegion(region) {
	case "cn", "":
		return "https://cn-apia.coolkit.cn"
	case "as":
		return "https://as-apia.coolkit.cc"
	case "us":
		return "https://us-apia.coolkit.cc"
	default:
		return "https://eu-apia.coolkit.cc"
	}
}

func signHMAC(_ string, body []byte) string {
	h := hmac.New(sha256.New, ewelinkSigningKey())
	h.Write(body)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func setEwelinkHeaders(req *http.Request, _ string, _ string, body []byte, bearer bool) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CK-Appid", ewelinkAppID)
	if bearer {
		return
	}
	req.Header.Set("Authorization", "Sign "+signHMAC("", body))
}

func randomAlphaNum(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	r := make([]byte, n)
	_, _ = rand.Read(r)
	for i := range b {
		b[i] = chars[int(r[i])%len(chars)]
	}
	return string(b)
}

func ewelinkFetchDevices(ctx context.Context, s providerSecret) ([]Device, error) {
	devices, _, err := ewelinkFetchDevicesAndKeys(ctx, s)
	return devices, err
}

func ewelinkFetchDevicesAndKeys(ctx context.Context, s providerSecret) ([]Device, map[string]string, error) {
	return ewelinkFetchDevicesWithClientAndKeys(ctx, s, &http.Client{Timeout: 30 * time.Second}, ewelinkHost(s.Region))
}

// ewelinkFetchDevicesForStateRefresh explicitly asks the status endpoint for
// every online device. The normal account-sync path remains conservative, but
// periodic polling must not rely only on the possibly cached params field in
// the device-list response after a user toggles a switch in another app.
func ewelinkFetchDevicesForStateRefresh(ctx context.Context, s providerSecret) ([]Device, map[string]string, error) {
	return ewelinkFetchDevicesWithClientAndKeysOptions(ctx, s, &http.Client{Timeout: 35 * time.Second}, ewelinkHost(s.Region), true)
}

// ewelinkFetchDevicesWithClient is kept as a small test seam for the official list endpoint.
func ewelinkFetchDevicesWithClient(ctx context.Context, s providerSecret, client *http.Client, baseURL string) ([]Device, error) {
	devices, _, err := ewelinkFetchDevicesWithClientAndKeys(ctx, s, client, baseURL)
	return devices, err
}

// parseEwelinkRoomNames parses the official GET /v2/family response.
// Device itemData.family.roomid is an ID, not a display name.
func parseEwelinkRoomNames(body []byte) (map[string]string, error) {
	var env struct {
		Error int    `json:"error"`
		Msg   string `json:"msg"`
		Data  struct {
			FamilyList []struct {
				RoomList []struct {
					ID   any    `json:"id"`
					Name string `json:"name"`
				} `json:"roomList"`
			} `json:"familyList"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("解析 eWeLink 房间列表失败：%w", err)
	}
	if env.Error != 0 {
		return nil, fmt.Errorf("读取 eWeLink 房间列表失败（%d）：%s", env.Error, env.Msg)
	}
	rooms := make(map[string]string)
	for _, family := range env.Data.FamilyList {
		for _, room := range family.RoomList {
			id, name := strings.TrimSpace(stringValue(room.ID)), strings.TrimSpace(room.Name)
			if id != "" && name != "" {
				rooms[id] = name
			}
		}
	}
	return rooms, nil
}

func ewelinkFetchRoomNames(ctx context.Context, s providerSecret, client *http.Client, baseURL string) (map[string]string, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/v2/family?lang=cn"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	setEwelinkHeaders(req, s.AppID, s.AppSecret, nil, true)
	req.Header.Set("Authorization", "Bearer "+s.AccessToken)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 eWeLink 家庭/房间列表失败：%w", err)
	}
	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	return parseEwelinkRoomNames(body)
}

// ewelinkFetchDevicesWithClientAndKeys uses the documented GET /v2/device/thing list endpoint.
func ewelinkFetchDevicesWithClientAndKeys(ctx context.Context, s providerSecret, client *http.Client, baseURL string) ([]Device, map[string]string, error) {
	return ewelinkFetchDevicesWithClientAndKeysOptions(ctx, s, client, baseURL, false)
}

func ewelinkFetchDevicesWithClientAndKeysOptions(ctx context.Context, s providerSecret, client *http.Client, baseURL string, forceStatusRefresh bool) ([]Device, map[string]string, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/v2/device/thing?lang=cn&num=0"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, err
	}
	setEwelinkHeaders(req, s.AppID, s.AppSecret, nil, true)
	req.Header.Set("Authorization", "Bearer "+s.AccessToken)
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("请求 eWeLink 设备清单失败: %w", err)
	}
	body, err := readBody(resp)
	if err != nil {
		return nil, nil, err
	}
	var env struct {
		Error int    `json:"error"`
		Msg   string `json:"msg"`
		Data  struct {
			Total     int `json:"total"`
			ThingList []struct {
				ItemType int `json:"itemType"`
				ItemData struct {
					DeviceID     string         `json:"deviceid"`
					Name         string         `json:"name"`
					ProductModel string         `json:"productModel"`
					BrandName    string         `json:"brandName"`
					Online       bool           `json:"online"`
					DeviceKey    string         `json:"devicekey"`
					Params       map[string]any `json:"params"`
					Family       struct {
						RoomID any `json:"roomid"`
					} `json:"family"`
				} `json:"itemData"`
			} `json:"thingList"`
		} `json:"data"`
	}
	if err = json.Unmarshal(body, &env); err != nil {
		return nil, nil, fmt.Errorf("解析 eWeLink 设备清单失败: %w", err)
	}
	if env.Error != 0 {
		return nil, nil, fmt.Errorf("eWeLink 设备清单 API 错误（%d）：%s", env.Error, env.Msg)
	}
	roomNames := map[string]string{}
	if !forceStatusRefresh {
		needsRooms := false
		for _, item := range env.Data.ThingList {
			if strings.TrimSpace(stringValue(item.ItemData.Family.RoomID)) != "" {
				needsRooms = true
				break
			}
		}
		if needsRooms {
			// Resolve room IDs only during explicit account/device sync. The live
			// status poll reuses the already stored room and never calls /v2/family.
			if names, roomErr := ewelinkFetchRoomNames(ctx, s, client, baseURL); roomErr == nil {
				roomNames = names
			}
		}
	}
	// Some eWeLink list responses omit the live params for an online device.
	// Query its status once during sync so supported switches are not
	// incorrectly classified as read-only. Keep concurrency and timeout low.
	var statusWG sync.WaitGroup
	statusFresh := make([]bool, len(env.Data.ThingList))
	// The official API asks clients to leave at least 500 ms between calls
	// to the same interface. Serialize and pace optional status lookups.
	statusSem := make(chan struct{}, 1)
	var lastStatusRequest time.Time
	for i := range env.Data.ThingList {
		item := &env.Data.ThingList[i]
		needsStatus := forceStatusRefresh || len(item.ItemData.Params) == 0
		if ewelinkModelHasMeter(item.ItemData.ProductModel) && (!ewelinkHasMeterReadings(item.ItemData.Params) || !ewelinkHasPeriodEnergyReadings(item.ItemData.Params)) {
			// Instant power alone is not a daily/weekly energy counter. Query the
			// status endpoint when period totals are absent, even if watts exist.
			needsStatus = true
		}
		if (item.ItemType != 1 && item.ItemType != 2) || item.ItemData.DeviceID == "" || !item.ItemData.Online || !needsStatus {
			continue
		}
		statusWG.Add(1)
		go func(index int) {
			defer statusWG.Done()
			statusSem <- struct{}{}
			defer func() { <-statusSem }()
			statusCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			if waitErr := waitEwelinkStatusInterval(statusCtx, &lastStatusRequest); waitErr != nil {
				return
			}
			deviceID := env.Data.ThingList[index].ItemData.DeviceID
			params, statusErr := ewelinkGetDeviceStatus(statusCtx, s, deviceID, client, baseURL)
			if statusErr == nil {
				statusFresh[index] = true
				if params == nil {
					params = map[string]any{}
				}
				env.Data.ThingList[index].ItemData.Params = params
			} else if forceStatusRefresh {
				// Do not fall back to the device-list's cached params during a live
				// refresh. That is precisely how an old ON state can survive a failed
				// status query and look current in the UI.
				env.Data.ThingList[index].ItemData.Params = map[string]any{}
			}
		}(i)
	}

	statusWG.Wait()
	out := make([]Device, 0, len(env.Data.ThingList))
	keys := make(map[string]string)
	for itemIndex, item := range env.Data.ThingList {
		d := item.ItemData
		if item.ItemType != 1 && item.ItemType != 2 {
			continue
		}
		if d.DeviceID == "" {
			continue
		}
		if d.DeviceKey != "" {
			keys[d.DeviceID] = d.DeviceKey
		}
		brand := firstNonEmpty(d.BrandName, "Sonoff")
		if strings.Contains(strings.ToLower(brand), "ewelink") {
			brand = "Sonoff"
		}
		power := parseSwitch(d.Params["switch"])
		channels := parseEwelinkChannels(d.Params)
		if power == nil && len(channels) > 0 {
			power = channels[0].Power
		}
		// Single-relay devices expose params.switch; multi-relay devices
		// commonly expose params.switches as [{outlet, switch}, ...].
		canControl := power != nil || len(channels) > 0
		roomName := roomNames[strings.TrimSpace(stringValue(d.Family.RoomID))]
		dev := Device{ID: "ewelink-" + safeID(d.DeviceID), RemoteID: d.DeviceID, Name: firstNonEmpty(d.Name, d.ProductModel, d.DeviceID), Brand: brand, Model: d.ProductModel, Room: firstNonEmpty(roomName, "未分区"), Category: guessCategory(d.Name, d.ProductModel), Protocol: "ewelink-cloud", Online: d.Online, Power: power, Channels: channels, CanControl: canControl, Energy: parseEwelinkEnergyMetrics(d.Params, d.ProductModel), Attributes: parseEwelinkDeviceAttributes(d.Params), LastSeen: time.Now(), CreatedAt: time.Now(), StateRead: statusFresh[itemIndex]}
		dev.Value = summarizeDeviceAttributes(&dev)
		out = append(out, dev)
	}
	return out, keys, nil
}

const ewelinkStatusMinInterval = 600 * time.Millisecond

func waitEwelinkStatusInterval(ctx context.Context, lastRequest *time.Time) error {
	if lastRequest != nil && !lastRequest.IsZero() {
		wait := time.Until(lastRequest.Add(ewelinkStatusMinInterval))
		if wait > 0 {
			timer := time.NewTimer(wait)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	if lastRequest != nil {
		*lastRequest = time.Now()
	}
	return nil
}

func ewelinkGetDeviceStatus(ctx context.Context, s providerSecret, deviceID string, client *http.Client, baseURL string) (map[string]any, error) {
	if strings.TrimSpace(deviceID) == "" {
		return nil, errors.New("eWeLink device ID is empty")
	}
	query := url.Values{"type": {"1"}, "id": {deviceID}}
	endpoint := strings.TrimRight(baseURL, "/") + "/v2/device/thing/status?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	setEwelinkHeaders(req, s.AppID, s.AppSecret, nil, true)
	req.Header.Set("Authorization", "Bearer "+s.AccessToken)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	var env struct {
		Error int    `json:"error"`
		Msg   string `json:"msg"`
		Data  struct {
			Params map[string]any `json:"params"`
			Config map[string]any `json:"config"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	if env.Error != 0 {
		return nil, fmt.Errorf("eWeLink 状态查询失败（%d）：%s", env.Error, env.Msg)
	}
	if len(env.Data.Params) == 0 && len(env.Data.Config) > 0 {
		return env.Data.Config, nil
	}
	return env.Data.Params, nil
}

func ewelinkHasMeterReadings(params map[string]any) bool {
	return findMetric(params, "power", "apower", "active_power", "electric_power", "power_w", "power_1") != nil ||
		findMetric(params, "voltage", "voltage_v", "voltage_1") != nil ||
		findMetric(params, "current", "current_a", "current_1") != nil ||
		findMetric(params, "dailyKwh", "dayKwh", "todayKwh", "weekKwh", "weeklyKwh", "monthlyKwh", "monthKwh") != nil ||
		findMetric(params, "energy_day", "energyday", "daily_energy", "today_energy", "today_kwh", "energy_week", "energy_month", "energy_year", "energy", "energy_1", "energy_2") != nil
}

func ewelinkHasPeriodEnergyReadings(params map[string]any) bool {
	hasToday := findMetric(params, "dailyKwh", "dayKwh", "todayKwh", "energy_day", "energyday", "daily_energy", "today_energy", "today_kwh") != nil
	hasWeek := findMetric(params, "weekKwh", "weeklyKwh", "energy_week", "energyweek", "weekly_energy", "week_kwh") != nil
	return hasToday && hasWeek
}

func ewelinkModelHasMeter(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(m, "pow") || strings.Contains(m, "s31") || strings.Contains(m, "s60tpf") || strings.Contains(m, "spm")
}

func ewelinkCommand(ctx context.Context, s providerSecret, deviceID, command string) error {
	return ewelinkCommandChannel(ctx, s, deviceID, command, 0, false)
}

func ewelinkCommandChannel(ctx context.Context, s providerSecret, deviceID, command string, outlet int, multi bool) error {
	return ewelinkCommandChannelWithClient(ctx, s, deviceID, command, outlet, multi, &http.Client{Timeout: 15 * time.Second}, ewelinkHost(s.Region))
}

// ewelinkCommandWithClient is retained as a test seam for single-relay devices.
func ewelinkCommandWithClient(ctx context.Context, s providerSecret, deviceID, command string, client *http.Client, baseURL string) error {
	return ewelinkCommandChannelWithClient(ctx, s, deviceID, command, 0, false, client, baseURL)
}

// ewelinkCommandChannelWithClient uses the documented consumer-account token
// endpoint. Standard single-relay devices use params.switch; multi-relay
// devices use params.switches with an outlet index.
func ewelinkCommandChannelWithClient(ctx context.Context, s providerSecret, deviceID, command string, outlet int, multi bool, client *http.Client, baseURL string) error {
	body, e := ewelinkdriver.BuildCommandPayload(deviceID, command, outlet, multi)
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/v2/device/thing/status", bytes.NewReader(body))
	if e != nil {
		return e
	}
	setEwelinkHeaders(req, s.AppID, s.AppSecret, body, true)
	req.Header.Set("Authorization", "Bearer "+s.AccessToken)
	resp, e := client.Do(req)
	if e != nil {
		return fmt.Errorf("发送 eWeLink 控制命令失败：%w", e)
	}
	b, e := readBody(resp)
	if e != nil {
		return e
	}
	var env struct {
		Error int    `json:"error"`
		Msg   string `json:"msg"`
	}
	if e = json.Unmarshal(b, &env); e != nil {
		return fmt.Errorf("解析 eWeLink 控制响应失败：%w", e)
	}
	if env.Error != 0 {
		return fmt.Errorf("eWeLink 控制失败（%d）：%s", env.Error, env.Msg)
	}
	return nil
}
