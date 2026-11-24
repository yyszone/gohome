package app

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type xiaomiPending struct {
	ID              string
	Username        string
	Password        string
	Region          string
	Client          *http.Client
	Form            url.Values
	CaptchaURL      string
	CaptchaIck      string
	CaptchaImage    string
	VerifyURL       string
	IdentitySession string
	IdentityOptions []int
	VerifyFlag      int
	VerifyChannel   string
	TicketSent      bool
	IdentityCaptcha bool
	DeviceID        string
	LastURL         string
	IdentityStarted bool
	UserID          string
	CUserID         string
	PassToken       string
	SSecurity       string
	CreatedAt       time.Time
}

func (m *integrationManager) xiaomiLogin(ctx context.Context, username, password, region, label string) (map[string]any, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, errors.New("小米账号和密码不能为空")
	}
	jar, _ := cookiejar.New(nil)
	deviceID := xiaomiAccountDeviceID()
	accountURL, _ := url.Parse("https://account.xiaomi.com/")
	jar.SetCookies(accountURL, []*http.Cookie{
		{Name: "sdkVersion", Value: "3.8.6", Path: "/"},
		{Name: "deviceId", Value: deviceID, Path: "/"},
	})
	client := &http.Client{Jar: jar, Timeout: 25 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("小米登录重定向次数过多")
		}
		return nil
	}}
	if region == "" {
		region = "cn"
	}
	loginURL := "https://account.xiaomi.com/pass/serviceLogin?sid=xiaomiio&_json=true"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, loginURL, nil)
	req.Header.Set("User-Agent", xiaomiUserAgentForDevice(deviceID))
	req.Header.Set("Referer", "https://mi.com/")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接小米账号服务失败：%w", err)
	}
	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	auth, err := decodeXiaomiJSON(body)
	if err != nil {
		return nil, fmt.Errorf("读取小米登录初始化响应失败：%w", err)
	}
	form := url.Values{"user": {username}, "hash": {strings.ToUpper(fmt.Sprintf("%x", md5.Sum([]byte(password))))}, "callback": {stringValue(auth["callback"])}, "sid": {"xiaomiio"}, "qs": {stringValue(auth["qs"])}, "_sign": {stringValue(auth["_sign"])}}
	return m.xiaomiSubmit(ctx, client, form, username, password, region, label, "")
}

const xiaomiUAFormat = "Android-7.1.1-1.0.0-ONEPLUS A3010-136-%s APP/xiaomi.smarthome APPV/62830"

// Xiaomi Miot Auto uses one 16-character client ID for the Passport cookie
// and for every subsequent API request User-Agent. Keep this stable for the
// entire login/API session instead of creating unrelated random identifiers.
func xiaomiUserAgentForDevice(deviceID string) string {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		deviceID = xiaomiAccountDeviceID()
	}
	return fmt.Sprintf(xiaomiUAFormat, deviceID)
}

func xiaomiUserAgentForClient(client *http.Client) string {
	if client != nil {
		if id := xiaomiCookieValue(client, "deviceId"); id != "" {
			return xiaomiUserAgentForDevice(id)
		}
	}
	return xiaomiUserAgentForDevice("")
}

// Xiaomi Miot Auto's client_id is 16 characters from A-Z and 0-9.
func xiaomiAccountDeviceID() string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 16)
	randomBytes := make([]byte, len(b))
	if _, err := rand.Read(randomBytes); err != nil {
		return "7FNJC2OVCB0IE39J"
	}
	for i := range b {
		b[i] = alphabet[int(randomBytes[i])%len(alphabet)]
	}
	return string(b)
}

// Xiaomi's MIoT API client may use a shorter, lowercase deviceId.
func xiaomiDeviceCookieID() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 6)
	randomBytes := make([]byte, len(b))
	if _, err := rand.Read(randomBytes); err != nil {
		return "abcdef"
	}
	for i := range b {
		b[i] = alphabet[int(randomBytes[i])%len(alphabet)]
	}
	return string(b)
}

func (m *integrationManager) xiaomiSubmit(ctx context.Context, client *http.Client, form url.Values, username, password, region, label, captcha string) (map[string]any, error) {
	endpoint := "https://account.xiaomi.com/pass/serviceLoginAuth2?_json=true"
	if captcha != "" {
		form.Set("captCode", captcha)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", xiaomiUserAgentForClient(client))
	req.Header.Set("Referer", "https://account.xiaomi.com/")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if captcha != "" {
		req.URL.RawQuery += "&_dc=" + url.QueryEscape(fmt.Sprint(time.Now().UnixMilli()))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("小米登录请求失败：%w", err)
	}
	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	auth, err := decodeXiaomiJSON(body)
	if err != nil {
		return nil, fmt.Errorf("解析小米登录响应失败：%w", err)
	}
	deviceID := xiaomiCookieValue(client, "deviceId")
	if loc := stringValue(auth["location"]); loc != "" {
		ssecurity := stringValue(auth["ssecurity"])
		uid := firstNonEmpty(stringValue(auth["userId"]), xiaomiCookieValue(client, "userId"))
		cuserID := firstNonEmpty(stringValue(auth["cUserId"]), xiaomiCookieValue(client, "cUserId"))
		passToken := firstNonEmpty(stringValue(auth["passToken"]), xiaomiCookieValue(client, "passToken"))
		if ssecurity == "" || uid == "" {
			return nil, errors.New("小米登录响应缺少 userId 或 ssecurity，无法建立 MIoT 会话")
		}
		// Xiaomi Miot Auto's xiaomiio flow follows the signed location as-is.
		// Accept the serviceToken only when it is issued by this final STS
		// response; never reuse an identity/callback token left in the jar.
		sts, stsErr := xiaomiFetchServiceSession(ctx, client, loc)
		if stsErr != nil {
			return nil, fmt.Errorf("小米登录成功后的 serviceToken 获取失败：%w", stsErr)
		}
		uid = firstNonEmpty(sts.UserID, uid)
		cuserID = firstNonEmpty(sts.CUserID, cuserID)
		secret := providerSecret{Provider: "xiaomi", Region: region, Username: username, UserID: uid, CUserID: cuserID, DeviceID: deviceID, PassToken: passToken, ServiceToken: sts.ServiceToken, SSecurity: ssecurity, UpdatedAt: time.Now()}
		id := newID()
		ac := Account{ID: id, Provider: "xiaomi", Label: firstNonEmpty(label, username), Region: region, Status: "connected", Note: "小米账号已登录，正在同步云端设备", CreatedAt: time.Now()}
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
			return map[string]any{"account": ac, "connected": true, "deviceCount": 0, "warning": "账号已登录，但暂时无法读取设备清单：" + err.Error()}, nil
		}
		m.importDevices(id, "xiaomi", devices)
		m.setAccountStatus(id, "connected", fmt.Sprintf("已授权 · 已同步 %d 台设备", len(devices)))
		account, _ := m.findAccount(id)
		return map[string]any{"account": account, "connected": true, "deviceCount": len(devices)}, nil
	}
	if capURL := stringValue(auth["captchaUrl"]); capURL != "" {
		if !strings.HasPrefix(capURL, "http") {
			capURL = "https://account.xiaomi.com" + capURL
		}
		capReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, capURL, nil)
		capReq.Header.Set("User-Agent", xiaomiUserAgentForClient(client))
		capResp, err := client.Do(capReq)
		if err != nil {
			return nil, fmt.Errorf("获取小米验证码失败：%w", err)
		}
		img, _ := readBody(capResp)
		tick := ""
		for _, c := range capResp.Cookies() {
			if c.Name == "ick" {
				tick = c.Value
			}
		}
		if tick == "" {
			u, _ := url.Parse("https://account.xiaomi.com")
			for _, c := range client.Jar.Cookies(u) {
				if c.Name == "ick" {
					tick = c.Value
				}
			}
		}
		if tick == "" || len(img) == 0 {
			return nil, errors.New("小米返回了验证码，但验证码会话信息不完整")
		}
		pid := newID()
		p := &xiaomiPending{ID: pid, Username: username, Password: password, Region: region, Client: client, Form: form, CaptchaURL: capURL, CaptchaIck: tick, CaptchaImage: base64.StdEncoding.EncodeToString(img), DeviceID: deviceID, CreatedAt: time.Now()}
		m.mu.Lock()
		m.xiaomi[pid] = p
		m.mu.Unlock()
		time.AfterFunc(5*time.Minute, func() { m.deleteXiaomiPending(pid) })
		return map[string]any{"challenge": true, "challengeId": pid, "captchaImage": "data:image/png;base64," + p.CaptchaImage, "message": "小米账号要求图形验证码，请输入图片中的字符"}, nil
	}
	if ntf := stringValue(auth["notificationUrl"]); ntf != "" {
		ntf = xiaomiAbsoluteAccountURL(ntf)
		pendingID := newID()
		pending := newXiaomiIdentityPending(pendingID, username, password, region, client, form, ntf)
		pending.DeviceID = deviceID
		pending.UserID = stringValue(auth["userId"])
		pending.CUserID = stringValue(auth["cUserId"])
		pending.PassToken = stringValue(auth["passToken"])
		pending.SSecurity = stringValue(auth["ssecurity"])
		// Keep the password only in this short-lived in-memory challenge. Some
		// Xiaomi accounts return a second serviceLoginAuth2 redirect after SMS/email
		// verification; the reference flow needs the same password hash to finish it.
		m.mu.Lock()
		m.xiaomi[pendingID] = pending
		m.mu.Unlock()
		time.AfterFunc(10*time.Minute, func() { m.deleteXiaomiPending(pendingID) })
		result, verifyErr := m.xiaomiSendVerifyCode(ctx, pendingID, "", false)
		if verifyErr != nil {
			// Keep the original CookieJar and identity context. The UI can retry
			// only the discovery/send step; it must not submit the account password
			// again and create a second Xiaomi security challenge.
			return map[string]any{
				"verifyPending": true,
				"challengeId":   pendingID,
				"message":       "小米已要求短信/邮箱安全验证，但暂时无法读取验证渠道。当前登录会话已保留，请使用下方按钮重试验证流程，不要重新登录。详情：" + verifyErr.Error(),
			}, nil
		}
		return result, nil
	}
	code := stringValue(auth["code"])
	msg := stringValue(auth["desc"])
	if msg == "" {
		msg = stringValue(auth["description"])
	}
	if code != "" || msg != "" {
		return nil, fmt.Errorf("小米登录未成功（code=%s）：%s", code, msg)
	}
	return nil, errors.New("小米登录失败：平台返回了无法识别的响应")
}

func (m *integrationManager) xiaomiCaptcha(ctx context.Context, id, code string) (map[string]any, error) {
	m.mu.Lock()
	p := m.xiaomi[id]
	delete(m.xiaomi, id)
	m.mu.Unlock()
	if p == nil {
		return nil, errors.New("验证码会话已过期，请重新登录")
	}
	if time.Since(p.CreatedAt) > 5*time.Minute {
		p.Password = ""
		p.CaptchaImage = ""
		p.CaptchaIck = ""
		return nil, errors.New("验证码已过期，请重新登录")
	}
	u, _ := url.Parse("https://account.xiaomi.com")
	p.Client.Jar.SetCookies(u, append(p.Client.Jar.Cookies(u), &http.Cookie{Name: "ick", Value: p.CaptchaIck, Path: "/"}))
	result, err := m.xiaomiSubmit(ctx, p.Client, p.Form, p.Username, p.Password, p.Region, "", strings.TrimSpace(code))
	p.Password = ""
	p.CaptchaImage = ""
	return result, err
}

// xiaomiIdentityList discovers the verification methods available for this
// exact Xiaomi login session. Never accept an arbitrary URL from the browser.
func xiaomiAbsoluteAccountURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if !u.IsAbs() {
		base, _ := url.Parse("https://account.xiaomi.com/")
		u = base.ResolveReference(u)
	}
	return u.String()
}

func xiaomiIdentityListURL(raw string) (string, error) {
	u, err := url.Parse(xiaomiAbsoluteAccountURL(raw))
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "account.xiaomi.com") || u.Port() != "" || !strings.HasSuffix(u.Path, "/identity/authStart") {
		return "", errors.New("小米身份验证地址无效或已过期，请重新登录")
	}
	// Xiaomi's current Miot Auto flow replaces the complete
	// /fe/service/identity/authStart path with the root /identity/list path.
	// Keep the query string because it contains the one-time context/session.
	u.Path = "/identity/list"
	u.RawPath = ""
	return u.String(), nil
}

// xiaomiTrustedURL resolves Xiaomi callbacks while rejecting arbitrary hosts.
// Xiaomi serviceLogin has used account.xiaomi.com and multiple *.mi.com STS
// hosts; the domain check deliberately uses a dot-boundary suffix.
func xiaomiTrustedURL(raw, base string, accountOnly bool) (string, error) {
	u, err := normalizeXiaomiLocation(raw, base)
	if err != nil {
		return "", err
	}
	host := strings.ToLower(u.Hostname())
	trusted := host == "account.xiaomi.com"
	if !accountOnly {
		trusted = trusted || host == "mi.com" || host == "xiaomi.com" || strings.HasSuffix(host, ".mi.com") || strings.HasSuffix(host, ".xiaomi.com")
	}
	if u.Scheme != "https" || !trusted || u.Port() != "" || u.User != nil {
		return "", fmt.Errorf("小米跳转地址不在可信 HTTPS 域名范围内：%s", host)
	}
	return u.String(), nil
}

func xiaomiSafeHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{Timeout: 25 * time.Second}
	}
	safeClient := *client
	safeClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("小米登录重定向次数过多")
		}
		if _, err := xiaomiTrustedURL(req.URL.String(), "https://account.xiaomi.com/", false); err != nil {
			return fmt.Errorf("小米重定向目标不可信：%w", err)
		}
		return nil
	}
	return &safeClient
}

func xiaomiFollowTrustedURLResponse(ctx context.Context, client *http.Client, rawURL, referer string) (string, *http.Response, []byte, error) {
	trusted, err := xiaomiTrustedURL(rawURL, "https://account.xiaomi.com/", false)
	if err != nil {
		return "", nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trusted, nil)
	if err != nil {
		return "", nil, nil, err
	}
	req.Header.Set("User-Agent", xiaomiUserAgentForClient(client))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	// Reuse the original CookieJar while preventing redirects to unrelated hosts.
	resp, err := xiaomiSafeHTTPClient(client).Do(req)
	if err != nil {
		return "", nil, nil, err
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	closeErr := resp.Body.Close()
	if readErr != nil {
		return "", resp, body, readErr
	}
	if closeErr != nil {
		return "", resp, body, closeErr
	}
	finalURL := trusted
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return finalURL, resp, body, fmt.Errorf("小米回调 HTTP %d：%s", resp.StatusCode, truncate(sanitizeIntegrationLog(string(body)), 240))
	}
	return finalURL, resp, body, nil
}

func xiaomiFollowTrustedURL(ctx context.Context, client *http.Client, rawURL, referer string) (string, error) {
	finalURL, _, _, err := xiaomiFollowTrustedURLResponse(ctx, client, rawURL, referer)
	return finalURL, err
}

// Compatibility wrapper for account.xiaomi.com-only paths.
func xiaomiFollowAccountURL(ctx context.Context, client *http.Client, rawURL, referer string) (string, error) {
	if _, err := xiaomiTrustedURL(rawURL, "https://account.xiaomi.com/", true); err != nil {
		return "", err
	}
	return xiaomiFollowTrustedURL(ctx, client, rawURL, referer)
}

// Xiaomi currently uses skipUrl on the phone-confirm page; older flows used
// confirmUrl. Both are one-time URLs and must remain on Xiaomi HTTPS domains.
func xiaomiResumeURLFromFinal(finalURL string) (string, error) {
	u, err := url.Parse(finalURL)
	if err != nil {
		return "", err
	}
	for _, key := range []string{"skipUrl", "confirmUrl"} {
		if raw := strings.TrimSpace(u.Query().Get(key)); raw != "" {
			return xiaomiTrustedURL(raw, finalURL, false)
		}
	}
	return "", nil
}

func xiaomiConfirmURLFromFinal(finalURL string) (string, error) {
	return xiaomiResumeURLFromFinal(finalURL)
}

// Cookie jars filter cookies by their Path attribute. Query the actual callback
// URL so a serviceToken scoped to /identity or /pass is not missed at host root.
func xiaomiCookieValueAt(client *http.Client, rawURL string, names ...string) string {
	if client == nil || client.Jar == nil || strings.TrimSpace(rawURL) == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return ""
	}
	for _, c := range client.Jar.Cookies(u) {
		for _, name := range names {
			if c.Name == name && c.Value != "" {
				return c.Value
			}
		}
	}
	return ""
}

func xiaomiCookieOnURL(client *http.Client, rawURL, name string) string {
	if client == nil || client.Jar == nil {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	for _, c := range client.Jar.Cookies(u) {
		if c.Name == name && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

// Match HA's _login_step3: service token and related identity cookies must be
// issued by the final STS HTTP response. Do not fall back to a possibly stale
// cookie already present in the shared identity CookieJar.
func xiaomiFetchServiceSession(ctx context.Context, client *http.Client, location string) (xiaomiServiceSession, error) {
	stsURL, err := xiaomiTrustedURL(location, "https://account.xiaomi.com/", false)
	if err != nil {
		return xiaomiServiceSession{}, fmt.Errorf("小米 STS 跳转地址无效：%w", err)
	}
	finalURL, resp, body, err := xiaomiFollowTrustedURLResponse(ctx, client, stsURL, "https://account.xiaomi.com/")
	if err != nil {
		return xiaomiServiceSession{}, fmt.Errorf("跟随小米 STS 地址失败：%w", err)
	}
	out := xiaomiServiceSession{FinalURL: finalURL}
	if resp != nil {
		out.StatusCode = resp.StatusCode
		for _, c := range resp.Cookies() {
			switch c.Name {
			case "serviceToken":
				out.ServiceToken = c.Value
			case "userId":
				out.UserID = c.Value
			case "cUserId":
				out.CUserID = c.Value
			}
		}
	}
	if out.ServiceToken == "" {
		return xiaomiServiceSession{}, fmt.Errorf("小米 STS 最终响应没有签发 serviceToken（HTTP %d，回调 %s，响应 Cookie: serviceToken=false userId=%t cUserId=%t，响应 %s）",
			out.StatusCode, safeURLForLog(finalURL), out.UserID != "", out.CUserID != "", truncate(sanitizeIntegrationLog(string(body)), 220))
	}
	return out, nil
}

func xiaomiCookies(client *http.Client, rawURL string) map[string]string {
	out := map[string]string{}
	u, err := url.Parse(rawURL)
	if err == nil && client.Jar != nil {
		for _, c := range client.Jar.Cookies(u) {
			out[c.Name] = c.Value
		}
	}
	return out
}

func xiaomiCookieValue(client *http.Client, names ...string) string {
	if client == nil || client.Jar == nil {
		return ""
	}
	for _, host := range []string{"https://account.xiaomi.com", "https://api.io.mi.com", "https://sts.api.io.mi.com", "https://api2.mina.mi.com", "https://mina.mi.com"} {
		for _, c := range client.Jar.Cookies(mustURL(host)) {
			for _, name := range names {
				if c.Name == name && c.Value != "" {
					return c.Value
				}
			}
		}
	}
	return ""
}

func (m *integrationManager) xiaomiSendVerifyCode(ctx context.Context, id, captcha string, resend bool) (map[string]any, error) {
	m.mu.Lock()
	p := m.xiaomi[id]
	m.mu.Unlock()
	if p == nil {
		return nil, errors.New("小米验证会话已过期，请重新登录")
	}
	if time.Since(p.CreatedAt) > 10*time.Minute {
		m.deleteXiaomiPending(id)
		return nil, errors.New("小米验证会话已过期，请重新登录")
	}
	if resend {
		p.TicketSent = false
	}
	if p.TicketSent && captcha == "" {
		return map[string]any{"verifyRequired": true, "challengeId": id, "channel": p.VerifyChannel, "message": "验证码已发送，请查收后输入"}, nil
	}

	if !p.IdentityStarted {
		openReq, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, p.VerifyURL, nil)
		if reqErr != nil {
			return nil, reqErr
		}
		openReq.Header.Set("User-Agent", xiaomiUserAgentForClient(p.Client))
		openReq.Header.Set("Referer", "https://account.xiaomi.com/")
		if p.DeviceID != "" {
			openReq.AddCookie(&http.Cookie{Name: "deviceId", Value: p.DeviceID})
		}
		openResp, openErr := p.Client.Do(openReq)
		if openErr != nil {
			return nil, fmt.Errorf("建立小米二次验证会话失败：%w", openErr)
		}
		if _, openErr = readBody(openResp); openErr != nil {
			return nil, openErr
		}
		p.IdentityStarted = true
	}
	if p.IdentitySession == "" || len(p.IdentityOptions) == 0 {
		listURL, err := xiaomiIdentityListURL(p.VerifyURL)
		if err != nil {
			return nil, err
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
		req.Header.Set("User-Agent", xiaomiUserAgentForClient(p.Client))
		req.Header.Set("Referer", p.VerifyURL)
		resp, err := p.Client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("读取小米验证方式失败：%w", err)
		}
		body, err := readBody(resp)
		if err != nil {
			return nil, err
		}
		data, err := decodeXiaomiJSON(body)
		if err != nil {
			return nil, fmt.Errorf("解析小米验证方式失败（HTTP %d，Content-Type %s）：%w；当前登录会话已保留，请勿重新输入密码", resp.StatusCode, firstNonEmpty(resp.Header.Get("Content-Type"), "未知"), err)
		}
		var identitySession string
		for _, c := range resp.Cookies() {
			if c.Name == "identity_session" {
				identitySession = c.Value
			}
		}
		if identitySession == "" {
			identitySession = xiaomiCookieValue(p.Client, "identity_session")
		}
		if identitySession == "" {
			return nil, errors.New("小米未建立 identity_session，请在同一网络环境下重新登录")
		}
		p.IdentitySession = identitySession
		if opts, ok := data["options"].([]any); ok {
			for _, v := range opts {
				if n, ok := v.(float64); ok && (int(n) == 4 || int(n) == 8) {
					p.IdentityOptions = append(p.IdentityOptions, int(n))
				}
			}
		}
		if len(p.IdentityOptions) == 0 {
			if n, ok := data["flag"].(float64); ok && (int(n) == 4 || int(n) == 8) {
				p.IdentityOptions = []int{int(n)}
			}
		}
		preferred, _ := data["flag"].(float64)
		p.VerifyFlag = int(preferred)
		if len(p.IdentityOptions) == 0 {
			return nil, errors.New("小米账号没有返回可用的短信或邮箱验证方式")
		}
	}

	flag := p.VerifyFlag
	valid := false
	for _, x := range p.IdentityOptions {
		if x == flag {
			valid = true
		}
	}
	if !valid {
		flag = p.IdentityOptions[0]
	}
	if flag != 4 && flag != 8 {
		return nil, errors.New("小米返回了不支持的验证方式")
	}
	channel := "Phone"
	if flag == 8 {
		channel = "Email"
	}
	p.VerifyFlag, p.VerifyChannel = flag, channel

	// Select the delivery channel in this identity session before asking Xiaomi to send a code.
	probeURL := "https://account.xiaomi.com/identity/auth/verify" + channel
	probeQ := url.Values{"_flag": {strconv.Itoa(flag)}, "_json": {"true"}}
	probeURL += "?" + probeQ.Encode()
	probeReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	probeReq.Header.Set("User-Agent", xiaomiUserAgentForClient(p.Client))
	probeReq.Header.Set("Referer", p.VerifyURL)
	probeReq.AddCookie(&http.Cookie{Name: "identity_session", Value: p.IdentitySession})
	probeResp, err := p.Client.Do(probeReq)
	if err != nil {
		return nil, fmt.Errorf("选择小米验证渠道失败：%w", err)
	}
	probeBody, err := readBody(probeResp)
	if err != nil {
		return nil, err
	}
	probeData, err := decodeXiaomiJSON(probeBody)
	if err != nil {
		return nil, err
	}
	if intValue(probeData["code"], -1) != 0 {
		return nil, fmt.Errorf("小米验证渠道暂不可用：%s", firstNonEmpty(stringValue(probeData["desc"]), stringValue(probeData["description"]), stringValue(probeData["code"])))
	}

	form := url.Values{"_flag": {strconv.Itoa(flag)}, "retry": {"0"}, "_json": {"true"}, "icode": {captcha}}
	sendURL := "https://account.xiaomi.com/identity/auth/send" + channel + "Ticket?_dc=" + url.QueryEscape(fmt.Sprint(time.Now().UnixMilli()))
	sendReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, sendURL, strings.NewReader(form.Encode()))
	sendReq.Header.Set("User-Agent", xiaomiUserAgentForClient(p.Client))
	sendReq.Header.Set("Referer", p.VerifyURL)
	sendReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sendReq.AddCookie(&http.Cookie{Name: "identity_session", Value: p.IdentitySession})
	if captcha != "" {
		if ck := xiaomiCookieValue(p.Client, "ick"); ck != "" {
			sendReq.AddCookie(&http.Cookie{Name: "ick", Value: ck})
		}
	}
	sendResp, err := p.Client.Do(sendReq)
	if err != nil {
		return nil, fmt.Errorf("请求小米发送验证码失败：%w", err)
	}
	sendBody, err := readBody(sendResp)
	if err != nil {
		return nil, err
	}
	sendData, err := decodeXiaomiJSON(sendBody)
	if err != nil {
		return nil, err
	}
	code := intValue(sendData["code"], -1)
	if code == 0 {
		p.TicketSent = true
		return map[string]any{"verifyRequired": true, "challengeId": id, "channel": channel, "message": map[bool]string{true: "短信验证码已发送，请查收短信", false: "邮箱验证码已发送，请查收邮件"}[flag == 4]}, nil
	}
	if code == 87001 && stringValue(sendData["captchaUrl"]) != "" {
		capURL := stringValue(sendData["captchaUrl"])
		if !strings.HasPrefix(capURL, "https://account.xiaomi.com/") {
			return nil, errors.New("小米返回了不安全的验证码地址")
		}
		capReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, capURL, nil)
		capReq.Header.Set("User-Agent", xiaomiUserAgentForClient(p.Client))
		capReq.Header.Set("Referer", p.VerifyURL)
		capResp, e := p.Client.Do(capReq)
		if e != nil {
			return nil, e
		}
		img, e := readBody(capResp)
		if e != nil {
			return nil, e
		}
		tick := ""
		for _, c := range capResp.Cookies() {
			if c.Name == "ick" {
				tick = c.Value
			}
		}
		if tick == "" {
			tick = xiaomiCookieValue(p.Client, "ick")
		}
		if tick == "" || len(img) == 0 {
			return nil, errors.New("小米短信验证图形验证码会话不完整")
		}
		p.CaptchaURL, p.CaptchaIck, p.CaptchaImage = capURL, tick, base64.StdEncoding.EncodeToString(img)
		p.IdentityCaptcha = true
		return map[string]any{"identityCaptcha": true, "challengeId": id, "captchaImage": "data:image/png;base64," + p.CaptchaImage, "message": "小米发送验证码前要求图形验证，请输入图片字符"}, nil
	}
	return nil, fmt.Errorf("小米发送%s验证码失败（%d）：%s", map[int]string{4: "短信", 8: "邮箱"}[flag], code, firstNonEmpty(stringValue(sendData["description"]), stringValue(sendData["desc"]), "请稍后重试"))
}

// Legacy form builder retained for regression coverage; OTP completion no longer re-posts serviceLoginAuth2.
func buildXiaomiPostVerifyForm(username, password string, auth map[string]any, fallback url.Values) url.Values {
	value := func(key, defaultValue string) string {
		if v := stringValue(auth[key]); v != "" {
			return v
		}
		if fallback != nil {
			if v := fallback.Get(key); v != "" {
				return v
			}
		}
		return defaultValue
	}
	passwordHash := strings.ToUpper(fmt.Sprintf("%x", md5.Sum([]byte(password))))
	return url.Values{
		"user":     {username},
		"hash":     {passwordHash},
		"callback": {value("callback", "")},
		"sid":      {value("sid", "xiaomiio")},
		"qs":       {value("qs", "")},
		"_sign":    {value("_sign", "")},
	}
}

func safeURLForLog(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "(URL unavailable)"
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.Scheme + "://" + u.Host + u.EscapedPath()
}

func (m *integrationManager) xiaomiVerifyTicket(ctx context.Context, id, ticket string) (map[string]any, error) {
	m.mu.Lock()
	p := m.xiaomi[id]
	m.mu.Unlock()
	if p == nil {
		return nil, errors.New("小米验证会话已过期，请重新登录")
	}
	if time.Since(p.CreatedAt) > 10*time.Minute {
		m.deleteXiaomiPending(id)
		return nil, errors.New("小米验证会话已过期，请重新登录")
	}
	if !p.TicketSent {
		return nil, errors.New("请先发送短信或邮箱验证码")
	}
	ticket = strings.TrimSpace(ticket)
	if ticket == "" {
		return nil, errors.New("请输入收到的验证码")
	}
	channel := "Phone"
	if p.VerifyFlag == 8 {
		channel = "Email"
	} else if p.VerifyFlag != 4 {
		return nil, errors.New("小米验证渠道无效")
	}

	// Step 1: submit the OTP within the original identity_session.
	form := url.Values{
		"_flag": {strconv.Itoa(p.VerifyFlag)}, "ticket": {ticket},
		"trust": {"false"}, "_json": {"true"},
	}
	endpoint := "https://account.xiaomi.com/identity/auth/verify" + channel + "?_dc=" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", xiaomiUserAgentForClient(p.Client))
	req.Header.Set("Referer", p.VerifyURL)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if p.IdentitySession != "" {
		req.AddCookie(&http.Cookie{Name: "identity_session", Value: p.IdentitySession})
	}
	if p.DeviceID != "" {
		req.AddCookie(&http.Cookie{Name: "deviceId", Value: p.DeviceID})
	}
	// Do not let the OTP POST auto-follow an HTML/STS redirect. Read the JSON
	// `location` (or the Location header on older responses) first, then follow
	// it explicitly through the Xiaomi host allow-list with the same CookieJar.
	otpClient := *p.Client
	otpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := otpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("提交小米验证码失败：%w", err)
	}
	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	data, err := decodeXiaomiJSON(body)
	if err != nil {
		if strings.TrimSpace(resp.Header.Get("Location")) == "" {
			return nil, fmt.Errorf("解析小米验证码响应失败（HTTP %d）：%w", resp.StatusCode, err)
		}
		data = map[string]any{"code": 0, "location": resp.Header.Get("Location")}
	}
	if code := intValue(data["code"], -1); code != 0 {
		return nil, fmt.Errorf("小米验证码校验失败（%d）：%s", code, firstNonEmpty(stringValue(data["description"]), stringValue(data["desc"]), "请检查验证码是否正确"))
	}
	locationRaw := firstNonEmpty(stringValue(data["location"]), resp.Header.Get("Location"))
	location, err := xiaomiTrustedURL(locationRaw, "https://account.xiaomi.com/", false)
	if err != nil {
		return nil, fmt.Errorf("小米验证码已通过，但回调地址无效：%w", err)
	}

	// Step 2: finish the identity callback. Xiaomi may redirect through a
	// phone-confirm page carrying skipUrl (newer flow) or confirmUrl (older).
	finalURL, err := xiaomiFollowTrustedURL(ctx, p.Client, location, p.VerifyURL)
	if err != nil {
		return nil, fmt.Errorf("跟随小米验证回调失败：%w", err)
	}
	p.LastURL = finalURL
	for hop := 0; hop < 2; hop++ {
		nextURL, parseErr := xiaomiResumeURLFromFinal(p.LastURL)
		if parseErr != nil {
			return nil, fmt.Errorf("解析小米手机号确认跳转失败：%w", parseErr)
		}
		if nextURL == "" {
			break
		}
		if nextURL == p.LastURL {
			return nil, errors.New("小米手机号确认地址发生循环跳转")
		}
		finalURL, err = xiaomiFollowTrustedURL(ctx, p.Client, nextURL, p.LastURL)
		if err != nil {
			return nil, fmt.Errorf("完成小米手机号确认失败：%w", err)
		}
		p.LastURL = finalURL
	}

	// The identity callback may set a temporary serviceToken cookie. Keep it
	// separately: it is valid only as the completed session token when the
	// refreshed serviceLogin response has ssecurity and does NOT return an STS
	// location. If serviceLogin returns a location, HA's _login_step3 must run
	// and its final response must issue the actual serviceToken.
	identityServiceToken := xiaomiCookieValueAt(p.Client, p.LastURL, "serviceToken")

	// Step 3: repeat the serviceLogin handshake using the same client_id,
	// CookieJar and User-Agent. This is the only authoritative source of the
	// post-verification ssecurity value.
	serviceURL := "https://account.xiaomi.com/pass/serviceLogin?sid=xiaomiio&_json=true"
	serviceReq, err := http.NewRequestWithContext(ctx, http.MethodGet, serviceURL, nil)
	if err != nil {
		return nil, err
	}
	serviceReq.Header.Set("User-Agent", xiaomiUserAgentForClient(p.Client))
	serviceReq.Header.Set("Referer", p.LastURL)
	serviceResp, err := xiaomiSafeHTTPClient(p.Client).Do(serviceReq)
	if err != nil {
		return nil, fmt.Errorf("小米二次验证后重新执行 serviceLogin 失败：%w", err)
	}
	serviceBody, err := readBody(serviceResp)
	if err != nil {
		return nil, fmt.Errorf("读取小米验证后的 serviceLogin 响应失败：%w", err)
	}
	serviceAuth, err := decodeXiaomiJSON(serviceBody)
	if err != nil {
		return nil, fmt.Errorf("解析小米验证后的 serviceLogin 响应失败（HTTP %d）：%w", serviceResp.StatusCode, err)
	}
	if code, exists := serviceAuth["code"]; exists && intValue(code, -1) != 0 {
		return nil, fmt.Errorf("小米验证码已通过，但 serviceLogin 未完成（code=%d）：%s", intValue(serviceAuth["code"], -1), firstNonEmpty(stringValue(serviceAuth["desc"]), stringValue(serviceAuth["description"]), truncate(string(serviceBody), 220)))
	}

	uid := firstNonEmpty(stringValue(serviceAuth["userId"]), p.UserID, xiaomiCookieValue(p.Client, "userId"))
	cuserID := firstNonEmpty(stringValue(serviceAuth["cUserId"]), p.CUserID, xiaomiCookieValue(p.Client, "cUserId"))
	passToken := firstNonEmpty(stringValue(serviceAuth["passToken"]), p.PassToken, xiaomiCookieValue(p.Client, "passToken"))
	ssecurity := firstNonEmpty(stringValue(serviceAuth["ssecurity"]), p.SSecurity, xiaomiCookieValue(p.Client, "ssecurity"))
	serviceLocation := strings.TrimSpace(stringValue(serviceAuth["location"]))
	serviceToken := ""

	if serviceLocation != "" {
		// Critical: never let the identity callback's old cookie short-circuit
		// this step. Follow the serviceLogin location and read the freshly issued
		// serviceToken/userId/cUserId only from the final STS response.
		sts, stsErr := xiaomiFetchServiceSession(ctx, p.Client, serviceLocation)
		if stsErr != nil {
			return nil, fmt.Errorf("小米验证后 serviceLogin 返回 STS location，但最终服务令牌获取失败：%w", stsErr)
		}
		serviceToken = sts.ServiceToken
		uid = firstNonEmpty(sts.UserID, uid)
		cuserID = firstNonEmpty(sts.CUserID, cuserID)
	} else if ssecurity != "" && identityServiceToken != "" {
		// HA returns early only for this exact condition: fresh ssecurity plus
		// the serviceToken finalized by the identity callback, with no location.
		serviceToken = identityServiceToken
	} else {
		// Match HA's fallback when the refreshed serviceLogin did not return a
		// complete (ssecurity + serviceToken) pair: submit serviceLoginAuth2 with
		// refreshed fields, preserving the original password only in this
		// short-lived verification challenge.
		step2Form := buildXiaomiPostVerifyForm(p.Username, p.Password, serviceAuth, p.Form)
		step2URL := "https://account.xiaomi.com/pass/serviceLoginAuth2?_json=true"
		step2Req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, step2URL, strings.NewReader(step2Form.Encode()))
		if reqErr != nil {
			return nil, reqErr
		}
		step2Req.Header.Set("User-Agent", xiaomiUserAgentForClient(p.Client))
		step2Req.Header.Set("Referer", serviceURL)
		step2Req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		step2Resp, reqErr := p.Client.Do(step2Req)
		if reqErr != nil {
			return nil, fmt.Errorf("小米二次验证后补全 serviceLoginAuth2 失败：%w", reqErr)
		}
		step2Body, reqErr := readBody(step2Resp)
		if reqErr != nil {
			return nil, fmt.Errorf("读取小米二次验证后的 serviceLoginAuth2 响应失败：%w", reqErr)
		}
		step2Auth, reqErr := decodeXiaomiJSON(step2Body)
		if reqErr != nil {
			return nil, fmt.Errorf("解析小米二次验证后的 serviceLoginAuth2 响应失败：%w", reqErr)
		}
		if step2Code := intValue(step2Auth["code"], 0); step2Code != 0 {
			if ntf := stringValue(step2Auth["notificationUrl"]); ntf != "" {
				return nil, errors.New("小米在短信/邮箱验证后再次要求身份验证，请重新发起登录并完成新的验证步骤")
			}
			return nil, fmt.Errorf("小米二次验证后的 serviceLoginAuth2 未完成（code=%d）：%s", step2Code, firstNonEmpty(stringValue(step2Auth["desc"]), stringValue(step2Auth["description"]), truncate(string(step2Body), 220)))
		}
		uid = firstNonEmpty(stringValue(step2Auth["userId"]), uid)
		cuserID = firstNonEmpty(stringValue(step2Auth["cUserId"]), cuserID)
		passToken = firstNonEmpty(stringValue(step2Auth["passToken"]), passToken)
		ssecurity = firstNonEmpty(stringValue(step2Auth["ssecurity"]), ssecurity)
		step2Location := strings.TrimSpace(stringValue(step2Auth["location"]))
		if step2Location == "" {
			return nil, fmt.Errorf("小米 serviceLoginAuth2 未返回 STS location（HTTP %d）", step2Resp.StatusCode)
		}
		sts, stsErr := xiaomiFetchServiceSession(ctx, p.Client, step2Location)
		if stsErr != nil {
			return nil, fmt.Errorf("小米 serviceLoginAuth2 后获取 serviceToken 失败：%w", stsErr)
		}
		serviceToken = sts.ServiceToken
		uid = firstNonEmpty(sts.UserID, uid)
		cuserID = firstNonEmpty(sts.CUserID, cuserID)
	}
	if uid == "" || ssecurity == "" || serviceToken == "" {
		return nil, fmt.Errorf("小米二次验证已通过，但登录会话不完整（userId=%t ssecurity=%t serviceToken=%t，serviceLogin HTTP %d，最终回调 %s）", uid != "", ssecurity != "", serviceToken != "", serviceResp.StatusCode, safeURLForLog(p.LastURL))
	}
	secret := providerSecret{
		Provider: "xiaomi", Region: p.Region, Username: p.Username,
		UserID: uid, CUserID: cuserID, DeviceID: p.DeviceID,
		PassToken: passToken, ServiceToken: serviceToken,
		SSecurity: ssecurity, UpdatedAt: time.Now(),
	}
	result, err := m.saveXiaomiAccountAndSync(ctx, secret, p.Username)
	if err == nil {
		m.deleteXiaomiPending(id)
	}
	return result, err
}

func decodeXiaomiJSON(b []byte) (map[string]any, error) {
	// Xiaomi's account endpoints use this anti-JSON-hijacking prefix. Match
	// Miot Auto's decoder instead of searching for the first '{': searching
	// through an HTML/JS challenge page can produce misleading JSON errors.
	s := strings.TrimSpace(strings.ReplaceAll(string(b), "&&&START&&&", ""))
	s = strings.TrimSpace(strings.TrimSuffix(s, "&&&END&&&"))
	if s == "" {
		return nil, errors.New("小米返回了空响应")
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html") || strings.HasPrefix(lower, "<head") {
		return nil, errors.New("小米返回了网页而不是 JSON；验证地址可能被重定向、会话过期或触发了网页安全页")
	}
	if !strings.HasPrefix(s, "{") {
		return nil, errors.New("小米响应不是 JSON 对象")
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("JSON 格式错误：%w", err)
	}
	if out == nil {
		return nil, errors.New("小米返回的 JSON 不是对象")
	}
	return out, nil
}
