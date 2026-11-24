package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rc4"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	miotdriver "gohome/internal/providers/xiaomi"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func xiaomiAPIUserAgent(deviceID string) string {
	return xiaomiUserAgentForDevice(deviceID)
}

// Xiaomi/MiCloud nonce format is fixed at 12 bytes: 8 cryptographically
// random bytes followed by uint32(unix_seconds/60), big-endian.
func xiaomiNonceAt(randomPart []byte, now time.Time) ([]byte, error) {
	if len(randomPart) != 8 {
		return nil, fmt.Errorf("小米 nonce 随机段必须是 8 字节，实际 %d 字节", len(randomPart))
	}
	nonce := make([]byte, 12)
	copy(nonce[:8], randomPart)
	binary.BigEndian.PutUint32(nonce[8:], uint32(now.Unix()/60))
	return nonce, nil
}

func xiaomiGenerateNonce() (string, []byte, error) {
	randomPart := make([]byte, 8)
	if _, err := rand.Read(randomPart); err != nil {
		return "", nil, err
	}
	value, err := xiaomiNonceAt(randomPart, time.Now())
	if err != nil {
		return "", nil, err
	}
	return base64.StdEncoding.EncodeToString(value), value, nil
}

func zoneOffset(t time.Time) int {
	_, offset := t.Zone()
	return offset
}

func xiaomiAPICookieJar(base string, s providerSecret) (*cookiejar.Jar, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	tz := time.Now().Format("-0700")
	if len(tz) == 5 {
		tz = "GMT" + tz[:3] + ":" + tz[3:]
	} else {
		tz = "GMT+00:00"
	}
	now := time.Now()
	dstOffset := "0"
	if now.IsDST() {
		dstOffset = "3600000"
	}
	// Python's time.daylight (used by Xiaomi Miot Auto) indicates whether
	// the local timezone defines a DST rule, not whether DST is active now.
	janOffset := zoneOffset(time.Date(now.Year(), time.January, 1, 12, 0, 0, 0, now.Location()))
	julOffset := zoneOffset(time.Date(now.Year(), time.July, 1, 12, 0, 0, 0, now.Location()))
	isDaylight := "0"
	if janOffset != julOffset {
		isDaylight = "1"
	}
	jar.SetCookies(u, []*http.Cookie{
		{Name: "serviceToken", Value: s.ServiceToken, Path: "/"},
		{Name: "yetAnotherServiceToken", Value: s.ServiceToken, Path: "/"},
		{Name: "userId", Value: s.UserID, Path: "/"},
		{Name: "locale", Value: "zh_CN", Path: "/"},
		{Name: "timezone", Value: tz, Path: "/"},
		{Name: "is_daylight", Value: isDaylight, Path: "/"},
		{Name: "dst_offset", Value: dstOffset, Path: "/"},
		{Name: "channel", Value: "MI_APP_STORE", Path: "/"},
	})
	return jar, nil
}

func xiaomiAPIStatusError(resp *http.Response, body []byte, s providerSecret, authProbe string) error {
	host, path := "unknown", "/"
	status := 0
	if resp != nil {
		status = resp.StatusCode
		if resp.Request != nil && resp.Request.URL != nil {
			host, path = resp.Request.URL.Host, resp.Request.URL.Path
		}
	}
	detail := strings.TrimSpace(sanitizeIntegrationLog(string(body)))
	if detail == "" {
		detail = "响应体为空"
	}
	class := "云端拒绝请求"
	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		msg := strings.ToLower(envelope.Message)
		if envelope.Code == 2 || envelope.Code == 3 || strings.Contains(msg, "auth err") || strings.Contains(msg, "servicetoken_expired") {
			class = "云端返回认证失效，serviceToken/userId 组合被拒绝"
		} else if strings.Contains(msg, "invalid signature") {
			class = "云端判定签名无效，请检查 ssecurity、nonce、UA 与请求参数"
		}
	}
	probeText := ""
	if strings.TrimSpace(authProbe) != "" {
		probeText = "；HA 同款认证探测：" + truncate(sanitizeIntegrationLog(authProbe), 180)
	}
	return fmt.Errorf("小米云端 HTTP %d（%s）：%s；region=%s host=%s path=%s；认证字段：userId=%t serviceToken=%t ssecurity=%t clientID=%t；响应：%s%s", status, class, http.StatusText(status), firstNonEmpty(s.Region, "cn"), host, path, s.UserID != "", s.ServiceToken != "", s.SSecurity != "", s.DeviceID != "", truncate(detail, 260), probeText)
}

// Xiaomi Miot Auto validates a saved xiaomiio session against this lightweight
// endpoint before/while retrying cloud requests. It is independent of RC4
// request signing, so it helps distinguish an expired serviceToken from a
// signature/region problem on api.io.mi.com.
func xiaomiProbeAuthWithClient(ctx context.Context, s providerSecret, client *http.Client) string {
	if client == nil {
		return "探测客户端不可用"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api2.mina.mi.com/admin/v2/device_list", nil)
	if err != nil {
		return "创建认证探测请求失败：" + err.Error()
	}
	req.Header.Set("User-Agent", xiaomiAPIUserAgent(s.DeviceID))
	req.Header.Set("x-xiaomi-protocal-flag-cli", "PROTOCAL-HTTP2")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return "网络探测失败：" + truncate(sanitizeIntegrationLog(err.Error()), 120)
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	if readErr != nil {
		return fmt.Sprintf("HTTP %d，但读取探测响应失败：%s", resp.StatusCode, truncate(readErr.Error(), 100))
	}
	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	validJSON := json.Unmarshal(body, &envelope) == nil
	message := strings.TrimSpace(sanitizeIntegrationLog(envelope.Message))
	messageLower := strings.ToLower(message)
	switch resp.StatusCode {
	case http.StatusOK:
		if validJSON && (envelope.Code == 2 || envelope.Code == 3 || strings.Contains(messageLower, "auth err") || strings.Contains(messageLower, "servicetoken_expired")) {
			return fmt.Sprintf("HTTP 200 但业务层仍判定认证失效（code=%d message=%s）", envelope.Code, firstNonEmpty(message, "无"))
		}
		if validJSON && envelope.Code == 0 {
			return "HTTP 200 且业务 code=0，账号 Cookie 已被 mina 服务接受；主 API 的 401 更可能是区域/签名/请求头差异"
		}
		return fmt.Sprintf("HTTP 200，但探测响应无法确认认证状态（json=%t code=%d message=%s）", validJSON, envelope.Code, firstNonEmpty(message, "无"))
	case http.StatusUnauthorized:
		return fmt.Sprintf("HTTP 401，mina 服务也拒绝此 serviceToken/userId（code=%d message=%s）；应重新登录并确认账号区域", envelope.Code, firstNonEmpty(message, "无"))
	case http.StatusForbidden:
		return fmt.Sprintf("HTTP 403，mina 服务拒绝访问（message=%s）；检查账号权限/风控状态", firstNonEmpty(message, "无"))
	default:
		return fmt.Sprintf("HTTP %d，code=%d message=%s", resp.StatusCode, envelope.Code, firstNonEmpty(message, "无"))
	}
}

func xiaomiProbeAuth(ctx context.Context, s providerSecret) string {
	jar, err := xiaomiAPICookieJar("https://api2.mina.mi.com", s)
	if err != nil {
		return "无法创建认证探测 Cookie：" + err.Error()
	}
	client := &http.Client{Jar: jar, Timeout: 12 * time.Second}
	return xiaomiProbeAuthWithClient(ctx, s, client)
}

func xiaomiFetchDevices(ctx context.Context, s providerSecret) ([]Device, error) {
	return xiaomiFetchDevicesWithOptions(ctx, s, true, false)
}

// xiaomiFetchDevicesForStateRefresh reads the online flag and current MIoT
// properties without querying model-specific historical energy endpoints.
// It is used by the background state poller; interactive/manual sync still
// uses xiaomiFetchDevices and includes the historical energy lookup.
func xiaomiFetchDevicesForStateRefresh(ctx context.Context, s providerSecret) ([]Device, error) {
	return xiaomiFetchDevicesWithOptions(ctx, s, false, true)
}

func xiaomiFetchDevicesWithOptions(ctx context.Context, s providerSecret, includeEnergyStats, onlineOnly bool) ([]Device, error) {
	base := xiaomiAPIBase(s.Region)
	apiURL := base + "/app/home/device_list"
	rawData := []byte(`{"getVirtualModel":true,"getHuamiDevices":1,"get_split_device":false,"support_smart_home":true}`)
	nonce, nb, e := xiaomiGenerateNonce()
	if e != nil {
		return nil, e
	}
	if s.DeviceID == "" {
		s.DeviceID = xiaomiAccountDeviceID()
	}
	ss, e := base64.StdEncoding.DecodeString(s.SSecurity)
	if e != nil {
		return nil, fmt.Errorf("小米 ssecurity 无效: %w", e)
	}
	h := sha256.New()
	h.Write(ss)
	h.Write(nb)
	signed := base64.StdEncoding.EncodeToString(h.Sum(nil))
	key, _ := base64.StdEncoding.DecodeString(signed)
	dataText := string(rawData)
	pathPart := "/home/device_list"
	// The Xiaomi RC4 signature is order-sensitive. The MIoT client inserts
	// data first and rc4_hash__ second; sorting these keys changes the SHA1
	// input and is rejected by api.io.mi.com as "invalid signature".
	rc4hash := xiaomiSHA1SignFields("POST", pathPart, []string{"data=" + dataText}, signed)
	encData, err := xiaomiRC4Encode(key, dataText)
	if err != nil {
		return nil, err
	}
	encRC4Hash, err := xiaomiRC4Encode(key, rc4hash)
	if err != nil {
		return nil, err
	}
	sig := xiaomiSHA1SignFields("POST", pathPart, []string{"data=" + encData, "rc4_hash__=" + encRC4Hash}, signed)
	params := url.Values{}
	params.Set("data", encData)
	params.Set("rc4_hash__", encRC4Hash)
	params.Set("signature", sig)
	params.Set("ssecurity", s.SSecurity)
	params.Set("_nonce", nonce)
	jar, err := xiaomiAPICookieJar(base, s)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Jar: jar, Timeout: 35 * time.Second}
	apiUA := xiaomiAPIUserAgent(s.DeviceID)
	// Keep the same insertion order as Xiaomi Miot Auto's Python dict:
	// data, rc4_hash__, signature, ssecurity, _nonce. url.Values.Encode sorts
	// keys alphabetically and can diverge from the reference client's wire form.
	formBody := xiaomiOrderedAPIFormBody(encData, encRC4Hash, sig, s.SSecurity, nonce)
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(formBody))
	if e != nil {
		return nil, e
	}
	req.Header.Set("User-Agent", apiUA)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("MIOT-ENCRYPT-ALGORITHM", "ENCRYPT-RC4")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("x-xiaomi-protocal-flag-cli", "PROTOCAL-HTTP2")
	resp, e := client.Do(req)
	if e != nil {
		return nil, fmt.Errorf("请求小米设备列表失败: %w", e)
	}
	body, e := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	_ = resp.Body.Close()
	if e != nil {
		return nil, fmt.Errorf("读取小米设备列表响应失败：%w", e)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		probe := ""
		if resp.StatusCode == http.StatusUnauthorized {
			probe = xiaomiProbeAuth(ctx, s)
		}
		return nil, xiaomiAPIStatusError(resp, body, s, probe)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, errors.New("小米云端返回空响应")
	}
	// MIoT cloud replies with a JSON envelope; RC4 is used for request parameters.
	// Some older endpoints return a base64 RC4 payload, so keep a defensive fallback.
	decoded := bytes.TrimSpace(body)
	if !json.Valid(decoded) {
		if plain, decErr := xiaomiRC4Decode(key, string(decoded)); decErr == nil && json.Valid(plain) {
			decoded = plain
		} else {
			return nil, fmt.Errorf("小米云端响应不是有效 JSON：%s", truncate(string(decoded), 240))
		}
	}
	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Result  struct {
			List []struct {
				DID      string `json:"did"`
				Name     string `json:"name"`
				Model    string `json:"model"`
				LocalIP  string `json:"localip"`
				Online   any    `json:"isOnline"`
				MAC      string `json:"mac"`
				RoomName string `json:"room_name"`
			} `json:"list"`
		} `json:"result"`
	}
	if e = json.Unmarshal(decoded, &envelope); e != nil {
		return nil, fmt.Errorf("解析小米设备列表失败: %w", e)
	}
	if envelope.Code != 0 {
		return nil, fmt.Errorf("小米云端错误 %d: %s", envelope.Code, envelope.Message)
	}
	out := make([]Device, 0, len(envelope.Result.List))
	for _, x := range envelope.Result.List {
		if x.DID == "" {
			continue
		}
		online := boolValue(x.Online)
		cat := guessCategory(x.Name, x.Model)
		out = append(out, Device{ID: "xiaomi-" + safeID(x.DID), RemoteID: x.DID, Name: firstNonEmpty(x.Name, x.Model, x.DID), Brand: "小米", Model: x.Model, Room: firstNonEmpty(x.RoomName, "未分区"), Category: cat, Protocol: "xiaomi-cloud", Online: online, LastSeen: time.Now(), Demo: false, CreatedAt: time.Now()})
	}
	// Read useful read-only MIoT properties for every supported device class.
	// Unsupported models remain in the list with their online/offline state.
	xiaomiPopulateDeviceDetailsWithOptions(ctx, s, out, includeEnergyStats, onlineOnly)
	return out, nil
}

type miotSpecIndex struct {
	Instances []struct {
		Model string `json:"model"`
		Type  string `json:"type"`
	} `json:"instances"`
}

type miotSpecDocument struct {
	Services []struct {
		IID         int    `json:"iid"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Type        string `json:"type"`
		Properties  []struct {
			IID         int      `json:"iid"`
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Type        string   `json:"type"`
			Format      string   `json:"format"`
			Unit        string   `json:"unit"`
			Access      []string `json:"access"`
			ValueList   []struct {
				Value       any    `json:"value"`
				Description string `json:"description"`
			} `json:"value-list"`
		} `json:"properties"`
		Actions []struct {
			IID         int    `json:"iid"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Type        string `json:"type"`
			In          []int  `json:"in"`
			Out         []int  `json:"out"`
		} `json:"actions"`
	} `json:"services"`
}

type miotEnergyRef struct {
	SIID  int
	PIID  int
	Field string
	Scale float64
}

var miotSpecCache = struct {
	sync.Mutex
	indexAt time.Time
	models  map[string]string
	specs   map[string]miotSpecDocument
}{models: map[string]string{}, specs: map[string]miotSpecDocument{}}

func fetchMIoTModelType(ctx context.Context, model string) (string, error) {
	model = strings.TrimSpace(model)
	miotSpecCache.Lock()
	indexFresh := !miotSpecCache.indexAt.IsZero() && time.Since(miotSpecCache.indexAt) < 24*time.Hour
	if indexFresh {
		typ := miotSpecCache.models[model]
		miotSpecCache.Unlock()
		if typ == "" {
			return "", fmt.Errorf("MIoT 规格库未收录型号 %s", model)
		}
		return typ, nil
	}
	miotSpecCache.Unlock()

	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, "https://miot-spec.org/miot-spec-v2/instances?status=all", nil)
	if err != nil {
		return "", err
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	body, err := readBody(resp)
	if err != nil {
		return "", err
	}
	var index miotSpecIndex
	if err := json.Unmarshal(body, &index); err != nil {
		return "", fmt.Errorf("解析 MIoT 规格索引失败：%w", err)
	}
	models := make(map[string]string, len(index.Instances))
	for _, item := range index.Instances {
		if item.Model != "" && item.Type != "" {
			models[item.Model] = item.Type
		}
	}
	miotSpecCache.Lock()
	miotSpecCache.models = models
	miotSpecCache.indexAt = time.Now()
	typ := models[model]
	miotSpecCache.Unlock()
	if typ == "" {
		return "", fmt.Errorf("MIoT 规格库未收录型号 %s", model)
	}
	return typ, nil
}

func fetchMIoTSpec(ctx context.Context, typ string) (miotSpecDocument, error) {
	miotSpecCache.Lock()
	if cached, ok := miotSpecCache.specs[typ]; ok {
		miotSpecCache.Unlock()
		return cached, nil
	}
	miotSpecCache.Unlock()
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	u, _ := url.Parse("https://miot-spec.org/miot-spec-v2/instance")
	q := u.Query()
	q.Set("type", typ)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return miotSpecDocument{}, err
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return miotSpecDocument{}, err
	}
	body, err := readBody(resp)
	if err != nil {
		return miotSpecDocument{}, err
	}
	var spec miotSpecDocument
	if err := json.Unmarshal(body, &spec); err != nil {
		return spec, fmt.Errorf("解析 MIoT 设备规格失败：%w", err)
	}
	miotSpecCache.Lock()
	miotSpecCache.specs[typ] = spec
	miotSpecCache.Unlock()
	return spec, nil
}

func miotEnergyPropertyField(name, description, propType, unit string) (field string, scale float64) {
	text := strings.ToLower(strings.Join([]string{name, description, propType}, " "))
	u := strings.ToLower(strings.TrimSpace(unit))
	switch u {
	case "watt":
		u = "w"
	case "kilowatt":
		u = "kw"
	case "volt":
		u = "v"
	case "ampere":
		u = "a"
	case "milliampere":
		u = "ma"
	case "watt-hour", "watt hour":
		u = "wh"
	case "kilowatt-hour", "kilowatt hour":
		u = "kwh"
	}
	switch {
	case strings.Contains(text, "voltage") || strings.Contains(text, "electric_voltage") || strings.Contains(text, "电压"):
		scale = 1
		if u == "mv" || strings.Contains(u, "millivolt") {
			scale = 0.001
		}
		return "voltage", scale
	case strings.Contains(text, "current") || strings.Contains(text, "electric_current") || strings.Contains(text, "电流"):
		scale = 1
		if u == "ma" || strings.Contains(u, "milliamp") {
			scale = 0.001
		}
		if u == "ua" {
			scale = 0.000001
		}
		return "current", scale
	case (strings.Contains(text, "power") || strings.Contains(text, "electric_power") || strings.Contains(text, "功率")) && !strings.Contains(text, "consumption") && !strings.Contains(text, "消耗") && (u == "w" || u == "kw" || u == "mw" || strings.Contains(text, "active_power") || strings.Contains(text, "electric_power") || strings.Contains(text, "有功功率")):
		scale = 1
		if u == "kw" {
			scale = 1000
		}
		if u == "mw" {
			scale = 0.001
		}
		return "power", scale
	case strings.Contains(text, "energy") || strings.Contains(text, "electricity") || strings.Contains(text, "consumption") || strings.Contains(text, "电量") || strings.Contains(text, "用电") || strings.Contains(text, "耗电"):
		scale = 1
		if u == "wh" || u == "w·h" {
			scale = 0.001
		}
		if u == "mwh" {
			scale = 0.000001
		}
		if u == "j" {
			scale = 1.0 / 3600000.0
		}
		switch {
		case strings.Contains(text, "today") || strings.Contains(text, "daily") || strings.Contains(text, "day") || strings.Contains(text, "今日") || strings.Contains(text, "当天"):
			return "today", scale
		case strings.Contains(text, "week") || strings.Contains(text, "周"):
			return "week", scale
		case strings.Contains(text, "month") || strings.Contains(text, "月"):
			return "month", scale
		case strings.Contains(text, "year") || strings.Contains(text, "年"):
			return "year", scale
		case strings.Contains(text, "total") || strings.Contains(text, "accum") || strings.Contains(text, "累计") || strings.Contains(text, "总用电") || strings.Contains(text, "electricity_consumption"):
			return "total", scale
		default:
			// Only infer a lifetime counter when the MIoT specification explicitly
			// gives an energy unit. A boolean power switch is never a meter value.
			if u == "kwh" || u == "wh" || u == "w·h" {
				return "total", scale
			}
			return "", 0
		}
	}
	return "", 0
}

func miotReadEnergyRefs(spec miotSpecDocument) []miotEnergyRef {
	refs := make([]miotEnergyRef, 0, 8)
	seen := map[string]bool{}
	for _, service := range spec.Services {
		for _, prop := range service.Properties {
			readable := false
			for _, access := range prop.Access {
				if strings.EqualFold(access, "read") {
					readable = true
					break
				}
			}
			if !readable {
				continue
			}
			field, scale := miotEnergyPropertyField(prop.Name, prop.Description, prop.Type, prop.Unit)
			if field == "" {
				continue
			}
			key := fmt.Sprintf("%d:%d", service.IID, prop.IID)
			if seen[key] {
				continue
			}
			seen[key] = true
			refs = append(refs, miotEnergyRef{SIID: service.IID, PIID: prop.IID, Field: field, Scale: scale})
			if len(refs) >= 16 {
				return refs
			}
		}
	}
	return refs
}

func xiaomiEncryptedPost(ctx context.Context, s providerSecret, pathPart string, rawData []byte) ([]byte, error) {
	base := xiaomiAPIBase(s.Region)
	nonce, nb, err := xiaomiGenerateNonce()
	if err != nil {
		return nil, err
	}
	if s.DeviceID == "" {
		s.DeviceID = xiaomiAccountDeviceID()
	}
	ss, err := base64.StdEncoding.DecodeString(s.SSecurity)
	if err != nil {
		return nil, fmt.Errorf("小米 ssecurity 无效: %w", err)
	}
	h := sha256.New()
	h.Write(ss)
	h.Write(nb)
	signed := base64.StdEncoding.EncodeToString(h.Sum(nil))
	key, _ := base64.StdEncoding.DecodeString(signed)
	dataText := string(rawData)
	rc4hash := xiaomiSHA1SignFields("POST", pathPart, []string{"data=" + dataText}, signed)
	encData, err := xiaomiRC4Encode(key, dataText)
	if err != nil {
		return nil, err
	}
	encHash, err := xiaomiRC4Encode(key, rc4hash)
	if err != nil {
		return nil, err
	}
	jar, err := xiaomiAPICookieJar(base, s)
	if err != nil {
		return nil, err
	}
	formBody := xiaomiOrderedAPIFormBody(encData, encHash, xiaomiSHA1SignFields("POST", pathPart, []string{"data=" + encData, "rc4_hash__=" + encHash}, signed), s.SSecurity, nonce)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/app"+pathPart, strings.NewReader(formBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", xiaomiAPIUserAgent(s.DeviceID))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("MIOT-ENCRYPT-ALGORITHM", "ENCRYPT-RC4")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("x-xiaomi-protocal-flag-cli", "PROTOCAL-HTTP2")
	client := &http.Client{Jar: jar, Timeout: 18 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	decoded := bytes.TrimSpace(body)
	if !json.Valid(decoded) {
		if plain, decErr := xiaomiRC4Decode(key, string(decoded)); decErr == nil && json.Valid(plain) {
			decoded = plain
		} else {
			return nil, fmt.Errorf("小米 MIoT 返回非 JSON：%s", truncate(string(decoded), 180))
		}
	}
	return decoded, nil
}

func applyMIoTEnergyResult(e *EnergyMetrics, refs []miotEnergyRef, body []byte) {
	var env struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Result  []struct {
			SIID  int `json:"siid"`
			PIID  int `json:"piid"`
			Code  int `json:"code"`
			Value any `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &env) != nil || env.Code != 0 {
		return
	}
	refMap := make(map[string]miotEnergyRef, len(refs))
	for _, ref := range refs {
		refMap[fmt.Sprintf("%d:%d", ref.SIID, ref.PIID)] = ref
	}
	for _, item := range env.Result {
		if item.Code != 0 {
			continue
		}
		ref, ok := refMap[fmt.Sprintf("%d:%d", item.SIID, item.PIID)]
		if !ok {
			continue
		}
		n := numericMetric(item.Value)
		if n == nil {
			continue
		}
		value := *n * ref.Scale
		switch ref.Field {
		case "power":
			e.PowerW = &value
		case "voltage":
			e.VoltageV = &value
		case "current":
			e.CurrentA = &value
		case "today":
			e.TodayKWh = &value
		case "week":
			e.WeekKWh = &value
		case "month":
			e.MonthKWh = &value
		case "year":
			e.YearKWh = &value
		case "total":
			e.TotalKWh = &value
		}
	}
}

type miotDevicePropertyRef struct {
	SIID        int
	PIID        int
	Field       string
	Label       string
	Unit        string
	Scale       float64
	Format      string
	ControlOnly bool
	ValueList   []struct {
		Value       any    `json:"value"`
		Description string `json:"description"`
	}
}

func miotNormalizedName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(s)
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

// miotDisplayProperty maps common MIoT property identifiers to compact,
// useful card fields. It deliberately avoids reading arbitrary properties
// such as internal flags, firmware strings or write-only settings.
func miotDisplayProperty(name, description, serviceName, unit string) (field, label, displayUnit string, scale float64) {
	n := miotNormalizedName(name)
	t := strings.ToLower(strings.Join([]string{name, description}, " "))
	service := strings.ToLower(serviceName)
	u := strings.TrimSpace(unit)
	switch strings.ToLower(u) {
	case "celsius":
		u = "°C"
	case "fahrenheit":
		u = "°F"
	case "percentage":
		u = "%"
	case "lux":
		u = "lx"
	case "watt":
		u = "W"
	case "volt":
		u = "V"
	case "ampere":
		u = "A"
	case "kelvin":
		u = "K"
	case "pascal":
		u = "Pa"
	case "ppm":
		u = "ppm"
	case "μg/m3":
		u = "μg/m³"
	case "mg/m3":
		u = "mg/m³"
	}
	if strings.EqualFold(strings.TrimSpace(unit), "kelvin") || strings.EqualFold(u, "K") {
		return "color_temperature", "色温", "K", 1
	}
	switch {
	case n == "colortemperature" || n == "colourtemperature" || strings.Contains(n, "colortemperature") || strings.Contains(t, "色温"):
		return "color_temperature", "色温", firstNonEmpty(u, "K"), 1
	case n == "targettemperature" || n == "settemperature" || n == "desiredtemperature":
		return "target_temperature", "目标温度", firstNonEmpty(u, "°C"), 1
	case n == "currenttemperature" || n == "temperature" || strings.Contains(t, "环境温度") || strings.Contains(t, "temperature") || (strings.Contains(service, "temperature") && n == "current"):
		return "temperature", "温度", firstNonEmpty(u, "°C"), 1
	case n == "relativehumidity" || n == "humidity" || strings.Contains(t, "湿度"):
		return "humidity", "湿度", firstNonEmpty(u, "%"), 1
	case n == "batterylevel" || n == "batterypercentage" || n == "battery" || strings.Contains(t, "电池电量"):
		return "battery", "电量", firstNonEmpty(u, "%"), 1
	case n == "illuminance" || n == "lightintensity" || strings.Contains(t, "照度"):
		return "illuminance", "光照", firstNonEmpty(u, "lx"), 1
	case n == "pressure" || n == "atmosphericpressure" || strings.Contains(t, "气压"):
		return "pressure", "气压", firstNonEmpty(u, "Pa"), 1
	case n == "pm25density" || n == "pm25" || strings.Contains(t, "pm2.5"):
		return "pm25", "PM2.5", firstNonEmpty(u, "μg/m³"), 1
	case n == "co2density" || n == "co2" || strings.Contains(t, "二氧化碳"):
		return "co2", "CO₂", firstNonEmpty(u, "ppm"), 1
	case n == "tvocdensity" || n == "tvoc":
		return "tvoc", "TVOC", firstNonEmpty(u, "mg/m³"), 1
	case n == "volume" || n == "volumelevel" || strings.Contains(t, "音量"):
		return "volume", "音量", firstNonEmpty(u, "%"), 1
	case n == "isvolumemuted" || n == "mute" || n == "ismuted" || strings.Contains(t, "静音"):
		return "muted", "静音", "", 1
	case n == "playbackstate" || n == "playstatus" || n == "playstate" || n == "playing" || n == "mediastate":
		return "playback", "播放状态", "", 1
	case n == "motiondetection" || n == "humandetection" || n == "ishumandetection":
		return "motion_enabled", "移动侦测", "", 1
	case n == "motiondetected" || n == "ismotiondetected" || n == "humandetected" || n == "ishumandetected" || n == "motionstate" || n == "occupancy" || n == "presence":
		return "motion", "活动检测", "", 1
	case n == "recordingstatus" || n == "recordstatus" || n == "recording" || n == "videorecording" || n == "recordenable" || n == "recordmode":
		return "recording", "录像状态", "", 1
	case n == "storagestatus" || n == "sdcardstatus" || n == "memorycardstatus" || n == "sdcardstate":
		return "storage", "存储状态", "", 1
	case n == "isopen" || n == "dooropen" || n == "doorstate" || n == "contactstate" || n == "iscontact":
		return "contact", "门窗状态", "", 1
	case n == "waterleak" || n == "iswaterleak" || n == "leakdetected":
		return "waterleak", "漏水检测", "", 1
	case n == "smokedetected" || n == "smokealarm" || n == "coalarm" || n == "alarmstate":
		return "alarm", "报警状态", "", 1
	case n == "on" || n == "power" || n == "switch":
		return "on", "开关状态", "", 1
	case n == "brightness" || n == "brightnesslevel":
		return "brightness", "亮度", firstNonEmpty(u, "%"), 1
	}
	return "", "", "", 0
}

// xiaomiBuildDeviceControls converts the cached MIoT model specification into
// a persisted app-level control allow-list. Capability discovery lives in the
// independent internal/providers/xiaomi package; this adapter only maps types.
func xiaomiBuildDeviceControls(spec miotSpecDocument) []DeviceControl {
	definition := miotdriver.Specification{Services: make([]miotdriver.ServiceDefinition, 0, len(spec.Services))}
	for _, service := range spec.Services {
		entry := miotdriver.ServiceDefinition{IID: service.IID, Name: service.Name}
		for _, prop := range service.Properties {
			property := miotdriver.PropertyDefinition{
				IID: prop.IID, Name: prop.Name, Description: prop.Description,
				Type: prop.Type, Format: prop.Format, Access: append([]string(nil), prop.Access...),
			}
			for _, value := range prop.ValueList {
				property.ValueList = append(property.ValueList, miotdriver.ValueOption{Value: value.Value, Description: value.Description})
			}
			entry.Properties = append(entry.Properties, property)
		}
		for _, action := range service.Actions {
			entry.Actions = append(entry.Actions, miotdriver.ActionDefinition{
				IID: action.IID, Name: action.Name, Description: action.Description,
				Type: action.Type, In: append([]int(nil), action.In...), Out: append([]int(nil), action.Out...),
			})
		}
		definition.Services = append(definition.Services, entry)
	}

	discovered := miotdriver.DiscoverControls(definition)
	controls := make([]DeviceControl, 0, len(discovered))
	for _, item := range discovered {
		control := DeviceControl{
			ID: item.ID, Kind: item.Kind, Name: item.Name, Description: item.Description,
			SIID: item.SIID, PIID: item.PIID, AIID: item.AIID, Format: item.Format,
		}
		for _, option := range item.Options {
			control.Options = append(control.Options, DeviceControlOption{Value: option.Value, Label: option.Label})
		}
		for _, input := range item.Inputs {
			converted := DeviceControlInput{Name: input.Name, Format: input.Format}
			for _, option := range input.Options {
				converted.Options = append(converted.Options, DeviceControlOption{Value: option.Value, Label: option.Label})
			}
			control.Inputs = append(control.Inputs, converted)
		}
		controls = append(controls, control)
	}
	return controls
}

func miotReadDeviceRefs(spec miotSpecDocument) []miotDevicePropertyRef {
	refs := make([]miotDevicePropertyRef, 0, 16)
	seen := map[string]bool{}
	for _, service := range spec.Services {
		for _, prop := range service.Properties {
			readable := false
			for _, access := range prop.Access {
				if strings.EqualFold(access, "read") {
					readable = true
					break
				}
			}
			if !readable {
				continue
			}
			key := fmt.Sprintf("%d:%d", service.IID, prop.IID)
			if seen[key] {
				continue
			}
			field, scale := miotEnergyPropertyField(prop.Name, prop.Description, prop.Type, prop.Unit)
			if field != "" {
				label := map[string]string{"power": "实时功率", "voltage": "电压", "current": "电流", "today": "今日用电", "week": "本周用电", "month": "本月用电", "year": "本年用电", "total": "累计用电"}[field]
				unit := "kWh"
				if field == "power" {
					unit = "W"
				}
				if field == "voltage" {
					unit = "V"
				}
				if field == "current" {
					unit = "A"
				}
				refs = append(refs, miotDevicePropertyRef{SIID: service.IID, PIID: prop.IID, Field: "energy:" + field, Label: label, Unit: unit, Scale: scale, Format: prop.Format})
				seen[key] = true
			} else if f, label, unit, scale := miotDisplayProperty(prop.Name, prop.Description, service.Name, prop.Unit); f != "" {
				refs = append(refs, miotDevicePropertyRef{SIID: service.IID, PIID: prop.IID, Field: f, Label: label, Unit: unit, Scale: scale, Format: prop.Format, ValueList: prop.ValueList})
				seen[key] = true
			} else if containsFold(prop.Access, "write") {
				// Read values for writable settings too, so enum selectors and
				// power switches reflect their real current state after sync.
				refs = append(refs, miotDevicePropertyRef{SIID: service.IID, PIID: prop.IID, Field: "control", Scale: 1, Format: prop.Format, ControlOnly: true, ValueList: prop.ValueList})
				seen[key] = true
			}
		}
	}
	priority := map[string]int{"energy:power": 0, "energy:voltage": 1, "energy:current": 2, "temperature": 0, "humidity": 1, "volume": 0, "playback": 1, "muted": 2, "motion": 0, "recording": 1, "storage": 2, "battery": 3, "illuminance": 3, "pm25": 3, "co2": 4, "on": 0}
	sort.SliceStable(refs, func(i, j int) bool {
		pi, ok := priority[refs[i].Field]
		if !ok {
			pi = 20
		}
		pj, ok := priority[refs[j].Field]
		if !ok {
			pj = 20
		}
		return pi < pj
	})
	if len(refs) > 24 {
		refs = refs[:24]
	}
	return refs
}

// xiaomiWritableSwitch resolves the actual writable on/off property from the
// MIoT model specification. Read-only power telemetry is never a command target.
func xiaomiWritableSwitch(spec miotSpecDocument) (miotdriver.SwitchProperty, bool) {
	services := make([]miotdriver.ServiceSpec, 0, len(spec.Services))
	for _, service := range spec.Services {
		entry := miotdriver.ServiceSpec{SIID: service.IID}
		for _, property := range service.Properties {
			entry.Properties = append(entry.Properties, miotdriver.PropertySpec{
				SIID: service.IID, PIID: property.IID, Name: property.Name,
				Description: property.Description, Type: property.Type,
				Format: property.Format, Access: property.Access,
			})
		}
		services = append(services, entry)
	}
	return miotdriver.FindWritableSwitch(services)
}

func miotMeterEligible(category string) bool {
	switch category {
	case "插座", "开关", "灯光", "空调", "风扇", "洗碗机", "冰箱", "空气净化器", "加湿器", "扫地机器人":
		return true
	default:
		return false
	}
}

func xiaomiCommandDevice(ctx context.Context, secret providerSecret, did, model, command string) error {
	var enabled bool
	switch command {
	case "turn_on":
		enabled = true
	case "turn_off":
		enabled = false
	default:
		return errors.New("小米 MIoT 仅支持开启和关闭命令")
	}
	if strings.TrimSpace(did) == "" || strings.TrimSpace(model) == "" {
		return errors.New("小米设备缺少 did 或型号，无法匹配 MIoT 控制属性")
	}
	typ, err := fetchMIoTModelType(ctx, model)
	if err != nil || typ == "" {
		if err == nil {
			err = errors.New("MIoT 型号规格未收录")
		}
		return fmt.Errorf("读取小米设备控制规格失败：%w", err)
	}
	spec, err := fetchMIoTSpec(ctx, typ)
	if err != nil {
		return fmt.Errorf("下载小米设备控制规格失败：%w", err)
	}
	property, ok := xiaomiWritableSwitch(spec)
	if !ok {
		return errors.New("该小米设备的 MIoT 规格没有可写开关属性，不能控制")
	}
	payload, err := miotdriver.BuildSetPropertyPayload(did, property, enabled)
	if err != nil {
		return err
	}
	body, err := xiaomiEncryptedPost(ctx, secret, "/miotspec/prop/set", payload)
	if err != nil {
		return fmt.Errorf("发送小米 MIoT 开关命令失败：%w", err)
	}
	if err := miotdriver.ValidateSetPropertyResponse(body); err != nil {
		return err
	}
	return nil
}

func xiaomiSetMIoTProperty(ctx context.Context, secret providerSecret, did string, siid, piid int, value any) error {
	if strings.TrimSpace(did) == "" || siid <= 0 || piid <= 0 {
		return errors.New("小米 MIoT 属性标识无效")
	}
	payload, err := json.Marshal(map[string]any{"params": []map[string]any{{
		"did": did, "siid": siid, "piid": piid, "value": value,
	}}})
	if err != nil {
		return err
	}
	body, err := xiaomiEncryptedPost(ctx, secret, "/miotspec/prop/set", payload)
	if err != nil {
		return fmt.Errorf("发送小米 MIoT 属性写入失败：%w", err)
	}
	if err := miotdriver.ValidateSetPropertyResponse(body); err != nil {
		return err
	}
	return nil
}

func xiaomiCallMIoTAction(ctx context.Context, secret providerSecret, did string, siid, aiid int, params []any) error {
	payload, err := miotdriver.BuildActionPayload(did, siid, aiid, params)
	if err != nil {
		return err
	}
	body, err := xiaomiEncryptedPost(ctx, secret, "/miotspec/action", payload)
	if err != nil {
		return fmt.Errorf("发送小米 MIoT 动作失败：%w", err)
	}
	if err := miotdriver.ValidateActionResponse(body); err != nil {
		return err
	}
	return nil
}

func guessCategoryWithMIoT(name, model, typ string) string {
	base := guessCategory(name, model)
	t := strings.ToLower(name + " " + model + " " + typ)
	// User-facing names such as "总插座" / "小冰箱" are not reliable product
	// categories. The explicit names below are treated as smart plugs, and the
	// MIoT model/type is authoritative whenever it identifies a plug/outlet.
	nameLower := strings.ToLower(strings.TrimSpace(name))
	modelType := strings.ToLower(strings.TrimSpace(model + " " + typ))
	if strings.Contains(nameLower, "总插座") || strings.Contains(nameLower, "小冰箱") || strings.Contains(modelType, "plug") || strings.Contains(modelType, "outlet") || strings.Contains(modelType, "socket") || strings.Contains(modelType, "air-conditioner-outlet") {
		return "插座"
	}
	switch {
	case strings.Contains(t, "camera"), strings.Contains(t, "video-doorbell"), strings.Contains(t, "cateye"), strings.Contains(t, "摄像"), strings.Contains(t, "门铃"):
		return "摄像头"
	case strings.Contains(t, "intelligent-speaker"), strings.Contains(t, "wifispeaker"), strings.Contains(t, "speaker"), strings.Contains(t, "小爱"), strings.Contains(t, "音箱"):
		return "音箱"
	case strings.Contains(t, "temperature-humidity-sensor"), strings.Contains(t, "sensor_ht"), strings.Contains(t, "thermometer"), strings.Contains(t, "温湿度计"), strings.Contains(t, "温湿度传感器"):
		return "温湿度计"
	case strings.Contains(t, "motion-sensor"), strings.Contains(t, "magnet-sensor"), strings.Contains(t, "vibration"), strings.Contains(t, "sensor_smoke"), strings.Contains(t, "flood"), strings.Contains(t, "人体传感器"), strings.Contains(t, "门窗传感器"):
		return "传感器"
	case strings.Contains(t, "acpartner"), strings.Contains(t, "空调伴侣"), strings.Contains(t, "空调伴侣插座"):
		return "插座"
	case strings.Contains(t, "air-conditioner"), strings.Contains(t, "aircondition"), strings.Contains(t, "空调"):
		return "空调"
	case strings.Contains(t, "vacuum"), strings.Contains(t, "扫地机器人"), strings.Contains(t, "扫拖机器人"):
		return "扫地机器人"
	case strings.Contains(t, "fan"), strings.Contains(t, "风扇"):
		return "风扇"
	case strings.Contains(t, "television"), strings.Contains(t, "tvbox"), strings.Contains(t, "projector"), strings.Contains(t, "电视"), strings.Contains(t, "显示器"):
		return "影音设备"
	case strings.Contains(t, "dishwasher"), strings.Contains(t, "洗碗机"):
		return "洗碗机"
	case strings.Contains(t, "fridge"), strings.Contains(t, "refrigerator"), strings.Contains(t, "冰箱"):
		return "冰箱"
	case strings.Contains(t, "humidifier"), strings.Contains(t, "dehumidifier"), strings.Contains(t, "加湿器"), strings.Contains(t, "除湿机"):
		return "加湿器"
	case strings.Contains(t, "air-purifier"), strings.Contains(t, "airpurifier"), strings.Contains(t, "空气净化"):
		return "空气净化器"
	case strings.Contains(t, "lock"), strings.Contains(t, "door-lock"), strings.Contains(t, "门锁"):
		return "智能门锁"
	case strings.Contains(t, "curtain"), strings.Contains(t, "窗帘"):
		return "窗帘"
	case strings.Contains(t, "watch"), strings.Contains(t, "band"), strings.Contains(t, "手环"), strings.Contains(t, "手表"):
		return "穿戴设备"
	case strings.Contains(t, "router"), strings.Contains(t, "路由器"):
		return "网络设备"
	case strings.Contains(t, "plug"), strings.Contains(t, "socket"), strings.Contains(t, "outlet"), strings.Contains(t, "插座"), strings.Contains(t, "插排"):
		return "插座"
	case base != "其他":
		return base
	default:
		return "其他"
	}
}

func miotFormatAttribute(ref miotDevicePropertyRef, raw any) string {
	if raw == nil {
		return ""
	}
	for _, item := range ref.ValueList {
		if fmt.Sprint(item.Value) == fmt.Sprint(raw) && strings.TrimSpace(item.Description) != "" {
			return item.Description
		}
	}
	if b, ok := raw.(bool); ok {
		if ref.Field == "muted" {
			if b {
				return "已静音"
			}
			return "未静音"
		}
		if ref.Field == "motion_enabled" {
			if b {
				return "已开启"
			}
			return "已关闭"
		}
		if ref.Field == "motion" {
			if b {
				return "检测到活动"
			}
			return "未检测到活动"
		}
		if ref.Field == "contact" {
			if b {
				return "打开"
			}
			return "关闭"
		}
		if ref.Field == "waterleak" || ref.Field == "alarm" {
			if b {
				return "报警"
			}
			return "正常"
		}
		if ref.Field == "playback" {
			if b {
				return "播放中"
			}
			return "未播放"
		}
		if b {
			return "开启"
		}
		return "关闭"
	}
	if n := numericMetric(raw); n != nil {
		v := *n * ref.Scale
		// Use shortest round-trippable formatting. Trimming zeroes from a
		// fixed-point integer would turn values such as 100 into 1.
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}

func xiaomiPopulateDeviceDetails(ctx context.Context, secret providerSecret, devices []Device) {
	xiaomiPopulateDeviceDetailsWithOptions(ctx, secret, devices, true, false)
}

func xiaomiPopulateDeviceDetailsWithOptions(ctx context.Context, secret providerSecret, devices []Device, includeEnergyStats, onlineOnly bool) {
	if len(devices) == 0 {
		return
	}
	// Warm the model index once before per-device lookups. If the index host
	// is unreachable, stop here rather than retrying the same download for
	// every device in the account.
	for i := range devices {
		if strings.TrimSpace(devices[i].Model) != "" {
			_, _ = fetchMIoTModelType(ctx, devices[i].Model)
			break
		}
	}
	miotSpecCache.Lock()
	indexReady := !miotSpecCache.indexAt.IsZero()
	miotSpecCache.Unlock()
	if !indexReady {
		for i := range devices {
			devices[i].Category = guessCategoryWithMIoT(devices[i].Name, devices[i].Model, "")
		}
		return
	}
	types := make([]string, len(devices))
	for i := range devices {
		if strings.TrimSpace(devices[i].Model) == "" {
			continue
		}
		types[i], _ = fetchMIoTModelType(ctx, devices[i].Model)
		if types[i] != "" {
			devices[i].Category = guessCategoryWithMIoT(devices[i].Name, devices[i].Model, types[i])
		} else {
			devices[i].Category = guessCategoryWithMIoT(devices[i].Name, devices[i].Model, "")
		}
	}
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i := range devices {
		if types[i] == "" || devices[i].RemoteID == "" || (onlineOnly && !devices[i].Online) {
			continue
		}
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			deviceCtx, cancel := context.WithTimeout(ctx, 9*time.Second)
			defer cancel()
			spec, err := fetchMIoTSpec(deviceCtx, types[index])
			if err != nil {
				return
			}
			devices[index].Controls = xiaomiBuildDeviceControls(spec)
			if writable, ok := xiaomiWritableSwitch(spec); ok && writable.SIID > 0 && writable.PIID > 0 {
				devices[index].CanControl = true
			}
			if len(devices[index].Controls) > 0 {
				devices[index].CanControl = true
			}
			refs := miotReadDeviceRefs(spec)
			if !miotMeterEligible(devices[index].Category) {
				filtered := refs[:0]
				for _, ref := range refs {
					if !strings.HasPrefix(ref.Field, "energy:") {
						filtered = append(filtered, ref)
					}
				}
				refs = filtered
			}
			if len(refs) > 0 {
				params := make([]map[string]any, 0, len(refs))
				for _, ref := range refs {
					params = append(params, map[string]any{"did": devices[index].RemoteID, "siid": ref.SIID, "piid": ref.PIID})
				}
				payload, _ := json.Marshal(map[string]any{"params": params})
				body, readErr := xiaomiEncryptedPost(deviceCtx, secret, "/miotspec/prop/get", payload)
				if readErr == nil {
					applyMIoTDeviceResult(&devices[index], refs, body)
				}
			}
			// The daily/weekly history API uses model-specific statistic keys;
			// it is independent of the live MIoT property read above.
			if includeEnergyStats && miotMeterEligible(devices[index].Category) {
				today, week, _ := xiaomiFetchEnergyStats(deviceCtx, secret, devices[index].RemoteID, devices[index].Model, time.Now())
				if today != nil || week != nil {
					if devices[index].Energy == nil {
						devices[index].Energy = &EnergyMetrics{Supported: true, Source: "小米云端统计"}
					}
					devices[index].Energy.Supported = true
					if today != nil {
						devices[index].Energy.TodayKWh = today
					}
					if week != nil {
						devices[index].Energy.WeekKWh = week
					}
					devices[index].Energy.Source = "小米 MIoT / 云端统计"
					devices[index].Energy.UpdatedAt = time.Now()
				}
			}
		}(i)
	}
	wg.Wait()
}

func applyMIoTDeviceResult(device *Device, refs []miotDevicePropertyRef, body []byte) {
	energy := &EnergyMetrics{Supported: false, Source: "小米 MIoT", UpdatedAt: time.Now()}
	for _, ref := range refs {
		if strings.HasPrefix(ref.Field, "energy:") {
			energy.Supported = true
			break
		}
	}
	if energy.Supported {
		device.Energy = energy
	}
	var env struct {
		Code   int `json:"code"`
		Result []struct {
			DID   string `json:"did"`
			SIID  int    `json:"siid"`
			PIID  int    `json:"piid"`
			Code  int    `json:"code"`
			Value any    `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &env) != nil || env.Code != 0 {
		return
	}
	refMap := make(map[string]miotDevicePropertyRef, len(refs))
	for _, ref := range refs {
		refMap[fmt.Sprintf("%d:%d", ref.SIID, ref.PIID)] = ref
	}
	attrs := make([]DeviceAttribute, 0, len(refs))
	for _, item := range env.Result {
		if item.Code != 0 || (item.DID != "" && device.RemoteID != "" && item.DID != device.RemoteID) {
			continue
		}
		ref, ok := refMap[fmt.Sprintf("%d:%d", item.SIID, item.PIID)]
		if !ok {
			continue
		}
		for index := range device.Controls {
			control := &device.Controls[index]
			if control.Kind != "property" || control.SIID != item.SIID || control.PIID != item.PIID {
				continue
			}
			control.Value = item.Value
			// Some MIoT specifications name the writable power flag relay_state,
			// switch_state, etc. The discovered control label is more reliable
			// than relying only on a normalized property field name.
			if control.Name == "电源开关" {
				if enabled, ok := item.Value.(bool); ok {
					device.Power = &enabled
				}
			}
		}
		if strings.HasPrefix(ref.Field, "energy:") {
			energy.Supported = true
			if n := numericMetric(item.Value); n != nil {
				v := *n * ref.Scale
				switch strings.TrimPrefix(ref.Field, "energy:") {
				case "power":
					energy.PowerW = &v
				case "voltage":
					energy.VoltageV = &v
				case "current":
					energy.CurrentA = &v
				case "today":
					energy.TodayKWh = &v
				case "week":
					energy.WeekKWh = &v
				case "month":
					energy.MonthKWh = &v
				case "year":
					energy.YearKWh = &v
				case "total":
					energy.TotalKWh = &v
				}
			}
			continue
		}
		if ref.ControlOnly {
			continue
		}
		value := miotFormatAttribute(ref, item.Value)
		if value == "" {
			continue
		}
		attrs = append(attrs, DeviceAttribute{Key: ref.Field, Label: ref.Label, Value: value, Unit: ref.Unit})
		if ref.Field == "on" {
			if b, ok := item.Value.(bool); ok {
				device.Power = &b
			}
		}
	}
	if energy.Supported {
		device.Energy = energy
	}
	device.Attributes = attrs
	device.Value = summarizeDeviceAttributes(device)
}

func summarizeDeviceAttributes(device *Device) string {
	if len(device.Attributes) == 0 {
		return ""
	}
	preferred := map[string]int{"temperature": 1, "humidity": 2, "power": 1, "volume": 1, "playback": 2, "muted": 3, "motion": 4, "motion_enabled": 2, "battery": 3, "illuminance": 3, "pm25": 3, "co2": 4, "brightness": 3, "on": 1}
	attrs := append([]DeviceAttribute{}, device.Attributes...)
	sort.SliceStable(attrs, func(i, j int) bool {
		pi, okI := preferred[attrs[i].Key]
		if !okI {
			pi = 99
		}
		pj, okJ := preferred[attrs[j].Key]
		if !okJ {
			pj = 99
		}
		return pi < pj
	})
	parts := make([]string, 0, 2)
	for _, a := range attrs {
		if len(parts) >= 2 {
			break
		}
		part := a.Label + " " + a.Value
		if a.Unit != "" {
			part += a.Unit
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " · ")
}

func xiaomiAPIBase(region string) string {
	switch strings.ToLower(region) {
	case "us":
		return "https://us.api.io.mi.com"
	case "sg":
		return "https://sg.api.io.mi.com"
	case "de", "eu":
		return "https://de.api.io.mi.com"
	case "ru":
		return "https://ru.api.io.mi.com"
	case "tw":
		return "https://tw.api.io.mi.com"
	case "in":
		return "https://in.api.io.mi.com"
	default:
		return "https://api.io.mi.com"
	}
}

func xiaomiOrderedAPIFormBody(encData, encRC4Hash, signature, ssecurity, nonce string) string {
	return strings.Join([]string{
		"data=" + url.QueryEscape(encData),
		"rc4_hash__=" + url.QueryEscape(encRC4Hash),
		"signature=" + url.QueryEscape(signature),
		"ssecurity=" + url.QueryEscape(ssecurity),
		"_nonce=" + url.QueryEscape(nonce),
	}, "&")
}

func xiaomiSHA1SignFields(method, p string, fields []string, signed string) string {
	parts := make([]string, 0, len(fields)+3)
	parts = append(parts, strings.ToUpper(method), p)
	parts = append(parts, fields...)
	parts = append(parts, signed)
	sum := sha1.Sum([]byte(strings.Join(parts, "&")))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func xiaomiRC4Encode(key []byte, s string) (string, error) {
	b, e := xiaomiRC4Bytes(key, []byte(s))
	if e != nil {
		return "", e
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func xiaomiRC4Decode(key []byte, s string) ([]byte, error) {
	b, e := base64.StdEncoding.DecodeString(s)
	if e != nil {
		return nil, e
	}
	return xiaomiRC4Bytes(key, b)
}

func xiaomiRC4Bytes(key, data []byte) ([]byte, error) {
	c, e := rc4.NewCipher(key)
	if e != nil {
		return nil, e
	}
	drop := make([]byte, 1024)
	c.XORKeyStream(drop, drop)
	out := make([]byte, len(data))
	c.XORKeyStream(out, data)
	return out, nil
}
