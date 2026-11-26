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
	devices, err := xiaomiFetchDevicesWithOptions(ctx, s, false, true)
	// A transport can occasionally return context.Canceled even though the
	// poll's parent context is still alive (for example, a stale pooled
	// connection being torn down). Retry exactly once with a fresh bounded
	// context. A page lease release no longer cancels this in-flight context;
	// an actual application shutdown still does.
	if errors.Is(err, context.Canceled) && ctx.Err() == nil {
		retryCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
		defer cancel()
		return xiaomiFetchDevicesWithOptions(retryCtx, s, false, true)
	}
	return devices, err
}

// parseXiaomiHomeRoomNames mirrors Home Assistant's room enrichment logic.
// Xiaomi returns the actual display names in homelist[].roomlist[].name and
// the device IDs in roomlist[].dids. IDs may be strings or numbers depending
// on the account/device generation, so they are normalized through stringValue.
func parseXiaomiHomeRoomNames(body []byte) (map[string]string, error) {
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("解析小米家庭房间列表失败：%w", err)
	}
	if code := intValue(envelope["code"], 0); code != 0 {
		message := firstNonEmpty(stringValue(envelope["message"]), "无详细信息")
		return nil, fmt.Errorf("小米家庭房间接口错误 %d：%s", code, message)
	}
	result, _ := envelope["result"].(map[string]any)
	if result == nil {
		return nil, fmt.Errorf("小米家庭房间接口缺少 result：%s", firstNonEmpty(stringValue(envelope["message"]), "无详细信息"))
	}

	rooms := make(map[string]string)
	addDIDs := func(dids any, name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		items, ok := dids.([]any)
		if !ok {
			return
		}
		for _, rawDID := range items {
			if did := strings.TrimSpace(stringValue(rawDID)); did != "" {
				rooms[did] = name
			}
		}
	}
	addHomeList := func(rawHomes any) {
		homes, ok := rawHomes.([]any)
		if !ok {
			return
		}
		for _, rawHome := range homes {
			home, ok := rawHome.(map[string]any)
			if !ok {
				continue
			}
			// The official Xiaomi Home integration treats home-level DIDs
			// as belonging to the home itself. Room-level DIDs below take
			// precedence because they are more specific.
			addDIDs(home["dids"], stringValue(home["name"]))
			roomList, _ := home["roomlist"].([]any)
			for _, rawRoom := range roomList {
				room, ok := rawRoom.(map[string]any)
				if !ok {
					continue
				}
				addDIDs(room["dids"], stringValue(room["name"]))
			}
		}
	}

	// hass-xiaomi-miot builds result.devices[did] metadata from each room
	// list. Accept that same pre-merged map as well, which also makes this
	// parser compatible with Xiaomi responses that already include it.
	if deviceMap, ok := result["devices"].(map[string]any); ok {
		for did, rawDevice := range deviceMap {
			if device, ok := rawDevice.(map[string]any); ok {
				if name := strings.TrimSpace(stringValue(device["room_name"])); name != "" {
					rooms[strings.TrimSpace(did)] = name
				}
			}
		}
	}
	// The main and shared-home lists both carry roomlist[].dids. Processing
	// both mirrors Home Assistant's account/home handling.
	addHomeList(result["homelist"])
	addHomeList(result["share_home_list"])

	// Some firmware/API variants expose devices as a flat list rather than
	// an object keyed by DID.
	if deviceList, ok := result["device_info"].([]any); ok {
		for _, rawDevice := range deviceList {
			if device, ok := rawDevice.(map[string]any); ok {
				did := strings.TrimSpace(stringValue(device["did"]))
				name := strings.TrimSpace(stringValue(device["room_name"]))
				if did != "" && name != "" {
					rooms[did] = name
				}
			}
		}
	}
	return rooms, nil
}

// xiaomiHomeRoomMeta retains the home/room IDs from gethome. The official
// Xiaomi Home integration's overflow endpoint returns IDs and DID lists, but
// may omit display names; the names must be joined back to this index.
type xiaomiHomeRoomMeta struct {
	Name  string
	Rooms map[string]string
}

func parseXiaomiHomeRoomIndex(body []byte) (map[string]xiaomiHomeRoomMeta, error) {
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("解析小米家庭索引失败：%w", err)
	}
	if code := intValue(envelope["code"], 0); code != 0 {
		return nil, fmt.Errorf("小米家庭索引接口错误 %d：%s", code, firstNonEmpty(stringValue(envelope["message"]), "无详细信息"))
	}
	result, _ := envelope["result"].(map[string]any)
	if result == nil {
		return nil, fmt.Errorf("小米家庭索引接口缺少 result：%s", firstNonEmpty(stringValue(envelope["message"]), "无详细信息"))
	}

	index := make(map[string]xiaomiHomeRoomMeta)
	for _, source := range []string{"homelist", "share_home_list"} {
		homes, _ := result[source].([]any)
		for _, rawHome := range homes {
			home, ok := rawHome.(map[string]any)
			if !ok {
				continue
			}
			homeID := strings.TrimSpace(stringValue(home["id"]))
			if homeID == "" {
				continue
			}
			meta := index[homeID]
			meta.Name = strings.TrimSpace(stringValue(home["name"]))
			if meta.Rooms == nil {
				meta.Rooms = make(map[string]string)
			}
			rooms, _ := home["roomlist"].([]any)
			for _, rawRoom := range rooms {
				room, ok := rawRoom.(map[string]any)
				if !ok {
					continue
				}
				roomID := strings.TrimSpace(stringValue(room["id"]))
				roomName := strings.TrimSpace(stringValue(room["name"]))
				if roomID != "" && roomName != "" {
					meta.Rooms[roomID] = roomName
				}
			}
			index[homeID] = meta
		}
	}
	return index, nil
}

// parseXiaomiRoomPage mirrors the official Xiaomi Home integration's
// get_dev_room_page merge: page records contain home/room IDs and DIDs, while
// display names come from the initial gethome room index.
func parseXiaomiRoomPage(body []byte, homeIndex map[string]xiaomiHomeRoomMeta) (map[string]string, string, bool, error) {
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, "", false, fmt.Errorf("解析小米分页房间列表失败：%w", err)
	}
	if code := intValue(envelope["code"], 0); code != 0 {
		return nil, "", false, fmt.Errorf("小米分页房间接口错误 %d：%s", code, firstNonEmpty(stringValue(envelope["message"]), "无详细信息"))
	}
	result, _ := envelope["result"].(map[string]any)
	if result == nil {
		return nil, "", false, fmt.Errorf("小米分页房间接口缺少 result：%s", firstNonEmpty(stringValue(envelope["message"]), "无详细信息"))
	}

	rooms := make(map[string]string)
	infos, _ := result["info"].([]any)
	for _, rawHome := range infos {
		home, ok := rawHome.(map[string]any)
		if !ok {
			continue
		}
		homeID := strings.TrimSpace(stringValue(home["id"]))
		meta := homeIndex[homeID]

		// Devices listed directly under a home (rather than a room) are
		// represented by the home name in the official Xiaomi Home model.
		if meta.Name != "" {
			if dids, ok := home["dids"].([]any); ok {
				for _, rawDID := range dids {
					if did := strings.TrimSpace(stringValue(rawDID)); did != "" {
						rooms[did] = meta.Name
					}
				}
			}
		}

		roomList, _ := home["roomlist"].([]any)
		for _, rawRoom := range roomList {
			room, ok := rawRoom.(map[string]any)
			if !ok {
				continue
			}
			roomID := strings.TrimSpace(stringValue(room["id"]))
			roomName := firstNonEmpty(stringValue(room["name"]), meta.Rooms[roomID])
			dids, _ := room["dids"].([]any)
			for _, rawDID := range dids {
				if did := strings.TrimSpace(stringValue(rawDID)); did != "" && roomName != "" {
					rooms[did] = roomName
				}
			}
		}
	}
	return rooms, strings.TrimSpace(stringValue(result["max_id"])), boolValue(result["has_more"]), nil
}

func countMatchedXiaomiRoomNames(roomNames map[string]string, dids []string) int {
	count := 0
	for _, did := range dids {
		if strings.TrimSpace(roomNames[strings.TrimSpace(did)]) != "" {
			count++
		}
	}
	return count
}

func xiaomiRoomNamesCoverDevices(roomNames map[string]string, dids []string) bool {
	if len(dids) == 0 {
		return false
	}
	for _, did := range dids {
		if strings.TrimSpace(roomNames[strings.TrimSpace(did)]) == "" {
			return false
		}
	}
	return true
}

func mergeMissingXiaomiRoomNames(dst, src map[string]string) {
	for did, name := range src {
		if strings.TrimSpace(dst[did]) == "" && strings.TrimSpace(name) != "" {
			dst[did] = name
		}
	}
}

func xiaomiFetchHomeRoomNames(ctx context.Context, secret providerSecret, requiredDIDs []string) (map[string]string, error) {
	// First use the endpoint implemented by hass-xiaomi-miot. Give this
	// metadata request more time than a small property read; it is only made
	// during explicit account sync, never during the active-page state poll.
	mergedPayload := []byte(`{"fg":true,"fetch_share":true,"fetch_share_dev":true,"fetch_cariot":true,"limit":300,"app_ver":7,"plat_form":0}`)
	mergedCtx, cancelMerged := context.WithTimeout(ctx, 22*time.Second)
	mergedBody, mergedErr := xiaomiEncryptedPostWithTimeout(mergedCtx, secret, "/v2/homeroom/gethome_merged", mergedPayload, 24*time.Second)
	cancelMerged()
	mergedNames := make(map[string]string)
	if mergedErr == nil {
		if names, parseErr := parseXiaomiHomeRoomNames(mergedBody); parseErr == nil {
			mergeMissingXiaomiRoomNames(mergedNames, names)
			if xiaomiRoomNamesCoverDevices(mergedNames, requiredDIDs) {
				return mergedNames, nil
			}
			mergedErr = fmt.Errorf("gethome_merged 仅匹配到 %d/%d 台待补充分区设备", countMatchedXiaomiRoomNames(mergedNames, requiredDIDs), len(requiredDIDs))
		} else {
			mergedErr = parseErr
		}
	}

	// Fallback to the pagination flow used by the official Xiaomi Home
	// integration: gethome -> get_dev_room_page. Keep the first page's room
	// names and use its home/room IDs to resolve names in subsequent pages.
	getHomePayload := []byte(`{"limit":150,"fetch_share":true,"fetch_share_dev":true,"plat_form":0,"app_ver":9}`)
	getHomeCtx, cancelGetHome := context.WithTimeout(ctx, 18*time.Second)
	getHomeBody, getHomeErr := xiaomiEncryptedPostWithTimeout(getHomeCtx, secret, "/v2/homeroom/gethome", getHomePayload, 20*time.Second)
	cancelGetHome()
	if getHomeErr != nil {
		return mergedNames, fmt.Errorf("读取小米家庭房间信息失败：gethome_merged：%v；gethome：%w", mergedErr, getHomeErr)
	}
	getHomeNames, parseErr := parseXiaomiHomeRoomNames(getHomeBody)
	if parseErr != nil {
		return mergedNames, fmt.Errorf("解析小米家庭房间信息失败：gethome_merged：%v；gethome：%w", mergedErr, parseErr)
	}
	mergeMissingXiaomiRoomNames(mergedNames, getHomeNames)
	homeIndex, indexErr := parseXiaomiHomeRoomIndex(getHomeBody)
	if indexErr != nil {
		return mergedNames, fmt.Errorf("解析小米家庭/房间 ID 索引失败：%w", indexErr)
	}

	var pageEnvelope map[string]any
	if err := json.Unmarshal(getHomeBody, &pageEnvelope); err != nil {
		return mergedNames, nil
	}
	result, _ := pageEnvelope["result"].(map[string]any)
	if result == nil || !boolValue(result["has_more"]) {
		return mergedNames, nil // A valid empty mapping may mean no room is assigned.
	}
	startID := strings.TrimSpace(stringValue(result["max_id"]))
	for page := 0; page < 8 && startID != ""; page++ {
		pagePayload, _ := json.Marshal(map[string]any{"start_id": startID, "limit": 150})
		pageCtx, cancelPage := context.WithTimeout(ctx, 15*time.Second)
		pageBody, pageErr := xiaomiEncryptedPostWithTimeout(pageCtx, secret, "/v2/homeroom/get_dev_room_page", pagePayload, 17*time.Second)
		cancelPage()
		if pageErr != nil {
			return mergedNames, fmt.Errorf("小米房间分页查询失败：%w", pageErr)
		}
		pageNames, nextID, hasMore, pageParseErr := parseXiaomiRoomPage(pageBody, homeIndex)
		if pageParseErr != nil {
			return mergedNames, pageParseErr
		}
		mergeMissingXiaomiRoomNames(mergedNames, pageNames)
		if !hasMore || nextID == "" || nextID == startID {
			startID = ""
			break
		}
		startID = nextID
	}
	if startID != "" {
		return mergedNames, errors.New("小米房间分页超过 8 页，已保留已取得的房间信息")
	}
	return mergedNames, nil
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
	roomNames := map[string]string{}
	roomIssue := ""
	if !onlineOnly {
		missingRoomDIDs := make([]string, 0, len(envelope.Result.List))
		for _, item := range envelope.Result.List {
			if strings.TrimSpace(item.RoomName) == "" {
				missingRoomDIDs = append(missingRoomDIDs, item.DID)
			}
		}
		if len(missingRoomDIDs) > 0 {
			// Room metadata is fetched only during explicit account sync, not the
			// active-page live state poll. A room lookup failure must not block
			// device discovery or invalidate the saved Xiaomi login session, but
			// preserve the reason for the in-app sync log.
			names, roomErr := xiaomiFetchHomeRoomNames(ctx, s, missingRoomDIDs)
			if names != nil {
				roomNames = names
			}
			if roomErr != nil {
				roomIssue = sanitizeIntegrationLog(roomErr.Error())
			}
		}
	}
	out := make([]Device, 0, len(envelope.Result.List))
	for _, x := range envelope.Result.List {
		if x.DID == "" {
			continue
		}
		online := boolValue(x.Online)
		cat := guessCategory(x.Name, x.Model)
		room := firstNonEmpty(x.RoomName, roomNames[x.DID], "未分区")
		out = append(out, Device{ID: "xiaomi-" + safeID(x.DID), RemoteID: x.DID, Name: firstNonEmpty(x.Name, x.Model, x.DID), Brand: "小米", Model: x.Model, Room: room, Category: cat, Protocol: "xiaomi-cloud", Online: online, LastSeen: time.Now(), Demo: false, CreatedAt: time.Now(), roomIssue: roomIssue})
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

// lookupMIoTModelType tolerates model identifiers whose letter casing differs
// from the specification index, while preserving an exact-match fast path.
func lookupMIoTModelType(models map[string]string, model string) string {
	model = strings.TrimSpace(model)
	if typ := models[model]; typ != "" {
		return typ
	}
	for candidate, typ := range models {
		if strings.EqualFold(strings.TrimSpace(candidate), model) {
			return typ
		}
	}
	return ""
}

func fetchMIoTModelType(ctx context.Context, model string) (string, error) {
	model = strings.TrimSpace(model)
	miotSpecCache.Lock()
	indexFresh := !miotSpecCache.indexAt.IsZero() && time.Since(miotSpecCache.indexAt) < 24*time.Hour
	if indexFresh {
		typ := lookupMIoTModelType(miotSpecCache.models, model)
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
	typ := lookupMIoTModelType(models, model)
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
	return xiaomiEncryptedPostWithTimeout(ctx, s, pathPart, rawData, 18*time.Second)
}

func xiaomiEncryptedPostWithTimeout(ctx context.Context, s providerSecret, pathPart string, rawData []byte, timeout time.Duration) ([]byte, error) {
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
	client := &http.Client{Jar: jar, Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, xiaomiAPIStatusError(resp, body, s, "")
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
	SIID               int
	PIID               int
	ServiceName        string
	ServiceDescription string
	ServiceType        string
	Field              string
	Label              string
	Unit               string
	Scale              float64
	Format             string
	ControlOnly        bool
	ValueList          []struct {
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
func miotPowerStateName(normalized string) bool {
	switch normalized {
	case "on", "power", "switch", "powerstate", "switchstate", "switchstatus", "onoff", "ison", "relaystate", "outletstate", "plugstate", "poweron", "relayon":
		return true
	default:
		return false
	}
}

// miotIndicatorService distinguishes indicator/LED switches from the actual
// appliance relay. Some MIoT devices expose both as a boolean property named
// "on". Treating both as Device.Power lets the later LED result overwrite the
// real outlet state (for example chuangmi.plug.m3: switch.on is SIID 2/PIID 1,
// indicator-light.on is SIID 3/PIID 1).
func miotIndicatorService(name, description, serviceType string) bool {
	text := miotNormalizedName(strings.Join([]string{name, description, serviceType}, " "))
	for _, marker := range []string{"indicator", "indicatorlight", "statuslight", "led", "pilotlamp", "指示灯", "状态灯"} {
		if strings.Contains(text, miotNormalizedName(marker)) {
			return true
		}
	}
	return false
}

func miotStatePropertyField(serviceName, serviceDescription, serviceType, propertyName, propertyDescription, propertyType string) string {
	name := miotNormalizedName(propertyName)
	description := strings.ToLower(strings.TrimSpace(propertyDescription))
	propType := strings.ToLower(strings.TrimSpace(propertyType))
	if miotIndicatorService(serviceName, serviceDescription, serviceType) && miotPowerStateName(name) {
		return "indicator_light"
	}
	if miotPowerStateName(name) || strings.Contains(propType, ":property:on:") || strings.Contains(propType, ":property:switch:") || strings.Contains(propType, ":property:power:") || strings.Contains(description, "开关状态") || strings.Contains(description, "电源开关") || strings.Contains(description, "继电器状态") {
		return "on"
	}
	return ""
}

func miotBoolValue(raw any) (bool, bool) {
	switch value := raw.(type) {
	case bool:
		return value, true
	case float64:
		if value == 1 {
			return true, true
		}
		if value == 0 {
			return false, true
		}
	case int:
		if value == 1 {
			return true, true
		}
		if value == 0 {
			return false, true
		}
	case json.Number:
		if value == "1" {
			return true, true
		}
		if value == "0" {
			return false, true
		}
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "true", "1", "on", "yes", "open":
			return true, true
		case "false", "0", "off", "no", "closed":
			return false, true
		}
	}
	return false, false
}

func miotDisplayProperty(name, description, serviceName, serviceDescription, serviceType, unit string) (field, label, displayUnit string, scale float64) {
	n := miotNormalizedName(name)
	t := strings.ToLower(strings.Join([]string{name, description}, " "))
	service := strings.ToLower(strings.Join([]string{serviceName, serviceDescription, serviceType}, " "))
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
	// Several environmental sensors expose generic property names (value,
	// current or measurement). Unit and MIoT service identity are more reliable
	// than a generic name when deciding temperature versus humidity.
	genericValue := n == "value" || n == "current" || n == "measurement" || n == "sensorvalue"
	if genericValue {
		if u == "%" || strings.Contains(service, "humidity") || strings.Contains(service, "湿度") {
			return "humidity", "湿度", firstNonEmpty(u, "%"), 1
		}
		if u == "°C" || u == "°F" || strings.Contains(service, "temperature") || strings.Contains(service, "温度") {
			return "temperature", "温度", firstNonEmpty(u, "°C"), 1
		}
	}
	if u == "%" && (strings.Contains(service, "environment") || strings.Contains(service, "sensor") || strings.Contains(service, "环境")) {
		return "humidity", "湿度", "%", 1
	}
	if (u == "°C" || u == "°F") && (strings.Contains(service, "environment") || strings.Contains(service, "sensor") || strings.Contains(service, "环境")) {
		return "temperature", "温度", u, 1
	}
	if miotIndicatorService(serviceName, "", "") && miotPowerStateName(n) {
		return "indicator_light", "指示灯", "", 1
	}
	switch {
	case miotPowerStateName(n):
		return "on", "开关状态", "", 1
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
			displayField, displayLabel, displayUnit, displayScale := miotDisplayProperty(prop.Name, prop.Description, service.Name, service.Description, service.Type, prop.Unit)
			// Prefer explicit environmental semantics over the substring "current"
			// in names such as currentTemperature; otherwise these sensors can be
			// incorrectly classified as electrical current in amperes.
			if displayField == "temperature" || displayField == "humidity" || displayField == "color_temperature" {
				field, scale = displayField, displayScale
			}
			// Service identity matters: a device can expose both switch.on and
			// indicator-light.on. Only the actual switch state may drive Device.Power.
			if stateField := miotStatePropertyField(service.Name, service.Description, service.Type, prop.Name, prop.Description, prop.Type); stateField != "" {
				field = stateField
				scale = 1
			}
			if field == "on" || field == "indicator_light" {
				// Switch state and indicator LED can both be named "on" but
				// belong to different MIoT services and must not overwrite each other.
				label := "开关状态"
				if field == "indicator_light" {
					label = "指示灯"
				}
				refs = append(refs, miotDevicePropertyRef{SIID: service.IID, PIID: prop.IID, ServiceName: service.Name, ServiceDescription: service.Description, ServiceType: service.Type, Field: field, Label: label, Scale: 1, Format: prop.Format, ValueList: prop.ValueList})
				seen[key] = true
			} else if field == "power" || field == "voltage" || field == "current" || field == "today" || field == "week" || field == "month" || field == "year" || field == "total" {
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
				refs = append(refs, miotDevicePropertyRef{SIID: service.IID, PIID: prop.IID, ServiceName: service.Name, ServiceDescription: service.Description, ServiceType: service.Type, Field: "energy:" + field, Label: label, Unit: unit, Scale: scale, Format: prop.Format})
				seen[key] = true
			} else if displayField != "" && (field == "" || field == displayField) {
				refs = append(refs, miotDevicePropertyRef{SIID: service.IID, PIID: prop.IID, ServiceName: service.Name, ServiceDescription: service.Description, ServiceType: service.Type, Field: displayField, Label: displayLabel, Unit: displayUnit, Scale: displayScale, Format: prop.Format, ValueList: prop.ValueList})
				seen[key] = true
			} else if containsFold(prop.Access, "write") {
				// Read values for writable settings too, so enum selectors and
				// power switches reflect their real current state after sync.
				refs = append(refs, miotDevicePropertyRef{SIID: service.IID, PIID: prop.IID, ServiceName: service.Name, ServiceType: service.Type, Field: "control", Scale: 1, Format: prop.Format, ControlOnly: true, ValueList: prop.ValueList})
				seen[key] = true
			}
		}
	}
	priority := map[string]int{"energy:power": 0, "energy:voltage": 1, "energy:current": 2, "temperature": 0, "humidity": 1, "volume": 0, "playback": 1, "muted": 2, "motion": 0, "recording": 1, "storage": 2, "battery": 3, "illuminance": 3, "pm25": 3, "co2": 4, "on": 0, "indicator_light": 6}
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

// miotReadDeviceRefsForModel applies model-specific semantic overrides after
// parsing the public MIoT specification. Some older devices expose multiple
// boolean properties named "on". For chuangmi.plug.m3 the public specification
// defines switch.on (2/1) as the outlet relay and indicator-light.on (3/1) as
// the LED. Never infer the relay from the last boolean result returned by cloud.
func miotReadDeviceRefsForModel(spec miotSpecDocument, model string) []miotDevicePropertyRef {
	refs := miotReadDeviceRefs(spec)
	if !strings.EqualFold(strings.TrimSpace(model), "chuangmi.plug.m3") {
		return normalizeMIoTPowerRefs(refs)
	}
	for i := range refs {
		switch {
		case refs[i].SIID == 2 && refs[i].PIID == 1:
			refs[i].Field = "on"
			refs[i].Label = "开关状态"
			refs[i].Unit = ""
			refs[i].ControlOnly = false
		case refs[i].SIID == 3 && refs[i].PIID == 1:
			refs[i].Field = "indicator_light"
			refs[i].Label = "指示灯"
			refs[i].Unit = ""
			refs[i].ControlOnly = false
		case refs[i].Field == "on":
			// Any additional on-like property is display-only and must not
			// drive the outlet's main power state.
			refs[i].Field = fmt.Sprintf("service_on_%d_%d", refs[i].SIID, refs[i].PIID)
			refs[i].Label = firstNonEmpty(refs[i].ServiceDescription, refs[i].ServiceName, fmt.Sprintf("服务 %d", refs[i].SIID)) + "状态"
		}
	}
	return refs
}

// normalizeMIoTPowerRefs prevents ambiguous same-name boolean properties in
// different services from overwriting one another. A service explicitly named
// switch/outlet/relay has priority; indicator services are always separate.
func normalizeMIoTPowerRefs(refs []miotDevicePropertyRef) []miotDevicePropertyRef {
	indices := make([]int, 0, 2)
	for i := range refs {
		if refs[i].Field == "on" {
			indices = append(indices, i)
		}
	}
	if len(indices) <= 1 {
		return refs
	}
	best := -1
	bestScore := -1
	for _, idx := range indices {
		ref := refs[idx]
		if miotIndicatorService(ref.ServiceName, ref.ServiceDescription, ref.ServiceType) {
			refs[idx].Field = "indicator_light"
			refs[idx].Label = "指示灯"
			continue
		}
		text := miotNormalizedName(strings.Join([]string{ref.ServiceName, ref.ServiceDescription, ref.ServiceType}, " "))
		score := 0
		for _, marker := range []string{"switch", "outlet", "socket", "relay", "powerswitch"} {
			if strings.Contains(text, marker) {
				score += 10
			}
		}
		if score > bestScore {
			best, bestScore = idx, score
		}
	}
	if best < 0 {
		// No service can be identified as the primary relay. Keep only the
		// first stable candidate as power; label the rest as service states.
		for _, idx := range indices[1:] {
			if refs[idx].Field == "on" {
				refs[idx].Field = fmt.Sprintf("service_on_%d_%d", refs[idx].SIID, refs[idx].PIID)
				refs[idx].Label = firstNonEmpty(refs[idx].ServiceDescription, refs[idx].ServiceName, fmt.Sprintf("服务 %d", refs[idx].SIID)) + "状态"
			}
		}
		return refs
	}
	for _, idx := range indices {
		if idx == best || refs[idx].Field != "on" {
			continue
		}
		refs[idx].Field = fmt.Sprintf("service_on_%d_%d", refs[idx].SIID, refs[idx].PIID)
		refs[idx].Label = firstNonEmpty(refs[idx].ServiceDescription, refs[idx].ServiceName, fmt.Sprintf("服务 %d", refs[idx].SIID)) + "状态"
	}
	return refs
}

func miotLiveStateRefs(refs []miotDevicePropertyRef) []miotDevicePropertyRef {
	// Live view focuses on values users can see changing: power, environment,
	// activity, basic media and instantaneous electrical measurements. Historical
	// energy counters remain on the normal account-sync/statistics path.
	allowed := map[string]bool{
		"on": true, "indicator_light": true, "temperature": true, "humidity": true, "battery": true,
		"illuminance": true, "pressure": true, "pm25": true, "co2": true, "tvoc": true,
		"volume": true, "playback": true, "muted": true, "motion": true,
		"motion_enabled": true, "recording": true, "storage": true, "contact": true,
		"waterleak": true, "alarm": true, "brightness": true, "color_temperature": true,
		"energy:power": true, "energy:voltage": true, "energy:current": true,
	}
	out := make([]miotDevicePropertyRef, 0, len(refs))
	for _, ref := range refs {
		if allowed[ref.Field] || strings.HasPrefix(ref.Field, "service_on_") {
			out = append(out, ref)
		}
	}
	return out
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

// xiaomiWritableSwitchForModel resolves the physical outlet relay, not an
// auxiliary indicator LED. The public chuangmi.plug.m3 MIoT specification has
// two writable boolean properties named "on": switch.on (2/1) controls power,
// indicator-light.on (3/1) controls only the LED. Generic first-match lookup
// is unsafe for this model because service ordering can differ between specs.
func xiaomiWritableSwitchForModel(spec miotSpecDocument, model string) (miotdriver.SwitchProperty, bool) {
	if strings.EqualFold(strings.TrimSpace(model), "chuangmi.plug.m3") {
		for _, service := range spec.Services {
			if service.IID != 2 {
				continue
			}
			for _, property := range service.Properties {
				if property.IID != 1 {
					continue
				}
				definition := miotdriver.PropertyDefinition{
					IID: property.IID, Name: property.Name, Description: property.Description,
					Type: property.Type, Format: property.Format, Access: append([]string(nil), property.Access...),
				}
				if miotdriver.IsPowerSwitch(definition) {
					return miotdriver.SwitchProperty{SIID: 2, PIID: 1}, true
				}
			}
		}
		return miotdriver.SwitchProperty{}, false
	}
	return xiaomiWritableSwitch(spec)
}

func miotMeterEligible(category string) bool {
	switch category {
	case "插座", "开关", "灯光", "空调", "风扇", "洗碗机", "冰箱", "空气净化器", "加湿器", "扫地机器人":
		return true
	default:
		return false
	}
}

// parseMIoTPowerPropertyResponse validates the exact relay property readback.
// A successful prop/set response only acknowledges the write; it is not a
// trustworthy source for the device's current state.
func parseMIoTPowerPropertyResponse(body []byte, did string, siid, piid int) (bool, error) {
	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Result  []struct {
			DID     string `json:"did"`
			SIID    int    `json:"siid"`
			PIID    int    `json:"piid"`
			Code    int    `json:"code"`
			Message string `json:"message"`
			Value   any    `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return false, fmt.Errorf("解析 MIoT 电源状态响应失败：%w", err)
	}
	if envelope.Code != 0 {
		return false, fmt.Errorf("MIoT 电源状态读取业务错误 code=%d message=%s", envelope.Code, envelope.Message)
	}
	for _, item := range envelope.Result {
		if item.SIID != siid || item.PIID != piid || (item.DID != "" && did != "" && item.DID != did) {
			continue
		}
		if item.Code != 0 {
			return false, fmt.Errorf("MIoT 电源属性读取失败 code=%d message=%s", item.Code, item.Message)
		}
		if value, ok := miotBoolValue(item.Value); ok {
			return value, nil
		}
		return false, fmt.Errorf("MIoT 电源属性值不是布尔值：%v", item.Value)
	}
	return false, fmt.Errorf("MIoT 电源状态响应没有匹配 DID/SIID/PIID（%s/%d/%d）", did, siid, piid)
}

func xiaomiReadMIoTPowerState(ctx context.Context, secret providerSecret, did string, siid, piid int) (bool, error) {
	ref := miotDevicePropertyRef{SIID: siid, PIID: piid, Field: "on"}
	payload, err := xiaomiBuildGetPropertiesPayload(did, []miotDevicePropertyRef{ref})
	if err != nil {
		return false, err
	}
	body, err := xiaomiEncryptedPost(ctx, secret, "/miotspec/prop/get", payload)
	if err != nil {
		return false, fmt.Errorf("MIoT 写入后读取电源状态失败：%w", err)
	}
	return parseMIoTPowerPropertyResponse(body, did, siid, piid)
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
	property, ok := xiaomiWritableSwitchForModel(spec, model)
	if !ok {
		return errors.New("该小米设备的 MIoT 规格没有可写主电源开关属性，不能控制")
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
	if ref.Field == "on" || ref.Field == "indicator_light" {
		if enabled, ok := miotBoolValue(raw); ok {
			if enabled {
				return "开启"
			}
			return "关闭"
		}
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
	if onlineOnly {
		xiaomiPopulateLiveDeviceDetailsBatched(ctx, secret, devices)
		return
	}
	xiaomiPopulateDeviceDetailsLegacy(ctx, secret, devices, includeEnergyStats, onlineOnly)
}

// xiaomiPopulateLiveDeviceDetailsBatched reads live properties using the
// legacy xiaomiio session. The request is deliberately isolated per DID:
// some Xiaomi cloud responses omit DID from result rows, and a cross-device
// batch cannot safely map such rows back to their source device.
func xiaomiPopulateLiveDeviceDetailsBatched(ctx context.Context, secret providerSecret, devices []Device) {
	if len(devices) == 0 {
		return
	}

	// Warm the MIoT model index once. A successful fetch is shared by all
	// devices through miotSpecCache for the next 24 hours.
	indexErr := error(nil)
	for i := range devices {
		if strings.TrimSpace(devices[i].Model) == "" {
			continue
		}
		_, indexErr = fetchMIoTModelType(ctx, devices[i].Model)
		if indexErr != nil {
			devices[i].StateIssue = indexErr.Error()
		}
		break
	}
	miotSpecCache.Lock()
	indexReady := !miotSpecCache.indexAt.IsZero()
	miotSpecCache.Unlock()
	if !indexReady {
		for i := range devices {
			devices[i].Category = guessCategoryWithMIoT(devices[i].Name, devices[i].Model, "")
			if devices[i].Online && devices[i].StateIssue == "" {
				devices[i].StateIssue = "MIoT 规格索引暂不可用"
			}
		}
		return
	}

	types := make([]string, len(devices))
	for i := range devices {
		devices[i].Category = guessCategoryWithMIoT(devices[i].Name, devices[i].Model, "")
		if strings.TrimSpace(devices[i].Model) == "" {
			if devices[i].Online {
				devices[i].StateIssue = "设备没有 model，无法匹配 MIoT 规格"
			}
			continue
		}
		typ, err := fetchMIoTModelType(ctx, devices[i].Model)
		if err != nil {
			devices[i].StateIssue = err.Error()
			continue
		}
		types[i] = typ
		devices[i].Category = guessCategoryWithMIoT(devices[i].Name, devices[i].Model, typ)
	}

	type readPlan struct {
		refs []miotDevicePropertyRef
	}
	plans := make([]readPlan, len(devices))
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for i := range devices {
		if !devices[i].Online || devices[i].RemoteID == "" || types[i] == "" {
			continue
		}
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			deviceCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			spec, err := fetchMIoTSpec(deviceCtx, types[index])
			if err != nil {
				devices[index].StateIssue = "读取 MIoT 设备规格失败：" + err.Error()
				return
			}
			devices[index].Controls = xiaomiBuildDeviceControls(spec)
			if _, ok := xiaomiWritableSwitchForModel(spec, devices[index].Model); ok {
				devices[index].CanControl = true
			}
			if len(devices[index].Controls) > 0 {
				devices[index].CanControl = true
			}
			refs := miotLiveStateRefs(miotReadDeviceRefsForModel(spec, devices[index].Model))
			if !miotMeterEligible(devices[index].Category) {
				filtered := refs[:0]
				for _, ref := range refs {
					if !strings.HasPrefix(ref.Field, "energy:") {
						filtered = append(filtered, ref)
					}
				}
				refs = filtered
			}
			plans[index].refs = refs
			if len(refs) == 0 {
				devices[index].StateIssue = "MIoT 规格中没有可读取的实时属性（access 不包含 read）"
			}
		}(i)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return
	}

	// One property-get call per device. Keep bounded concurrency to avoid
	// bursts against Xiaomi cloud; properties of the same DID stay batched.
	for i := range devices {
		if !devices[i].Online || devices[i].RemoteID == "" || len(plans[i].refs) == 0 {
			continue
		}
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			requestCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()
			payload, err := xiaomiBuildGetPropertiesPayload(devices[index].RemoteID, plans[index].refs)
			if err != nil {
				devices[index].StateIssue = "构造 MIoT 属性读取请求失败：" + err.Error()
				return
			}
			body, err := xiaomiEncryptedPost(requestCtx, secret, "/miotspec/prop/get", payload)
			if err != nil {
				devices[index].StateIssue = "请求 MIoT prop/get 失败：" + truncate(sanitizeIntegrationLog(err.Error()), 220)
				return
			}
			var envelope struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Result  []any  `json:"result"`
			}
			if err := json.Unmarshal(body, &envelope); err != nil {
				devices[index].StateIssue = "解析 MIoT prop/get 响应失败：" + err.Error()
				return
			}
			if envelope.Code != 0 {
				devices[index].StateIssue = fmt.Sprintf("MIoT prop/get 业务错误 code=%d message=%s", envelope.Code, truncate(sanitizeIntegrationLog(envelope.Message), 160))
				return
			}
			needsPowerState := false
			for _, ref := range plans[index].refs {
				if ref.Field == "on" {
					needsPowerState = true
					break
				}
			}
			applied := applyMIoTDeviceResultForDID(&devices[index], plans[index].refs, body, false)
			if applied == 0 {
				if isVacuumDevice(devices[index]) {
					devices[index].StateIssue = ""
					return
				}
				devices[index].StateIssue = fmt.Sprintf("MIoT prop/get 返回 %d 条记录，但没有匹配到请求的 SIID/PIID", len(envelope.Result))
				return
			}
			if needsPowerState && devices[index].Power == nil && !isVacuumDevice(devices[index]) {
				devices[index].StateIssue = "MIoT 属性读取有响应，但未返回有效的开关属性值（switch.on）；其他属性不能代替电源状态"
				return
			}
			devices[index].StateIssue = ""
		}(i)
	}
	wg.Wait()
}

// xiaomiBuildGetPropertiesPayload keeps a property batch within a single DID.
// Xiaomi's legacy xiaomiio endpoint may omit DID in result records; isolation
// makes matching by (SIID, PIID) unambiguous without weakening cross-device checks.
func xiaomiBuildGetPropertiesPayload(did string, refs []miotDevicePropertyRef) ([]byte, error) {
	did = strings.TrimSpace(did)
	if did == "" {
		return nil, errors.New("MIoT 请求缺少 did")
	}
	if len(refs) == 0 {
		return nil, errors.New("MIoT 请求没有属性")
	}
	params := make([]map[string]any, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if ref.SIID <= 0 || ref.PIID <= 0 {
			continue
		}
		key := fmt.Sprintf("%d:%d", ref.SIID, ref.PIID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		params = append(params, map[string]any{"did": did, "siid": ref.SIID, "piid": ref.PIID})
	}
	if len(params) == 0 {
		return nil, errors.New("MIoT 请求没有有效的 SIID/PIID")
	}
	return json.Marshal(map[string]any{"params": params})
}

func xiaomiPopulateDeviceDetailsLegacy(ctx context.Context, secret providerSecret, devices []Device, includeEnergyStats, onlineOnly bool) {
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
			if writable, ok := xiaomiWritableSwitchForModel(spec, devices[index].Model); ok && writable.SIID > 0 && writable.PIID > 0 {
				devices[index].CanControl = true
			}
			if len(devices[index].Controls) > 0 {
				devices[index].CanControl = true
			}
			refs := miotReadDeviceRefsForModel(spec, devices[index].Model)
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
	applyMIoTDeviceResultForDID(device, refs, body, false)
}

// applyMIoTDeviceResultForDID applies property values to one device. In a
// multi-device batch, DID is mandatory: matching only SIID/PIID can otherwise
// copy one device's switch state onto another device with the same spec.
func applyMIoTDeviceResultForDID(device *Device, refs []miotDevicePropertyRef, body []byte, requireDID bool) int {
	if isVacuumDevice(*device) {
		device.Power = nil
	}
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
		return 0
	}
	refMap := make(map[string]miotDevicePropertyRef, len(refs))
	for _, ref := range refs {
		refMap[fmt.Sprintf("%d:%d", ref.SIID, ref.PIID)] = ref
	}
	attrs := make([]DeviceAttribute, 0, len(refs))
	applied := 0
	for _, item := range env.Result {
		if item.Code != 0 {
			continue
		}
		if device.RemoteID != "" {
			if requireDID && item.DID == "" {
				continue
			}
			if item.DID != "" && item.DID != device.RemoteID {
				continue
			}
		}
		ref, ok := refMap[fmt.Sprintf("%d:%d", item.SIID, item.PIID)]
		if !ok {
			continue
		}
		if ref.Field == "on" || ref.Field == "indicator_light" {
			if isVacuumDevice(*device) {
				continue
			}
			if _, validBool := miotBoolValue(item.Value); !validBool {
				continue
			}
		}
		// Writable vacuum properties are retained, including boolean modes
		// such as carpet boost or do-not-disturb. Main power is suppressed by
		// the device-specific UI semantics and by clearing Device.Power below;
		// dropping every boolean control here would also remove useful actions.
		device.StateRead = true
		applied++
		for index := range device.Controls {
			control := &device.Controls[index]
			if control.Kind != "property" || control.SIID != item.SIID || control.PIID != item.PIID {
				continue
			}
			control.Value = item.Value
			if strings.EqualFold(control.Format, "bool") || strings.EqualFold(control.Format, "boolean") {
				if enabled, ok := miotBoolValue(item.Value); ok {
					control.Value = enabled
				}
			}
			// Backward-compatible fallback for custom specs whose relay property
			// is exposed only as a writable control. Never use this for an LED or
			// indicator service, even if its translated label contains "开关".
			if ref.Field == "control" && control.Name == "电源开关" && !miotIndicatorService(ref.ServiceName, "", ref.ServiceType) {
				if enabled, ok := miotBoolValue(item.Value); ok {
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
			if b, ok := miotBoolValue(item.Value); ok {
				device.Power = &b
			}
		}
	}
	if energy.Supported {
		device.Energy = energy
	}
	if isVacuumDevice(*device) {
		device.Power = nil
		filtered := attrs[:0]
		for _, attr := range attrs {
			if attr.Key == "on" || attr.Label == "开关状态" || attr.Label == "电源开关" {
				continue
			}
			filtered = append(filtered, attr)
		}
		attrs = filtered
	}
	device.Attributes = attrs
	device.Value = summarizeDeviceAttributes(device)
	return applied
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
