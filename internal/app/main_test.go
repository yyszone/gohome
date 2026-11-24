package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testApp(t *testing.T) *App {
	t.Helper()
	s, err := newStore(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(s)
	// Endpoint tests use an explicit auth bypass. Persistent cookie auth is
	// covered separately in TestPersistentCookieAuth and is always enabled in
	// production; administrator credentials are fixed in source code.
	a.authDisabled = true
	return a
}

func TestPersistentCookieAuth(t *testing.T) {
	a := testApp(t)
	a.authDisabled = false

	// The API is protected before login.
	unauthenticated := httptest.NewRecorder()
	a.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d want=%d", unauthenticated.Code, http.StatusUnauthorized)
	}

	loginBody := `{"username":"admin","password":"123456"}`
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	loginReq.RemoteAddr = "192.0.2.10:12345"
	loginReq.Header.Set("Content-Type", "application/json")
	loginResp := httptest.NewRecorder()
	a.ServeHTTP(loginResp, loginReq)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginResp.Code, loginResp.Body.String())
	}
	cookies := loginResp.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || cookies[0].MaxAge <= 0 || !cookies[0].HttpOnly {
		t.Fatalf("persistent HttpOnly session cookie missing: %+v", cookies)
	}

	requestWithCookie := func(app *App) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.AddCookie(cookies[0])
		recorder := httptest.NewRecorder()
		app.ServeHTTP(recorder, req)
		return recorder
	}
	if got := requestWithCookie(a); got.Code != http.StatusOK {
		t.Fatalf("authenticated status=%d body=%s", got.Code, got.Body.String())
	}

	// Recreate the App using the same data directory/key: browser cookies must
	// survive process restarts as long as data/gohome.session.key is preserved.
	restarted := newApp(a.store)
	if got := requestWithCookie(restarted); got.Code != http.StatusOK {
		t.Fatalf("cookie did not survive app restart: status=%d body=%s", got.Code, got.Body.String())
	}
}

func TestPersistentCookieAuthRejectsWrongPassword(t *testing.T) {
	a := testApp(t)
	a.authDisabled = false
	for _, credentials := range []string{`{"username":"wrong","password":"123456"}`, `{"username":"admin","password":"wrong"}`} {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(credentials))
		req.RemoteAddr = "192.0.2.11:12345"
		resp := httptest.NewRecorder()
		a.ServeHTTP(resp, req)
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("invalid credentials status=%d body=%s", resp.Code, resp.Body.String())
		}
	}
}

func TestXiaomi70022IsReportedAsSendFailure(t *testing.T) {
	pending := &xiaomiPending{
		VerifyChannel:   "Phone",
		VerifyFlag:      4,
		IdentityOptions: []int{4, 8},
	}
	result := (&integrationManager{}).xiaomiVerifyPendingResult("challenge-1", pending, errors.New("小米短信发送接口拒绝本次请求（70022）：发送验证码失败"))
	if result["verifyPending"] != true || result["errorCode"] != 70022 {
		t.Fatalf("unexpected pending response: %#v", result)
	}
	message, _ := result["message"].(string)
	if strings.Contains(message, "无法读取验证渠道") || !strings.Contains(message, "发送阶段") {
		t.Fatalf("70022 should be described as send failure, got: %s", message)
	}
	if !strings.Contains(message, "同一手机号 24 小时") || strings.Contains(message, "你提到其他手机号") {
		t.Fatalf("70022 should explain per-number throttling without user-specific wording, got: %s", message)
	}
	channels, _ := result["channels"].([]map[string]any)
	if len(channels) != 2 {
		t.Fatalf("expected SMS and email channels for retry, got: %#v", result["channels"])
	}
}

func TestHealthEndpoint(t *testing.T) {
	a := testApp(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	a.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["version"] != version {
		t.Fatalf("health version=%v want %s", payload["version"], version)
	}
}

func TestStoreSnapshotDeepCopiesDeviceChannels(t *testing.T) {
	a := testApp(t)
	on := true
	if err := a.store.putDevice(Device{ID: "multi-copy", Power: &on, Channels: []DeviceChannel{{Outlet: 0, Power: &on, CanControl: true}}}); err != nil {
		t.Fatal(err)
	}
	snap := a.store.snapshot()
	snap.Devices[0].Channels[0].Name = "mutated outside store"
	*snap.Devices[0].Channels[0].Power = false
	got := a.store.snapshot().Devices[0]
	if got.Channels[0].Name == "mutated outside store" || got.Channels[0].Power == nil || !*got.Channels[0].Power {
		t.Fatalf("snapshot aliased persisted channel state: %+v", got.Channels[0])
	}
}

func TestRefreshDeviceStatesAppliesExternalSwitchChanges(t *testing.T) {
	a := testApp(t)
	initialPower := true
	initialChannelPower := true
	if err := a.store.putDevice(Device{
		ID: "ewelink-state-1", AccountID: "account-1", Name: "电量插座",
		Protocol: "ewelink-cloud", Online: true, Power: &initialPower,
		CanControl: true, Channels: []DeviceChannel{{Outlet: 0, Name: "通道 1", Power: &initialChannelPower, CanControl: true}},
		Controls:   []DeviceControl{{ID: "switch", Kind: "property", Name: "开关", Value: true}},
		Attributes: []DeviceAttribute{{Key: "temperature", Label: "温度", Value: "31", Unit: "°C"}},
	}); err != nil {
		t.Fatal(err)
	}
	externalOff := false
	updatedChannel := false
	_, err := a.store.refreshDeviceStates("account-1", []Device{{
		ID: "ewelink-state-1", Online: true, Power: &externalOff,
		Channels: []DeviceChannel{{Outlet: 0, Power: &updatedChannel, CanControl: true}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := a.store.snapshot().Devices[0]
	if got.Power == nil || *got.Power {
		t.Fatalf("external off state was not applied: %+v", got.Power)
	}
	if len(got.Channels) != 1 || got.Channels[0].Power == nil || *got.Channels[0].Power {
		t.Fatalf("external channel off state was not applied: %+v", got.Channels)
	}
	if !got.CanControl || len(got.Controls) != 1 || len(got.Attributes) != 1 {
		t.Fatalf("refresh should preserve existing controls and attributes: %+v", got)
	}
}

func TestRefreshOnlineDeviceClearsStalePowerWhenProviderHasNoFreshValue(t *testing.T) {
	a := testApp(t)
	on := true
	if err := a.store.putDevice(Device{ID: "stale-power", AccountID: "account-live", Online: true, Power: &on}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.refreshDeviceStates("account-live", []Device{{ID: "stale-power", Online: true, StateRead: false}}); err != nil {
		t.Fatal(err)
	}
	got := a.store.snapshot().Devices[0]
	if got.Power != nil {
		t.Fatalf("stale ON must be cleared when live value is unavailable, got %v", *got.Power)
	}
}

func TestLiveDevicesEndpointReturnsRevisionAndNoContentWhenUnchanged(t *testing.T) {
	a := testApp(t)
	on := true
	if err := a.store.putDevice(Device{ID: "live-1", AccountID: "acct", Online: true, Power: &on, Controls: []DeviceControl{{ID: "switch-1", Kind: "property", Name: "电源开关", Value: true}}}); err != nil {
		t.Fatal(err)
	}
	first := httptest.NewRecorder()
	a.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/devices/live?since=0", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first live status=%d body=%s", first.Code, first.Body.String())
	}
	var payload struct {
		Revision uint64            `json:"revision"`
		Devices  []LiveDeviceState `json:"devices"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Revision == 0 || len(payload.Devices) != 1 || payload.Devices[0].Power == nil || !*payload.Devices[0].Power {
		t.Fatalf("unexpected live snapshot: %+v", payload)
	}
	if len(payload.Devices[0].ControlValues) != 1 || payload.Devices[0].ControlValues[0].ID != "switch-1" || payload.Devices[0].ControlValues[0].Value != true {
		t.Fatalf("live snapshot should include only current MIoT property values: %+v", payload.Devices[0].ControlValues)
	}
	unchanged := httptest.NewRecorder()
	a.ServeHTTP(unchanged, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/devices/live?since=%d", payload.Revision), nil))
	if unchanged.Code != http.StatusNoContent {
		t.Fatalf("unchanged live status=%d body=%s", unchanged.Code, unchanged.Body.String())
	}
	off := false
	if _, err := a.store.refreshDeviceStates("acct", []Device{{ID: "live-1", Online: true, Power: &off, StateRead: true}}); err != nil {
		t.Fatal(err)
	}
	changed := httptest.NewRecorder()
	a.ServeHTTP(changed, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/devices/live?since=%d", payload.Revision), nil))
	if changed.Code != http.StatusOK {
		t.Fatalf("changed live status=%d body=%s", changed.Code, changed.Body.String())
	}
	var updated struct {
		Devices []LiveDeviceState `json:"devices"`
	}
	if err := json.Unmarshal(changed.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if len(updated.Devices) != 1 || updated.Devices[0].Power == nil || *updated.Devices[0].Power {
		t.Fatalf("external OFF not published: %+v", updated)
	}
}

func TestMIoTReadRefsKeepSwitchOutOfEnergyMetrics(t *testing.T) {
	spec := miotSpecDocument{Services: []struct {
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
	}{}}
	service := struct {
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
	}{IID: 2}
	property := struct {
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
	}{IID: 1, Name: "relay_state", Type: "urn:miot-spec-v2:property:on:00000006", Format: "bool", Access: []string{"read", "write"}}
	service.Properties = append(service.Properties, property)
	spec.Services = append(spec.Services, service)
	refs := miotReadDeviceRefs(spec)
	if len(refs) != 1 || refs[0].Field != "on" {
		t.Fatalf("relay state should be read as a switch, refs=%+v", refs)
	}
	if strings.HasPrefix(refs[0].Field, "energy:") {
		t.Fatalf("switch state must not be classified as energy: %+v", refs[0])
	}
}

func TestApplyMIoTDeviceResultUpdatesExternalOffValue(t *testing.T) {
	device := Device{RemoteID: "did-live-1"}
	refs := []miotDevicePropertyRef{{SIID: 2, PIID: 1, Field: "on", Label: "开关状态", Format: "bool"}}
	body := []byte(`{"code":0,"result":[{"did":"did-live-1","siid":2,"piid":1,"code":0,"value":false}]}`)
	applyMIoTDeviceResult(&device, refs, body)
	if !device.StateRead {
		t.Fatal("successful MIoT property response was not marked as fresh")
	}
	if device.Power == nil || *device.Power {
		t.Fatalf("MIoT OFF value was not applied: %+v", device.Power)
	}
}

func TestMIoTBatchResultMustMatchDeviceDID(t *testing.T) {
	refs := []miotDevicePropertyRef{{SIID: 2, PIID: 1, Field: "on", Label: "开关状态", Format: "bool"}}
	body := []byte(`{"code":0,"result":[{"did":"did-other","siid":2,"piid":1,"code":0,"value":true},{"did":"did-target","siid":2,"piid":1,"code":0,"value":false},{"siid":2,"piid":1,"code":0,"value":true}]}`)
	target := Device{RemoteID: "did-target"}
	applyMIoTDeviceResultForDID(&target, refs, body, true)
	if !target.StateRead || target.Power == nil || *target.Power {
		t.Fatalf("batch must apply only the target device's explicit OFF result: %+v", target)
	}
	other := Device{RemoteID: "did-other"}
	applyMIoTDeviceResultForDID(&other, refs, body, true)
	if !other.StateRead || other.Power == nil || !*other.Power {
		t.Fatalf("batch must keep the other device's explicit ON result separate: %+v", other)
	}
	missing := Device{RemoteID: "did-missing"}
	applyMIoTDeviceResultForDID(&missing, refs, []byte(`{"code":0,"result":[{"siid":2,"piid":1,"code":0,"value":true}]}`), true)
	if missing.StateRead || missing.Power != nil {
		t.Fatalf("batch result without DID must not be assigned to an arbitrary device: %+v", missing)
	}
}

func TestEwelinkLiveRefreshPrefersFreshStatusOverCachedDeviceList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v2/device/thing":
			_, _ = io.WriteString(w, `{"error":0,"data":{"thingList":[{"itemType":1,"itemData":{"deviceid":"ew-live-1","name":"插座","productModel":"DIY_SWITCH","brandName":"Sonoff","online":true,"devicekey":"dk","params":{"switch":"on"}}}]}}`)
		case r.URL.Path == "/v2/device/thing/status":
			_, _ = io.WriteString(w, `{"error":0,"data":{"params":{"switch":"off"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	secret := providerSecret{AppID: "app", AppSecret: "secret", AccessToken: "access"}
	devices, _, err := ewelinkFetchDevicesWithClientAndKeysOptions(context.Background(), secret, server.Client(), server.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected one device, got %d", len(devices))
	}
	if !devices[0].StateRead {
		t.Fatal("status endpoint response should be marked fresh")
	}
	if devices[0].Power == nil || *devices[0].Power {
		t.Fatalf("fresh OFF should override cached ON, got %+v", devices[0].Power)
	}
}

func TestMIoTPowerStateNamesAndValues(t *testing.T) {
	for _, name := range []string{"on", "relay_state", "switch_state", "power_state", "outlet_state", "is_on"} {
		if !miotPowerStateName(miotNormalizedName(name)) {
			t.Errorf("power state name %q was not recognized", name)
		}
	}
	for raw, want := range map[any]bool{true: true, false: false, float64(1): true, float64(0): false, "ON": true, "off": false, "1": true, "0": false} {
		got, ok := miotBoolValue(raw)
		if !ok || got != want {
			t.Errorf("miotBoolValue(%v)=(%v,%v), want (%v,true)", raw, got, ok, want)
		}
	}
}

func TestProviderAccountCanPollAfterTransientSyncError(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   bool
	}{
		{status: "connected", want: true},
		{status: "sync_error", want: true},
		{status: "pending", want: false},
		{status: "disconnected", want: false},
		{status: " SYNC_ERROR ", want: true},
	} {
		if got := providerAccountCanPoll(tc.status); got != tc.want {
			t.Errorf("providerAccountCanPoll(%q)=%v, want %v", tc.status, got, tc.want)
		}
	}
}

func TestRefreshDeviceStatesAppliesOfflineStatus(t *testing.T) {
	a := testApp(t)
	power := true
	lastSeen := time.Now().Add(-time.Minute)
	if err := a.store.putDevice(Device{ID: "offline-poll-device", AccountID: "account-offline", Online: true, Power: &power, LastSeen: lastSeen}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.refreshDeviceStates("account-offline", []Device{{ID: "offline-poll-device", Online: false}}); err != nil {
		t.Fatal(err)
	}
	got := a.store.snapshot().Devices[0]
	if got.Online {
		t.Fatal("external offline status was not applied")
	}
	if got.Power == nil || !*got.Power {
		t.Fatal("missing power in an offline snapshot must not erase the last known switch state")
	}
	if !got.LastSeen.Equal(lastSeen) {
		t.Fatal("offline polling must not move last-seen time forward")
	}
}

func TestRefreshDeviceStatesDoesNotUpdateOtherAccountsOrReimport(t *testing.T) {
	a := testApp(t)
	power := true
	if err := a.store.putDevice(Device{ID: "kept-device", AccountID: "account-a", Online: true, Power: &power}); err != nil {
		t.Fatal(err)
	}
	off := false
	if _, err := a.store.refreshDeviceStates("account-b", []Device{{ID: "kept-device", Online: true, Power: &off}, {ID: "new-device", Online: true}}); err != nil {
		t.Fatal(err)
	}
	got := a.store.snapshot().Devices
	if len(got) != 1 || got[0].Power == nil || !*got[0].Power {
		t.Fatalf("polling must not cross accounts or auto-import devices: %+v", got)
	}
}

func TestLoginPageDoesNotDisplayDefaultCredentials(t *testing.T) {
	page, err := webFiles.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	text := string(page)
	for _, unwanted := range []string{`value="admin"`, "默认账号：admin", "默认密码：123456"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("login page still exposes default credential hint %q", unwanted)
		}
	}
}

func TestEnergyUIHidesUnavailablePeriodValues(t *testing.T) {
	page, err := webFiles.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(page)
	start := strings.Index(text, "function renderEnergyMetrics(energy)")
	end := strings.Index(text[start:], "function renderDeviceAttributes(d)")
	if start < 0 || end < 0 {
		t.Fatal("could not locate energy renderer in embedded app.js")
	}
	energyRenderer := text[start : start+end]
	if strings.Contains(energyRenderer, "—") || strings.Contains(energyRenderer, "metrics.push") {
		t.Fatal("energy renderer must not fabricate placeholder metrics")
	}
	start = strings.Index(text, "function overviewEnergy(d)")
	end = strings.Index(text[start:], "function renderDeviceCard(d)")
	if start < 0 || end < 0 {
		t.Fatal("could not locate overview energy renderer in embedded app.js")
	}
	overviewRenderer := text[start : start+end]
	if strings.Contains(overviewRenderer, "—") {
		t.Fatal("overview energy renderer must not show missing values as dashes")
	}
}

func TestStoreSnapshotDeepCopiesMIoTControls(t *testing.T) {
	a := testApp(t)
	device := Device{ID: "controls-copy", Controls: []DeviceControl{{
		ID: "prop:2:1", Kind: "property", Name: "工作模式", SIID: 2, PIID: 1,
		Options: []DeviceControlOption{{Value: float64(1), Label: "自动"}},
		Inputs:  []DeviceControlInput{{Name: "mode", Options: []DeviceControlOption{{Value: float64(2), Label: "静音"}}}},
	}}}
	if err := a.store.putDevice(device); err != nil {
		t.Fatal(err)
	}
	snapshot := a.store.snapshot()
	snapshot.Devices[0].Controls[0].Name = "外部修改"
	snapshot.Devices[0].Controls[0].Options[0].Label = "外部选项"
	snapshot.Devices[0].Controls[0].Inputs[0].Options[0].Label = "外部输入选项"
	got := a.store.snapshot().Devices[0].Controls[0]
	if got.Name != "工作模式" || got.Options[0].Label != "自动" || got.Inputs[0].Options[0].Label != "静音" {
		t.Fatalf("snapshot aliases MIoT controls: %+v", got)
	}
}

func TestGetDeviceDetailsEndpointIncludesDiscoveredControls(t *testing.T) {
	a := testApp(t)
	want := Device{ID: "plug-api", Name: "总插座", Protocol: "xiaomi-cloud", Controls: []DeviceControl{{
		ID: "prop:2:1", Kind: "property", Name: "电源开关", SIID: 2, PIID: 1, Format: "bool",
	}}}
	if err := a.store.putDevice(want); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/plug-api", nil)
	a.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET device status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got Device
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Controls) != 1 || got.Controls[0].SIID != 2 || got.Controls[0].PIID != 1 {
		t.Fatalf("device details did not include API capability metadata: %+v", got)
	}
}

func TestMIoTPowerValueUpdatesDevicePowerFromGenericControlRef(t *testing.T) {
	powerControl := DeviceControl{ID: "prop:3:2", Kind: "property", Name: "电源开关", SIID: 3, PIID: 2, Format: "bool"}
	device := Device{RemoteID: "plug-remote", Controls: []DeviceControl{powerControl}}
	refs := []miotDevicePropertyRef{{SIID: 3, PIID: 2, Field: "control", Format: "bool", ControlOnly: true}}
	body := []byte(`{"code":0,"result":[{"did":"plug-remote","siid":3,"piid":2,"code":0,"value":true}]}`)
	applyMIoTDeviceResult(&device, refs, body)
	if device.Power == nil || !*device.Power {
		t.Fatalf("MIoT writable relay_state should update Device.Power: %+v", device)
	}
	if device.Controls[0].Value != true {
		t.Fatalf("MIoT control current value was not updated: %+v", device.Controls[0])
	}
}

func TestFreshInstallHasNoDemoDevices(t *testing.T) {
	a := testApp(t)
	if got := len(a.store.snapshot().Devices); got != 0 {
		t.Fatalf("fresh install should not seed fake devices, got %d", got)
	}
}

func TestManualDeviceIsNotFalselyControllable(t *testing.T) {
	a := testApp(t)
	d := Device{ID: "manual-1", Name: "真实插座档案", Brand: "小米", Protocol: "manual", Demo: false}
	if err := a.store.putDevice(d); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"command":"turn_on"}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/devices/manual-1/command", bytes.NewReader(body))
	a.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rr.Code)
	}
}

func TestXiaomiDeviceWithoutWritableCapabilityIsRejected(t *testing.T) {
	a := testApp(t)
	if err := a.store.putDevice(Device{ID: "xiaomi-readonly", AccountID: "account-1", RemoteID: "did-1", Model: "demo.plug", Protocol: "xiaomi-cloud", CanControl: false}); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/devices/xiaomi-readonly/command", strings.NewReader(`{"command":"turn_on"}`))
	a.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("expected unsupported capability to return 501, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "没有识别到可写开关属性") {
		t.Fatalf("expected explicit MIoT capability explanation, got %s", rr.Body.String())
	}
}

func TestEwelinkImportedSwitchIsMarkedControllable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v2/device/thing" {
			t.Fatalf("unexpected list request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":0,"msg":"","data":{"thingList":[{"itemType":1,"itemData":{"deviceid":"sw-1","name":"Kitchen switch","productModel":"BASICR2","online":true,"params":{"switch":"off"}}}]}}`))
	}))
	defer server.Close()
	devices, _, err := ewelinkFetchDevicesWithClientAndKeys(context.Background(), providerSecret{AccessToken: "at"}, server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || !devices[0].CanControl {
		t.Fatalf("expected supported eWeLink switch to be controllable: %+v", devices)
	}
}

func TestEwelinkCommandUsesDocumentedControlEndpoint(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":0,"msg":"success"}`))
	}))
	defer server.Close()
	secret := providerSecret{Region: "cn", AppID: ewelinkAppID, AccessToken: "test-access-token"}
	if err := ewelinkCommandWithClient(context.Background(), secret, "device-123", "turn_on", server.Client(), server.URL); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/v2/device/thing/status" {
		t.Fatalf("unexpected command endpoint: %s %s", gotMethod, gotPath)
	}
	if gotAuth != "Bearer test-access-token" {
		t.Fatalf("unexpected auth header: %q", gotAuth)
	}
	var payload struct {
		Type   int               `json:"type"`
		ID     string            `json:"id"`
		Params map[string]string `json:"params"`
	}
	if err := json.Unmarshal([]byte(gotBody), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Type != 1 || payload.ID != "device-123" || payload.Params["switch"] != "on" {
		t.Fatalf("unexpected control payload: %s", gotBody)
	}
}

func TestOldDemoDevicesAreRemovedAndEwelinkControlMigrated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatal(err)
	}
	old := diskState{
		Devices: []Device{
			{ID: "demo-sonoff-switch", Name: "fake", Demo: true, Protocol: "demo"},
			{ID: "ewelink-switch", AccountID: "account-1", RemoteID: "remote-1", Protocol: "ewelink-cloud", Power: boolPtr(false), CanControl: false},
		},
		Accounts: []Account{}, Discovery: []Discovery{}, Logs: []LogEntry{},
	}
	b, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gohome.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := newStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := store.snapshot().Devices
	if len(got) != 1 || got[0].ID != "ewelink-switch" || !got[0].CanControl {
		t.Fatalf("migration did not clean demos / enable control: %+v", got)
	}
}

func boolPtr(v bool) *bool { return &v }

func TestStableV1APIEndpoints(t *testing.T) {
	a := testApp(t)
	for _, path := range []string{"/api/v1/devices", "/api/v1/openapi.json"} {
		rr := httptest.NewRecorder()
		a.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: status=%d body=%s", path, rr.Code, rr.Body.String())
		}
	}
}

func TestLegacyAccountNoteEndpointIsDisabled(t *testing.T) {
	a := testApp(t)
	body := []byte(`{"provider":"xiaomi","label":"测试米家账号","region":"cn"}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/accounts", bytes.NewReader(body))
	a.ServeHTTP(rr, req)
	if rr.Code != http.StatusGone {
		t.Fatalf("expected old placeholder endpoint to be disabled, status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestEmptyCollectionsReturnJSONArraysNotNull(t *testing.T) {
	a := testApp(t)
	a.store.mu.Lock()
	a.store.data.Devices = nil
	a.store.data.Accounts = nil
	a.store.data.Discovery = nil
	a.store.data.Logs = nil
	a.store.mu.Unlock()

	for _, path := range []string{"/api/devices", "/api/accounts", "/api/discovery", "/api/logs"} {
		t.Run(path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			a.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
			}
			if got := bytes.TrimSpace(rr.Body.Bytes()); !bytes.Equal(got, []byte("[]")) {
				t.Fatalf("expected JSON [] for empty collection, got %q", got)
			}
		})
	}
}

func TestStorePersistsData(t *testing.T) {
	dir := t.TempDir()
	s, err := newStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.putDevice(Device{ID: "persist-1", Name: "持久化设备"}); err != nil {
		t.Fatal(err)
	}
	s2, err := newStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range s2.snapshot().Devices {
		if d.ID == "persist-1" {
			found = true
		}
	}
	if !found {
		t.Fatal("device was not persisted")
	}
	if _, err := os.Stat(filepath.Join(dir, "gohome.json")); err != nil {
		t.Fatal(err)
	}
}

func TestEwelinkDeviceListUsesOfficialGetEndpoint(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.RequestURI(), r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":0,"msg":"","data":{"total":2,"thingList":[{"itemType":1,"itemData":{"deviceid":"1001","name":"客厅插座","productModel":"S26R2","brandName":"SONOFF","online":true,"devicekey":"local-key-1","params":{"switch":"on"},"family":{"roomid":"客厅"}}},{"itemType":3,"itemData":{"deviceid":"group-1","name":"组"}}]}}`))
	}))
	defer server.Close()

	secret := providerSecret{Provider: "ewelink", Region: "cn", AppID: "app-id", AppSecret: "app-secret", AccessToken: "access-token"}
	devices, keys, err := ewelinkFetchDevicesWithClientAndKeys(context.Background(), secret, server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet || gotPath != "/v2/device/thing?lang=cn&num=0" {
		t.Fatalf("unexpected request %s %s", gotMethod, gotPath)
	}
	if gotAuth != "Bearer access-token" {
		t.Fatalf("unexpected Authorization header: %q", gotAuth)
	}
	if len(devices) != 1 || devices[0].RemoteID != "1001" || devices[0].Power == nil || !*devices[0].Power || !devices[0].CanControl {
		t.Fatalf("unexpected parsed devices: %+v", devices)
	}
	if keys["1001"] != "local-key-1" {
		t.Fatalf("device key not parsed: %#v", keys)
	}
}

func TestEwelinkSyncFetchesMissingOnlineSwitchStatus(t *testing.T) {
	var statusCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/device/thing":
			if r.Method != http.MethodGet {
				t.Fatalf("unexpected list method: %s", r.Method)
			}
			_, _ = w.Write([]byte(`{"error":0,"msg":"","data":{"thingList":[{"itemType":1,"itemData":{"deviceid":"status-1","name":"Hall switch","online":true,"params":{}}}]}}`))
		case "/v2/device/thing/status":
			statusCalls++
			if r.Method != http.MethodGet || r.URL.Query().Get("id") != "status-1" {
				t.Fatalf("unexpected status query: %s %s", r.Method, r.URL.String())
			}
			if r.Header.Get("Authorization") != "Bearer access-token" {
				t.Fatalf("missing bearer token: %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"error":0,"msg":"success","data":{"params":{"switch":"on"}}}`))
		default:
			t.Fatalf("unexpected endpoint: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	devices, _, err := ewelinkFetchDevicesWithClientAndKeys(context.Background(), providerSecret{AccessToken: "access-token"}, server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if statusCalls != 1 {
		t.Fatalf("expected one fallback status query, got %d", statusCalls)
	}
	if len(devices) != 1 || !devices[0].CanControl || devices[0].Power == nil || !*devices[0].Power {
		t.Fatalf("expected status-enriched controllable switch, got %+v", devices)
	}
}

func TestEwelinkMeterSyncFetchesStatusWhenSwitchParamsExist(t *testing.T) {
	var statusCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/device/thing":
			_, _ = w.Write([]byte(`{"error":0,"msg":"","data":{"thingList":[{"itemType":1,"itemData":{"deviceid":"meter-1","name":"Living room power meter","productModel":"POWR3","online":true,"params":{"switch":"on"}}}]}}`))
		case "/v2/device/thing/status":
			statusCalls++
			_, _ = w.Write([]byte(`{"error":0,"msg":"success","data":{"params":{"switch":"on","power":56.7,"voltage":229.8,"current":0.24,"energy_day":0.8,"energy_month":12.4}}}`))
		default:
			t.Fatalf("unexpected endpoint: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	devices, _, err := ewelinkFetchDevicesWithClientAndKeys(context.Background(), providerSecret{AccessToken: "access-token"}, server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if statusCalls != 1 {
		t.Fatalf("metering model should trigger a status query even when switch params exist, got %d", statusCalls)
	}
	if len(devices) != 1 || devices[0].Energy == nil || devices[0].Energy.PowerW == nil || *devices[0].Energy.PowerW != 56.7 || devices[0].Energy.TodayKWh == nil || *devices[0].Energy.TodayKWh != 0.8 {
		t.Fatalf("expected electrical measurements to be imported: %+v", devices)
	}
}

func TestEwelinkStateRefreshQueriesLatestStatusEvenWhenListHasParams(t *testing.T) {
	var statusCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/device/thing":
			_, _ = w.Write([]byte(`{"error":0,"msg":"success","data":{"thingList":[{"itemType":1,"itemData":{"deviceid":"poll-plug-1","name":"External switch","productModel":"MINI-R4","brandName":"SONOFF","online":true,"devicekey":"device-key","params":{"switch":"on"}}}]}}`))
		case "/v2/device/thing/status":
			statusCalls++
			_, _ = w.Write([]byte(`{"error":0,"msg":"success","data":{"params":{"switch":"off"}}}`))
		default:
			t.Fatalf("unexpected endpoint: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	devices, _, err := ewelinkFetchDevicesWithClientAndKeysOptions(
		context.Background(),
		providerSecret{Provider: "ewelink", Region: "cn", AccessToken: "test-access-token", AppID: ewelinkAppID},
		server.Client(), server.URL, true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if statusCalls != 1 {
		t.Fatalf("expected one forced status query, got %d", statusCalls)
	}
	if len(devices) != 1 || devices[0].Power == nil || *devices[0].Power {
		t.Fatalf("expected latest external off state, got %+v", devices)
	}
}

func TestBuildXiaomiPostVerifyFormUsesOriginalSessionFields(t *testing.T) {
	fallback := url.Values{"callback": {"old-callback"}, "sid": {"xiaomiio"}, "qs": {"old-query"}, "_sign": {"old-sign"}}
	form := buildXiaomiPostVerifyForm("user@example.com", "secret", map[string]any{"callback": "new-callback", "sid": "xiaomiio"}, fallback)
	if form.Get("user") != "user@example.com" || form.Get("hash") != "5EBE2294ECD0E0F08EAB7690D2A6EE69" {
		t.Fatalf("unexpected Xiaomi auth identity/hash: %#v", form)
	}
	if form.Get("callback") != "new-callback" || form.Get("qs") != "old-query" || form.Get("_sign") != "old-sign" {
		t.Fatalf("unexpected Xiaomi serviceLogin fields: %#v", form)
	}
}

func TestXiaomiDeviceCookieIDIsSeparateSixLowercaseLetters(t *testing.T) {
	for i := 0; i < 10; i++ {
		got := xiaomiDeviceCookieID()
		if len(got) != 6 {
			t.Fatalf("deviceId cookie must be six chars, got %q", got)
		}
		for _, c := range got {
			if c < 'a' || c > 'z' {
				t.Fatalf("deviceId cookie must be lowercase letters, got %q", got)
			}
		}
	}
}

func TestEwelinkMultiChannelDiscovery(t *testing.T) {
	params := map[string]any{"switches": []any{
		map[string]any{"outlet": float64(0), "switch": "on"},
		map[string]any{"outlet": float64(1), "switch": "off"},
	}}
	channels := parseEwelinkChannels(params)
	if len(channels) != 2 || channels[0].Outlet != 0 || channels[1].Outlet != 1 {
		t.Fatalf("unexpected multi-channel parse: %+v", channels)
	}
	if channels[0].Power == nil || !*channels[0].Power || channels[1].Power == nil || *channels[1].Power {
		t.Fatalf("unexpected channel states: %+v", channels)
	}
}

func TestEwelinkMultiChannelCommandPayload(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":0,"msg":"success"}`))
	}))
	defer server.Close()
	secret := providerSecret{Region: "cn", AccessToken: "access"}
	if err := ewelinkCommandChannelWithClient(context.Background(), secret, "multi-1", "turn_on", 2, true, server.Client(), server.URL); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Params struct {
			Switches []struct {
				Outlet int    `json:"outlet"`
				Switch string `json:"switch"`
			} `json:"switches"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(gotBody), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Params.Switches) != 1 || payload.Params.Switches[0].Outlet != 2 || payload.Params.Switches[0].Switch != "on" {
		t.Fatalf("unexpected multi-channel command payload: %s", gotBody)
	}
}

func TestXiaomiOrderedAPIFormBodyPreservesClientFieldOrder(t *testing.T) {
	got := xiaomiOrderedAPIFormBody("DATA+1", "HASH/2", "SIGN=3", "SEC", "NONCE")
	wantPrefix := "data=DATA%2B1&rc4_hash__=HASH%2F2&signature=SIGN%3D3&ssecurity=SEC&_nonce=NONCE"
	if got != wantPrefix {
		t.Fatalf("unexpected ordered Xiaomi form body\n got: %s\nwant: %s", got, wantPrefix)
	}
}

func TestVaultEncryptsSecretsAtRest(t *testing.T) {
	dir := t.TempDir()
	v, err := newVault(dir)
	if err != nil {
		t.Fatal(err)
	}
	secret := providerSecret{Provider: "ewelink", AppSecret: "super-secret-app-secret", AccessToken: "private-access-token", RefreshToken: "private-refresh-token"}
	if err := v.Put("account-1", secret); err != nil {
		t.Fatal(err)
	}
	ciphertext, err := os.ReadFile(filepath.Join(dir, "gohome.vault.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, plain := range []string{"super-secret-app-secret", "private-access-token", "private-refresh-token"} {
		if strings.Contains(string(ciphertext), plain) {
			t.Fatalf("plaintext secret leaked into vault file: %s", plain)
		}
	}
	v2, err := newVault(dir)
	if err != nil {
		t.Fatal(err)
	}
	var loaded providerSecret
	if err := v2.Get("account-1", &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.AccessToken != secret.AccessToken || loaded.AppSecret != secret.AppSecret {
		t.Fatalf("vault round trip failed: %+v", loaded)
	}
}

func TestXiaomiIdentityListURLKeepsContextAndRejectsExternalHost(t *testing.T) {
	got, err := xiaomiIdentityListURL("https://account.xiaomi.com/fe/service/identity/authStart?sid=xiaomiio&context=abc123")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "https://account.xiaomi.com/identity/list?") || !strings.Contains(got, "context=abc123") {
		t.Fatalf("identity context was not preserved: %s", got)
	}
	if _, err := xiaomiIdentityListURL("https://attacker.example/fe/service/identity/authStart?context=x"); err == nil {
		t.Fatal("expected external Xiaomi identity URL to be rejected")
	}
}

func TestEwelinkConsumerLoginPayloadNeedsNoDeveloperCredentials(t *testing.T) {
	body, country, err := buildEwelinkLoginPayload(ewelinkLoginInput{
		Username: "13800000000", Password: "test-password", CountryCode: "+86", Region: "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	if country != "+86" {
		t.Fatalf("unexpected country code: %q", country)
	}
	if got, want := string(body), `{"countryCode":"+86","password":"test-password","phoneNumber":"+8613800000000"}`; got != want {
		t.Fatalf("unexpected consumer login payload\n got: %s\nwant: %s", got, want)
	}
	if strings.Contains(string(body), "appId") || strings.Contains(string(body), "appSecret") {
		t.Fatalf("consumer login payload must not require developer credentials: %s", body)
	}

	emailBody, _, err := buildEwelinkLoginPayload(ewelinkLoginInput{
		Username: "home@example.com", Password: "test-password", CountryCode: "+86",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(emailBody), `{"countryCode":"+86","email":"home@example.com","password":"test-password"}`; got != want {
		t.Fatalf("unexpected email login payload\n got: %s\nwant: %s", got, want)
	}
}

func TestEwelinkCountryCodeSelectsAutomaticRegion(t *testing.T) {
	cases := map[string]string{
		"+86": "cn", "+65": "as", "+1": "us", "+44": "eu", "+49": "eu", "+852": "as",
	}
	for code, want := range cases {
		if got := ewelinkRegionForCountry(code, "auto"); got != want {
			t.Errorf("country %s: got region %q, want %q", code, got, want)
		}
	}
	if got := ewelinkRegionForCountry("+86", "us"); got != "us" {
		t.Fatalf("explicit region override was ignored: %q", got)
	}
}

func TestEwelinkSonoffLANCompatibleSignature(t *testing.T) {
	if got := len(ewelinkSigningKey()); got != 32 {
		t.Fatalf("SonoffLAN compatibility signing key must be 32 bytes, got %d", got)
	}
	body := []byte(`{"countryCode":"+86","password":"test-password","phoneNumber":"+8613800000000"}`)
	const want = "JS9p7KsqvYUzGTWFJBV9cJ7S4PjY/6KM5tPbDoXmQ7M="
	if got := signHMAC("", body); got != want {
		t.Fatalf("unexpected eWeLink signing vector: %s", got)
	}
	req := httptest.NewRequest(http.MethodPost, "https://cn-apia.coolkit.cn/v2/user/login", bytes.NewReader(body))
	setEwelinkHeaders(req, "", "", body, false)
	if req.Header.Get("X-CK-Appid") != ewelinkAppID {
		t.Fatalf("unexpected eWeLink AppID: %q", req.Header.Get("X-CK-Appid"))
	}
	if req.Header.Get("Authorization") != "Sign "+want {
		t.Fatalf("unexpected eWeLink authorization header: %q", req.Header.Get("Authorization"))
	}
}

func TestDecodeXiaomiJSONHandlesAntiHijackPrefix(t *testing.T) {
	got, err := decodeXiaomiJSON([]byte(`&&&START&&&{"code":0,"flag":4,"options":[4,8]}`))
	if err != nil {
		t.Fatal(err)
	}
	if intValue(got["code"], -1) != 0 || intValue(got["flag"], -1) != 4 {
		t.Fatalf("unexpected Xiaomi JSON result: %#v", got)
	}
}

func TestDecodeXiaomiJSONRejectsHTMLWithoutLeakingBody(t *testing.T) {
	_, err := decodeXiaomiJSON([]byte(`<!doctype html><html><body>security challenge</body></html>`))
	if err == nil || !strings.Contains(err.Error(), "网页而不是 JSON") {
		t.Fatalf("expected clear HTML challenge error, got %v", err)
	}
}

func TestXiaomiIdentityListUsesRootPathAndPreservesContext(t *testing.T) {
	got, err := xiaomiIdentityListURL("https://account.xiaomi.com/fe/service/identity/authStart?sid=xiaomiio&context=keep-me")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/identity/list" || u.Query().Get("context") != "keep-me" {
		t.Fatalf("wrong Xiaomi identity list URL: %s", got)
	}
}

func TestXiaomiRC4SignaturePreservesMIoTParameterOrder(t *testing.T) {
	const signed = "c2lnbmVkLW5vbmNl"
	ordered := xiaomiSHA1SignFields("POST", "/home/device_list", []string{"data={}", "rc4_hash__=hash"}, signed)
	reversed := xiaomiSHA1SignFields("POST", "/home/device_list", []string{"rc4_hash__=hash", "data={}"}, signed)
	if ordered == reversed {
		t.Fatal("Xiaomi RC4 signature must preserve insertion order")
	}
	if got, want := xiaomiSHA1SignFields("POST", "/home/device_list", []string{"data={}"}, signed), ""; got == want {
		t.Fatal("signature unexpectedly empty")
	}
}

func TestXiaomiRC4RoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef")
	plain := `{"getVirtualModel":true}`
	encoded, err := xiaomiRC4Encode(key, plain)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := xiaomiRC4Decode(key, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != plain {
		t.Fatalf("RC4 roundtrip mismatch: %q", decoded)
	}
}

func TestXiaomiIdentityPendingRetainsPasswordOnlyInMemory(t *testing.T) {
	client := &http.Client{}
	form := url.Values{"sid": {"xiaomiio"}}
	pending := newXiaomiIdentityPending("challenge", "user@example.com", "secret-password", "cn", client, form, "https://account.xiaomi.com/fe/service/identity/authStart?context=opaque")
	if pending.Password != "secret-password" {
		t.Fatal("password must remain in the short-lived in-memory challenge so serviceLoginAuth2 can finish after SMS/email verification")
	}
	if pending.Client != client || pending.Form.Get("sid") != "xiaomiio" {
		t.Fatal("identity challenge must preserve its original HTTP session and login form")
	}
}

func TestIntegrationLogRedactsCredentialsAndXiaomiContext(t *testing.T) {
	input := `login failed https://account.xiaomi.com/auth?context=one-time&ticket=12345 password=hunter2 {"serviceToken":"abc","ssecurity":"xyz"}`
	got := sanitizeIntegrationLog(input)
	for _, secret := range []string{"one-time", "12345", "hunter2", "abc", "xyz"} {
		if strings.Contains(got, secret) {
			t.Fatalf("log sanitizer leaked %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected redaction marker, got %s", got)
	}
}

func TestIntegrationErrorsAreAddedToPersistentLogs(t *testing.T) {
	a := testApp(t)
	a.logIntegrationError("小米", "账号登录", errors.New("模拟登录错误 password=secret"))
	logs := a.store.snapshot().Logs
	if len(logs) == 0 || !strings.Contains(logs[0].Message, "模拟登录错误") {
		t.Fatalf("integration failure missing from persisted logs: %+v", logs)
	}
	if strings.Contains(logs[0].Message, "secret") {
		t.Fatalf("password leaked into logs: %s", logs[0].Message)
	}
}

func TestXiaomiLocationNormalization(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"root-relative", "/sts?ticket=abc", "https://account.xiaomi.com/sts?ticket=abc"},
		{"protocol-relative", "//sts.api.io.mi.com/sts?ticket=abc", "https://sts.api.io.mi.com/sts?ticket=abc"},
		{"host-without-scheme", "sts.api.io.mi.com/sts?ticket=abc", "https://sts.api.io.mi.com/sts?ticket=abc"},
		{"encoded-absolute", "https%3A%2F%2Fsts.api.io.mi.com%2Fsts%3Fticket%3Dabc", "https://sts.api.io.mi.com/sts?ticket=abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := xiaomiTrustedURL(tt.raw, "https://account.xiaomi.com/", false)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("normalized URL=%q want %q", got, tt.want)
			}
		})
	}
	if _, err := xiaomiTrustedURL("https://evil.example/sts", "https://account.xiaomi.com/", false); err == nil {
		t.Fatal("untrusted redirect host must remain rejected")
	}
}

func TestXiaomiConfirmURLAndSTSHostValidation(t *testing.T) {
	finalURL := "https://account.xiaomi.com/fe/service/verifyPhone?confirmUrl=https%3A%2F%2Faccount.xiaomi.com%2Fpass%2FserviceLoginAuth2%2Fend"
	got, err := xiaomiConfirmURLFromFinal(finalURL)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://account.xiaomi.com/pass/serviceLoginAuth2/end" {
		t.Fatalf("unexpected confirmUrl: %s", got)
	}
	if _, err := xiaomiConfirmURLFromFinal("https://account.xiaomi.com/verify?confirmUrl=https%3A%2F%2Fevil.example%2Fsteal"); err == nil {
		t.Fatal("expected untrusted confirmUrl to be rejected")
	}
	if _, err := xiaomiTrustedURL("https://sts.api.io.mi.com/sts?sign=x", "https://account.xiaomi.com/", false); err != nil {
		t.Fatalf("trusted Xiaomi STS host rejected: %v", err)
	}
	if _, err := xiaomiTrustedURL("https://evilmi.com/sts", "https://account.xiaomi.com/", false); err == nil {
		t.Fatal("expected unrelated host to be rejected")
	}
}

func TestXiaomiResumeURLSupportsCurrentSkipURL(t *testing.T) {
	finalURL := "https://account.xiaomi.com/fe/service/verifyPhone?skipUrl=https%3A%2F%2Faccount.xiaomi.com%2Fpass%2FserviceLoginAuth2%2Fend"
	got, err := xiaomiResumeURLFromFinal(finalURL)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://account.xiaomi.com/pass/serviceLoginAuth2/end" {
		t.Fatalf("unexpected skipUrl: %s", got)
	}
	if _, err := xiaomiResumeURLFromFinal("https://account.xiaomi.com/fe/service/verifyPhone?skipUrl=https%3A%2F%2Fevil.example%2Fsteal"); err == nil {
		t.Fatal("expected untrusted skipUrl to be rejected")
	}
	if _, err := xiaomiTrustedURL("https://api2.mina.mi.com/sts?sign=x", "https://account.xiaomi.com/", false); err != nil {
		t.Fatalf("current Xiaomi STS host rejected: %v", err)
	}
}

func TestXiaomiServiceTokenCookieCanBeReadAtCallbackPath(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	callback, _ := url.Parse("https://account.xiaomi.com/identity/result/check")
	jar.SetCookies(callback, []*http.Cookie{{Name: "serviceToken", Value: "path-scoped-token", Path: "/identity/result"}})
	client := &http.Client{Jar: jar}
	if got := xiaomiCookieValueAt(client, callback.String(), "serviceToken"); got != "path-scoped-token" {
		t.Fatalf("could not read path-scoped callback serviceToken: %q", got)
	}
	if got := xiaomiCookieValueAt(client, "https://account.xiaomi.com/", "serviceToken"); got != "" {
		t.Fatalf("cookie path filtering should prevent reading callback cookie at root, got %q", got)
	}
}

func TestXiaomiServiceTokenCookieCanBeReadFromMinaSTSJar(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	stsURL, _ := url.Parse("https://api2.mina.mi.com/")
	jar.SetCookies(stsURL, []*http.Cookie{{Name: "serviceToken", Value: "test-service-token", Path: "/"}})
	client := &http.Client{Jar: jar}
	if got := xiaomiCookieValue(client, "serviceToken"); got != "test-service-token" {
		t.Fatalf("could not read serviceToken from current STS host: %q", got)
	}
}

func TestMIoTGenericWhEnergyMapsToLifetimeCounter(t *testing.T) {
	field, scale := miotEnergyPropertyField("energy", "Electricity", "float", "Wh")
	if field != "total" || scale != 0.001 {
		t.Fatalf("generic MIoT Wh energy should map to cumulative kWh: field=%q scale=%v", field, scale)
	}
	field, _ = miotEnergyPropertyField("power", "Power switch", "bool", "")
	if field != "" {
		t.Fatalf("boolean switch must not map to power meter: %q", field)
	}
}

func TestMIoTModelIndexCachesNegativeLookups(t *testing.T) {
	miotSpecCache.Lock()
	oldAt, oldModels := miotSpecCache.indexAt, miotSpecCache.models
	miotSpecCache.indexAt = time.Now()
	miotSpecCache.models = map[string]string{"known.model": "urn:miot-spec-v2:device:known:0000A001:1"}
	miotSpecCache.Unlock()
	t.Cleanup(func() {
		miotSpecCache.Lock()
		miotSpecCache.indexAt, miotSpecCache.models = oldAt, oldModels
		miotSpecCache.Unlock()
	})
	if _, err := fetchMIoTModelType(context.Background(), "missing.model"); err == nil || !strings.Contains(err.Error(), "未收录") {
		t.Fatalf("cached missing model should fail without network refresh, got %v", err)
	}
}

func TestMIoTEnergySpecDoesNotTreatSwitchPowerAsWatts(t *testing.T) {
	field, _ := miotEnergyPropertyField("power", "Power switch", "bool", "")
	if field != "" {
		t.Fatalf("boolean power state must not be reported as watts, got %q", field)
	}
	field, scale := miotEnergyPropertyField("active_power", "Active power", "float", "W")
	if field != "power" || scale != 1 {
		t.Fatalf("valid power meter property was not recognized: %q, %v", field, scale)
	}
}

func TestXiaomiAccountDeviceIDFormat(t *testing.T) {
	id := xiaomiAccountDeviceID()
	if len(id) != 16 || strings.ToUpper(id) != id {
		t.Fatalf("invalid Xiaomi account deviceId: %q", id)
	}
	for _, r := range id {
		if !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", r) {
			t.Fatalf("unexpected character in deviceId: %q", id)
		}
	}
}

type testRoundTripper func(*http.Request) (*http.Response, error)

func (fn testRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestXiaomiNonceIsExactly12Bytes(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	randomPart := []byte{0, 1, 2, 3, 4, 5, 6, 7}
	got, err := xiaomiNonceAt(randomPart, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 12 {
		t.Fatalf("nonce length=%d, want 12", len(got))
	}
	if !bytes.Equal(got[:8], randomPart) {
		t.Fatalf("nonce random prefix mismatch: %x", got[:8])
	}
	if minute := binary.BigEndian.Uint32(got[8:]); uint64(minute) != uint64(now.Unix()/60) {
		t.Fatalf("nonce minute=%d, want %d", minute, now.Unix()/60)
	}
	encoded, raw, err := xiaomiGenerateNonce()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(decoded) != 12 || !bytes.Equal(decoded, raw) {
		t.Fatalf("generated nonce is not canonical 12-byte base64: len=%d err=%v", len(decoded), err)
	}
}

func TestXiaomiUserAgentUsesStableClientID(t *testing.T) {
	const id = "7FNJC2OVCB0IE39J"
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(mustURL("https://account.xiaomi.com/"), []*http.Cookie{{Name: "deviceId", Value: id, Path: "/"}})
	client := &http.Client{Jar: jar}
	want := "Android-7.1.1-1.0.0-ONEPLUS A3010-136-" + id + " APP/xiaomi.smarthome APPV/62830"
	if got := xiaomiUserAgentForClient(client); got != want {
		t.Fatalf("login User-Agent=%q, want %q", got, want)
	}
	if got := xiaomiAPIUserAgent(id); got != want {
		t.Fatalf("API User-Agent=%q, want same session User-Agent %q", got, want)
	}
}

func TestXiaomiAPICookiesMatchMiotAutoSession(t *testing.T) {
	secret := providerSecret{UserID: "123456", ServiceToken: "fresh-token", DeviceID: "7FNJC2OVCB0IE39J"}
	jar, err := xiaomiAPICookieJar("https://api.io.mi.com", secret)
	if err != nil {
		t.Fatal(err)
	}
	cookies := map[string]string{}
	for _, c := range jar.Cookies(mustURL("https://api.io.mi.com/app/home/device_list")) {
		cookies[c.Name] = c.Value
	}
	for name, want := range map[string]string{
		"userId": "123456", "serviceToken": "fresh-token", "yetAnotherServiceToken": "fresh-token",
		"locale": "zh_CN", "channel": "MI_APP_STORE",
	} {
		if cookies[name] != want {
			t.Fatalf("cookie %s=%q want %q; all=%v", name, cookies[name], want, cookies)
		}
	}
	if _, ok := cookies["deviceId"]; ok {
		t.Fatalf("API cookies should match HA api_cookies and omit Passport deviceId: %v", cookies)
	}
	if _, ok := cookies["sdkVersion"]; ok {
		t.Fatalf("API cookies should not leak Passport sdkVersion: %v", cookies)
	}
}

func TestXiaomiAuthProbeDistinguishesAcceptedAndRejectedToken(t *testing.T) {
	secret := providerSecret{UserID: "123456", ServiceToken: "token", DeviceID: "7FNJC2OVCB0IE39J"}
	status := http.StatusUnauthorized
	probeBody := `{"code":3,"message":"auth err"}`
	client := &http.Client{Transport: testRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api2.mina.mi.com" || req.URL.Path != "/admin/v2/device_list" {
			t.Fatalf("unexpected HA auth probe endpoint: %s", req.URL)
		}
		if got := req.Header.Get("User-Agent"); got != xiaomiAPIUserAgent(secret.DeviceID) {
			t.Fatalf("auth probe User-Agent=%q, want %q", got, xiaomiAPIUserAgent(secret.DeviceID))
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(probeBody)), Request: req}, nil
	})}
	got := xiaomiProbeAuthWithClient(context.Background(), secret, client)
	if !strings.Contains(got, "HTTP 401") || !strings.Contains(got, "拒绝此 serviceToken/userId") {
		t.Fatalf("unexpected rejected-token probe result: %s", got)
	}
	status = http.StatusOK
	probeBody = `{"code":0,"result":{"list":[]}}`
	got = xiaomiProbeAuthWithClient(context.Background(), secret, client)
	if !strings.Contains(got, "HTTP 200") || !strings.Contains(got, "已被 mina 服务接受") {
		t.Fatalf("unexpected accepted-token probe result: %s", got)
	}
}

func TestXiaomiSTSUsesOnlyFreshFinalResponseCookies(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	stsURL := "https://sts.api.io.mi.com/sts"
	jar.SetCookies(mustURL(stsURL), []*http.Cookie{{Name: "serviceToken", Value: "old-stale-token", Path: "/"}})
	client := &http.Client{Jar: jar, Transport: testRoundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
	})}
	if _, err := xiaomiFetchServiceSession(context.Background(), client, stsURL); err == nil || !strings.Contains(err.Error(), "没有签发 serviceToken") {
		t.Fatalf("must reject stale CookieJar token when final STS response has no token, got %v", err)
	}

	client.Transport = testRoundTripper(func(req *http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Add("Set-Cookie", "serviceToken=fresh-sts-token; Path=/; Secure")
		header.Add("Set-Cookie", "userId=987654; Path=/; Secure")
		header.Add("Set-Cookie", "cUserId=user-abc; Path=/; Secure")
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
	})
	got, err := xiaomiFetchServiceSession(context.Background(), client, stsURL)
	if err != nil {
		t.Fatal(err)
	}
	if got.ServiceToken != "fresh-sts-token" || got.UserID != "987654" || got.CUserID != "user-abc" {
		t.Fatalf("unexpected STS session: %+v", got)
	}
}

func TestXiaomiMIoTEnergySpecMapping(t *testing.T) {
	var spec miotSpecDocument
	payload := []byte(`{"services":[{"iid":4,"name":"电能统计","properties":[{"iid":1,"name":"today_energy","description":"Daily energy","unit":"kWh","access":["read"]},{"iid":2,"name":"current","description":"Current","type":"current","unit":"mA","access":["read"]},{"iid":3,"name":"voltage","description":"Voltage","type":"voltage","unit":"mV","access":["read"]},{"iid":4,"name":"power","description":"Active power","type":"power","unit":"W","access":["read"]},{"iid":5,"name":"energy_total","description":"Total energy","type":"energy","unit":"Wh","access":["read"]}]}]}`)
	if err := json.Unmarshal(payload, &spec); err != nil {
		t.Fatal(err)
	}
	refs := miotReadEnergyRefs(spec)
	if len(refs) != 5 {
		t.Fatalf("expected five meter properties, got %+v", refs)
	}
	energy := &EnergyMetrics{}
	result := []byte(`{"code":0,"result":[{"siid":4,"piid":1,"code":0,"value":1.25},{"siid":4,"piid":2,"code":0,"value":250},{"siid":4,"piid":3,"code":0,"value":220000},{"siid":4,"piid":4,"code":0,"value":36.8},{"siid":4,"piid":5,"code":0,"value":12000}]}`)
	applyMIoTEnergyResult(energy, refs, result)
	if energy.TodayKWh == nil || *energy.TodayKWh != 1.25 {
		t.Fatalf("today energy missing: %+v", energy)
	}
	if energy.CurrentA == nil || *energy.CurrentA != 0.25 {
		t.Fatalf("current unit conversion failed: %+v", energy)
	}
	if energy.VoltageV == nil || *energy.VoltageV != 220 {
		t.Fatalf("voltage unit conversion failed: %+v", energy)
	}
	if energy.PowerW == nil || *energy.PowerW != 36.8 {
		t.Fatalf("power missing: %+v", energy)
	}
	if energy.TotalKWh == nil || *energy.TotalKWh != 12 {
		t.Fatalf("Wh to kWh conversion failed: %+v", energy)
	}
}

func TestEwelinkEnergyMetricParsing(t *testing.T) {
	energy := parseEwelinkEnergyMetrics(map[string]any{
		"power": 42.5, "voltage": 230.1, "current": 0.18,
		"energy_day": 0.42, "energy_week": 2.3, "energy_month": 8.4,
		"energy_year": 44.2, "energy": 1.7,
	}, "POWR3")
	if energy == nil || !energy.Supported {
		t.Fatalf("expected meter metrics, got %+v", energy)
	}
	if energy.PowerW == nil || *energy.PowerW != 42.5 {
		t.Fatalf("power not parsed: %+v", energy)
	}
	if energy.TodayKWh == nil || *energy.TodayKWh != 0.42 {
		t.Fatalf("today kWh not parsed: %+v", energy)
	}
	if energy.HistoryKWh == nil || *energy.HistoryKWh != 1.7 {
		t.Fatalf("historical energy not parsed: %+v", energy)
	}
	if energy.TotalKWh != nil {
		t.Fatalf("generic eWeLink energy must not be mislabeled as lifetime total: %+v", energy)
	}
}

func TestStoreSnapshotDeepCopiesEnergyMetrics(t *testing.T) {
	a := testApp(t)
	power := 18.5
	if err := a.store.putDevice(Device{ID: "meter", Energy: &EnergyMetrics{Supported: true, PowerW: &power}}); err != nil {
		t.Fatal(err)
	}
	snapshot := a.store.snapshot()
	*snapshot.Devices[0].Energy.PowerW = 999
	stored := a.store.snapshot().Devices[0].Energy.PowerW
	if stored == nil || *stored != 18.5 {
		t.Fatalf("energy snapshot leaked mutable pointer: %v", stored)
	}
}

func TestGuessCategoryRecognizesXiaomiDeviceClasses(t *testing.T) {
	cases := []struct{ name, model, want string }{
		{"小米智能室外摄像机 4C 3K", "", "摄像头"},
		{"小爱触屏音箱", "", "音箱"},
		{"卧室小米温湿度计", "", "温湿度计"},
		{"米家智能插座", "chuangmi.plug.212a01", "插座"},
		{"米家互联网空调", "", "空调"},
		{"米家扫拖机器人 3C", "", "扫地机器人"},
		{"米家风扇", "", "风扇"},
		{"小米手环8 NFC版", "", "穿戴设备"},
		{"米家智能台式洗碗机5套S1", "", "洗碗机"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := guessCategory(tc.name, tc.model); got != tc.want {
				t.Fatalf("category=%q want %q", got, tc.want)
			}
		})
	}
}

func TestMIoTGenericPropertiesAndEnergyAreParsed(t *testing.T) {
	var spec miotSpecDocument
	fixture := []byte(`{"services":[{"iid":2,"name":"environment","properties":[{"iid":1,"name":"temperature","description":"Temperature","type":"urn:miot-spec-v2:property:temperature:00000020","format":"float","unit":"celsius","access":["read"]},{"iid":2,"name":"relative_humidity","description":"Relative Humidity","format":"uint8","unit":"percentage","access":["read"]},{"iid":3,"name":"battery_level","description":"Battery Level","format":"uint8","unit":"percentage","access":["read"]}]},{"iid":3,"name":"speaker","properties":[{"iid":1,"name":"volume","description":"Volume","format":"uint8","unit":"percentage","access":["read"]},{"iid":2,"name":"is_volume_muted","description":"Is Volume Muted","format":"bool","access":["read"]}]},{"iid":4,"name":"electrical","properties":[{"iid":1,"name":"electric_power","description":"Electric Power","format":"float","unit":"watt","access":["read"]},{"iid":2,"name":"power_consumption","description":"Power Consumption","format":"float","unit":"watt-hour","access":["read"]}]}]}`)
	if err := json.Unmarshal(fixture, &spec); err != nil {
		t.Fatal(err)
	}
	refs := miotReadDeviceRefs(spec)
	if len(refs) != 7 {
		t.Fatalf("expected 7 mapped refs, got %d: %+v", len(refs), refs)
	}
	var body = []byte(`{"code":0,"result":[{"did":"sensor-1","siid":2,"piid":1,"code":0,"value":23.5},{"did":"sensor-1","siid":2,"piid":2,"code":0,"value":48},{"did":"sensor-1","siid":2,"piid":3,"code":0,"value":92},{"did":"sensor-1","siid":3,"piid":1,"code":0,"value":35},{"did":"sensor-1","siid":3,"piid":2,"code":0,"value":false},{"did":"sensor-1","siid":4,"piid":1,"code":0,"value":88.2},{"did":"sensor-1","siid":4,"piid":2,"code":0,"value":12500}]}`)
	d := Device{RemoteID: "sensor-1"}
	applyMIoTDeviceResult(&d, refs, body)
	if len(d.Attributes) != 5 {
		t.Fatalf("expected 5 display attributes, got %+v", d.Attributes)
	}
	if d.Value == "" || !strings.Contains(d.Value, "23.5") {
		t.Fatalf("device summary missing temperature: %q", d.Value)
	}
	if d.Energy == nil || d.Energy.PowerW == nil || *d.Energy.PowerW != 88.2 {
		t.Fatalf("power metric missing: %+v", d.Energy)
	}
	if d.Energy.TotalKWh == nil || *d.Energy.TotalKWh != 12.5 {
		t.Fatalf("Wh to kWh conversion wrong: %+v", d.Energy)
	}
}

func TestMIoTValueFormattingKeepsIntegerTrailingZeroes(t *testing.T) {
	ref := miotDevicePropertyRef{Field: "volume", Scale: 1}
	if got := miotFormatAttribute(ref, float64(100)); got != "100" {
		t.Fatalf("volume format=%q want 100", got)
	}
}

func TestGuessCategoryWithMIoTType(t *testing.T) {
	cases := []struct{ name, model, typ, want string }{
		{"Xiaomi device", "some.model", "urn:miot-spec-v2:device:intelligent-speaker:0000A001:demo:1", "音箱"},
		{"小冰箱", "some.model", "", "插座"},
		{"总插座", "unknown.model", "", "插座"},
		{"空调伴侣插座", "lumi.acpartner.mcn02", "", "插座"},
	}
	for _, tc := range cases {
		if got := guessCategoryWithMIoT(tc.name, tc.model, tc.typ); got != tc.want {
			t.Errorf("guessCategoryWithMIoT(%q, %q, %q)=%q want %q", tc.name, tc.model, tc.typ, got, tc.want)
		}
	}
}

func TestEwelinkDeviceAttributesExposeSensorReadings(t *testing.T) {
	attrs := parseEwelinkDeviceAttributes(map[string]any{"temperature": 24.5, "humidity": 48, "battery": 91, "motion": true})
	if len(attrs) != 4 {
		t.Fatalf("expected four device attributes, got %+v", attrs)
	}
	device := Device{Attributes: attrs}
	if got := summarizeDeviceAttributes(&device); got == "" || !strings.Contains(got, "24.5") || !strings.Contains(got, "48") {
		t.Fatalf("unexpected sensor summary: %q", got)
	}
}

func TestImportDevicesPreservesPreviousAttributesWhenRefreshHasNone(t *testing.T) {
	a := testApp(t)
	old := Device{ID: "xiaomi-sensor", Name: "卧室温湿度计", Brand: "小米", Category: "温湿度计", Attributes: []DeviceAttribute{{Key: "temperature", Label: "温度", Value: "23.5", Unit: "°C"}}, Value: "温度 23.5°C"}
	if err := a.store.putDevice(old); err != nil {
		t.Fatal(err)
	}
	m := &integrationManager{store: a.store}
	m.importDevices("account-1", "xiaomi", []Device{{ID: "xiaomi-sensor", Name: "卧室温湿度计", Brand: "小米", Category: "其他", Protocol: "xiaomi-cloud"}})
	got := a.store.snapshot().Devices[0]
	if got.Category != "温湿度计" || len(got.Attributes) != 1 || got.Attributes[0].Value != "23.5" || got.Value == "" {
		t.Fatalf("cached detail was not preserved: %+v", got)
	}
}

func TestMIoTSupportedMeterShowsEvenWhenNoValuesReturned(t *testing.T) {
	device := Device{RemoteID: "plug-1"}
	refs := []miotDevicePropertyRef{{SIID: 5, PIID: 1, Field: "energy:power", Label: "实时功率", Unit: "W", Scale: 1}}
	applyMIoTDeviceResult(&device, refs, []byte(`{"code":0,"result":[]}`))
	if device.Energy == nil || !device.Energy.Supported || device.Energy.PowerW != nil {
		t.Fatalf("meter capability should be visible without inventing a value: %+v", device.Energy)
	}
}

func TestStoreSnapshotDeepCopiesDeviceAttributes(t *testing.T) {
	a := testApp(t)
	if err := a.store.putDevice(Device{ID: "sensor-copy", Attributes: []DeviceAttribute{{Key: "temperature", Label: "温度", Value: "22.5", Unit: "°C"}}}); err != nil {
		t.Fatal(err)
	}
	snap := a.store.snapshot()
	snap.Devices[0].Attributes[0].Value = "mutated"
	got := a.store.snapshot().Devices[0].Attributes[0].Value
	if got != "22.5" {
		t.Fatalf("snapshot aliases mutable device attributes: %q", got)
	}
}

func TestXiaomiWritableSwitchCapability(t *testing.T) {
	var spec miotSpecDocument
	// Build via JSON to keep the fixture readable and close to upstream MIoT spec JSON.
	fixture := []byte(`{"services":[{"iid":2,"properties":[{"iid":1,"name":"on","format":"bool","access":["read","write","notify"]},{"iid":2,"name":"power","format":"float","access":["read"]}]}]}`)
	if err := json.Unmarshal(fixture, &spec); err != nil {
		t.Fatal(err)
	}
	ref, ok := xiaomiWritableSwitch(spec)
	if !ok || ref.SIID != 2 || ref.PIID != 1 {
		t.Fatalf("writable switch not detected: %+v, %v", ref, ok)
	}
}

func TestMIoTDisplayColorTemperatureAndCategory(t *testing.T) {
	field, label, unit, _ := miotDisplayProperty("color-temperature", "", "light", "kelvin")
	if field != "color_temperature" || label != "色温" || unit != "K" {
		t.Fatalf("color temperature misclassified: %q %q %q", field, label, unit)
	}
	if got := guessCategoryWithMIoT("空调伴侣插座", "lumi.acpartner.mcn02", ""); got != "插座" {
		t.Fatalf("AC companion plug misclassified: %q", got)
	}
}

func TestMIoTMeterEligibilityDoesNotMarkCamerasAndSpeakers(t *testing.T) {
	for _, category := range []string{"摄像头", "音箱", "温湿度计", "穿戴设备", "网络设备", "其他"} {
		if miotMeterEligible(category) {
			t.Errorf("category %q must not be marked as an electrical meter by default", category)
		}
	}
	for _, category := range []string{"插座", "空调", "风扇"} {
		if !miotMeterEligible(category) {
			t.Errorf("category %q should allow real meter attributes", category)
		}
	}
}

func TestListDevicesReturnsLogicalProviderGroups(t *testing.T) {
	a := testApp(t)
	if err := a.store.putDevice(Device{ID: "xm1", Name: "plug", Brand: "小米", Protocol: "xiaomi-cloud"}); err != nil {
		t.Fatal(err)
	}
	if err := a.store.putDevice(Device{ID: "ew1", Name: "switch", Brand: "coolkit", Protocol: "ewelink-cloud"}); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	a.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/devices", nil))
	var devices []Device
	if err := json.Unmarshal(rr.Body.Bytes(), &devices); err != nil {
		t.Fatal(err)
	}
	groups := map[string]string{}
	for _, d := range devices {
		groups[d.ID] = d.ProviderGroup
	}
	if groups["xm1"] != "小米 / 米家" || groups["ew1"] != "易微联 / eWeLink" {
		t.Fatalf("provider groups not normalized: %#v", groups)
	}
}

func TestParseXiaomiDailyEnergyStatsAggregatesTodayAndWeek(t *testing.T) {
	now := time.Date(2026, time.November, 24, 18, 0, 0, 0, time.Local)
	weekStart := startOfLocalWeek(now)
	monday := weekStart.Add(10 * time.Hour).Unix()
	today := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, time.Local).Unix()
	previousSunday := weekStart.Add(-2 * time.Hour).Unix()
	body := []byte(fmt.Sprintf(`{"code":0,"message":"ok","result":[{"time":%d,"value":"[10,5]"},{"time":%d,"value":"[30,2]"},{"time":%d,"value":"[100]"}]}`, monday, today, previousSunday))
	day, week, err := parseXiaomiDailyEnergyStats(body, weekStart, now, 0.01)
	if err != nil {
		t.Fatal(err)
	}
	if day == nil || math.Abs(*day-0.32) > 1e-9 {
		t.Fatalf("today total = %v, want 0.32 kWh", day)
	}
	if week == nil || math.Abs(*week-0.47) > 1e-9 {
		t.Fatalf("week total = %v, want 0.47 kWh", week)
	}
}

func TestXiaomiEnergyStatsProfilesAreModelSpecific(t *testing.T) {
	profile, ok := xiaomiEnergyStatsProfileForModel("cuco.plug.cp2d")
	if !ok || len(profile.Keys) != 1 || profile.Keys[0] != "3.1" || profile.Scale != 0.01 {
		t.Fatalf("unexpected CP2D statistics profile: %+v, %v", profile, ok)
	}
	if _, ok := xiaomiEnergyStatsProfileForModel("unknown.smart.plug"); ok {
		t.Fatal("unknown models must not get a guessed Xiaomi statistics key")
	}
}

func TestEwelinkDayWeekKWhAliasesAndScale(t *testing.T) {
	energy := parseEwelinkEnergyMetrics(map[string]any{
		"dayKwh":   42,
		"weekKwh":  245,
		"monthKwh": 931,
		"yearKwh":  5000,
	}, "POWR3")
	if energy == nil || energy.TodayKWh == nil || math.Abs(*energy.TodayKWh-0.42) > 1e-9 {
		t.Fatalf("dayKwh scaling failed: %+v", energy)
	}
	if energy.WeekKWh == nil || math.Abs(*energy.WeekKWh-2.45) > 1e-9 {
		t.Fatalf("weekKwh scaling failed: %+v", energy)
	}
	if energy.MonthKWh == nil || math.Abs(*energy.MonthKWh-9.31) > 1e-9 {
		t.Fatalf("monthKwh scaling failed: %+v", energy)
	}
}

func TestEwelinkPeriodEnergyReadingsRequiresDayAndWeek(t *testing.T) {
	if ewelinkHasPeriodEnergyReadings(map[string]any{"dayKwh": 12}) {
		t.Fatal("daily-only values must still trigger a query for weekly energy")
	}
	if !ewelinkHasPeriodEnergyReadings(map[string]any{"dayKwh": 12, "weekKwh": 40}) {
		t.Fatal("dayKwh and weekKwh should satisfy period energy detection")
	}
}

func TestEwelinkOfficialDailyAndMonthlyKWhFields(t *testing.T) {
	energy := parseEwelinkEnergyMetrics(map[string]any{
		"dailyKwh":   135,
		"monthlyKwh": 2460,
		"power":      63.0,
	}, "POWR3")
	if energy == nil {
		t.Fatal("expected energy metrics")
	}
	if energy.TodayKWh == nil || math.Abs(*energy.TodayKWh-1.35) > 1e-9 {
		t.Fatalf("dailyKwh must be converted from 0.01 kWh units: %+v", energy)
	}
	if energy.MonthKWh == nil || math.Abs(*energy.MonthKWh-24.60) > 1e-9 {
		t.Fatalf("monthlyKwh must be converted from 0.01 kWh units: %+v", energy)
	}
	if energy.WeekKWh != nil {
		t.Fatalf("official daily/monthly fields must not be mislabeled as weekly energy: %+v", energy)
	}
}

func TestEwelinkNoValueSentinelDoesNotBecomeEnergy(t *testing.T) {
	energy := parseEwelinkEnergyMetrics(map[string]any{
		"dailyKwh": -99999,
		"power":    63.0,
	}, "POWR3")
	if energy == nil || energy.TodayKWh != nil {
		t.Fatalf("-99999 is the documented no-value sentinel: %+v", energy)
	}
}

func TestRecordEnergySampleSeedsAndIntegratesDayAndWeek(t *testing.T) {
	a := testApp(t)
	power := 120.0
	if err := a.store.putDevice(Device{
		ID: "meter-sample", Name: "测试计量插座", Protocol: "ewelink-cloud", Online: true,
		Energy: &EnergyMetrics{Supported: true, PowerW: &power, Source: "eWeLink"},
	}); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 24, 12, 0, 0, 0, time.Local)
	first := &EnergyMetrics{Supported: true, PowerW: &power, Source: "eWeLink"}
	if err := a.store.recordEnergySample("meter-sample", first, start); err != nil {
		t.Fatal(err)
	}
	firstState := a.store.snapshot().Devices[0].Energy
	if firstState.TodayKWh == nil || *firstState.TodayKWh != 0 {
		t.Fatalf("first sample should establish a visible zero baseline: %+v", firstState)
	}
	if firstState.WeekKWh == nil || *firstState.WeekKWh != 0 {
		t.Fatalf("first sample should establish weekly baseline: %+v", firstState)
	}

	if err := a.store.recordEnergySample("meter-sample", first, start.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	updated := a.store.snapshot().Devices[0].Energy
	want := 120.0 * 5.0 / 60.0 / 1000.0
	if updated.TodayKWh == nil || math.Abs(*updated.TodayKWh-want) > 1e-9 {
		t.Fatalf("today energy = %v, want %.8f kWh", updated.TodayKWh, want)
	}
	if updated.WeekKWh == nil || math.Abs(*updated.WeekKWh-want) > 1e-9 {
		t.Fatalf("week energy = %v, want %.8f kWh", updated.WeekKWh, want)
	}
	if !strings.Contains(updated.TodaySource, "首次采集") || !strings.Contains(updated.WeekSource, "本机每日累计") {
		t.Fatalf("expected clear local-estimate source labels: %+v", updated)
	}
}

func TestEwelinkOfficialDailyKWhUnderscoreAlias(t *testing.T) {
	energy := parseEwelinkEnergyMetrics(map[string]any{"daily_kwh": 100}, "POWR3")
	if energy == nil || energy.TodayKWh == nil || math.Abs(*energy.TodayKWh-1.0) > 1e-9 {
		t.Fatalf("daily_kwh alias should normalize to 1.0 kWh: %+v", energy)
	}
}

func TestRecordEnergySampleDerivesTodayAndWeekFromMonthlyCounter(t *testing.T) {
	a := testApp(t)
	if err := a.store.putDevice(Device{
		ID: "meter-month-counter", Name: "月累计测试插座", Protocol: "ewelink-cloud", Online: true,
		Energy: &EnergyMetrics{Supported: true, Source: "eWeLink"},
	}); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 24, 12, 0, 0, 0, time.Local)
	first := &EnergyMetrics{Supported: true, MonthKWh: floatPointer(12.30), Source: "eWeLink"}
	if err := a.store.recordEnergySample("meter-month-counter", first, start); err != nil {
		t.Fatal(err)
	}
	baseline := a.store.snapshot().Devices[0].Energy
	if baseline.TodayKWh == nil || *baseline.TodayKWh != 0 || baseline.WeekKWh == nil || *baseline.WeekKWh != 0 {
		t.Fatalf("first month counter sample should establish explicit zero baselines: %+v", baseline)
	}
	second := &EnergyMetrics{Supported: true, MonthKWh: floatPointer(12.38), Source: "eWeLink"}
	if err := a.store.recordEnergySample("meter-month-counter", second, start.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	updated := a.store.snapshot().Devices[0].Energy
	if updated.TodayKWh == nil || math.Abs(*updated.TodayKWh-0.08) > 1e-9 {
		t.Fatalf("month counter delta should become today's kWh: %+v", updated)
	}
	if updated.WeekKWh == nil || math.Abs(*updated.WeekKWh-0.08) > 1e-9 {
		t.Fatalf("month counter delta should be included in the current week: %+v", updated)
	}
	if !strings.Contains(updated.TodaySource, "月累计差值") {
		t.Fatalf("expected monthly counter source label, got %q", updated.TodaySource)
	}
}

func TestRecordEnergySampleUsesRecentPowerWhenCloudOmitsPower(t *testing.T) {
	a := testApp(t)
	power := 60.0
	if err := a.store.putDevice(Device{
		ID: "meter-stale-cloud", Name: "云端不推送功率的插座", Protocol: "ewelink-cloud", Online: true,
		Energy: &EnergyMetrics{Supported: true, PowerW: &power, Source: "eWeLink"},
	}); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 24, 12, 0, 0, 0, time.Local)
	if err := a.store.recordEnergySample("meter-stale-cloud", &EnergyMetrics{Supported: true, PowerW: &power}, start); err != nil {
		t.Fatal(err)
	}
	// A later status response carries voltage/current but no fresh power value.
	voltage := 230.0
	current := 0.3
	if err := a.store.recordEnergySample("meter-stale-cloud", &EnergyMetrics{Supported: true, VoltageV: &voltage, CurrentA: &current}, start.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	energy := a.store.snapshot().Devices[0].Energy
	want := 60.0 * 5.0 / 60.0 / 1000.0
	if energy.TodayKWh == nil || math.Abs(*energy.TodayKWh-want) > 1e-9 {
		t.Fatalf("today estimate = %v, want %.6f kWh", energy.TodayKWh, want)
	}
	if energy.WeekKWh == nil || math.Abs(*energy.WeekKWh-want) > 1e-9 {
		t.Fatalf("week estimate = %v, want %.6f kWh", energy.WeekKWh, want)
	}
	if !strings.Contains(energy.TodaySource, "最近一次有效读数") {
		t.Fatalf("expected stale-power estimate label, got %q", energy.TodaySource)
	}
}

func TestRecordEnergySampleDoesNotExtendStalePowerBeyondLimit(t *testing.T) {
	a := testApp(t)
	power := 60.0
	if err := a.store.putDevice(Device{
		ID: "meter-power-expired", Name: "过期功率样本插座", Protocol: "ewelink-cloud", Online: true,
		Energy: &EnergyMetrics{Supported: true, PowerW: &power, Source: "eWeLink"},
	}); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 24, 12, 0, 0, 0, time.Local)
	if err := a.store.recordEnergySample("meter-power-expired", &EnergyMetrics{Supported: true, PowerW: &power}, start); err != nil {
		t.Fatal(err)
	}
	if err := a.store.recordEnergySample("meter-power-expired", &EnergyMetrics{Supported: true}, start.Add(20*time.Minute)); err != nil {
		t.Fatal(err)
	}
	energy := a.store.snapshot().Devices[0].Energy
	if energy.TodayKWh == nil || *energy.TodayKWh != 0 {
		t.Fatalf("stale power must not be extrapolated beyond 15 minutes: %+v", energy)
	}
}

func TestRecordEnergySamplePreservesImportedCurrentDayAndCloudWeekTotals(t *testing.T) {
	a := testApp(t)
	power := 63.0
	today := 1.23
	week := 4.56
	start := time.Date(2026, 11, 24, 12, 0, 0, 0, time.Local)
	if err := a.store.putDevice(Device{
		ID: "meter-imported-counters", Name: "已有云端统计插座", Protocol: "ewelink-cloud", Online: true,
		Energy: &EnergyMetrics{Supported: true, PowerW: &power, TodayKWh: &today, WeekKWh: &week, WeekSource: "eWeLink 云端周统计", UpdatedAt: start},
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.store.recordEnergySample("meter-imported-counters", &EnergyMetrics{Supported: true, PowerW: &power}, start.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	energy := a.store.snapshot().Devices[0].Energy
	if energy.TodayKWh == nil || math.Abs(*energy.TodayKWh-today) > 1e-9 {
		t.Fatalf("same-day cloud daily total should be retained: %+v", energy)
	}
	if energy.WeekKWh == nil || math.Abs(*energy.WeekKWh-week) > 1e-9 {
		t.Fatalf("current-week cloud total should not be replaced by a partial local sum: %+v", energy)
	}
}

func TestPageActivityLeasesSupportMultipleTabs(t *testing.T) {
	m := &integrationManager{pageLeases: map[string]time.Time{}}
	if got := m.updatePageActivity("web-tab-00000001", true); got != 1 {
		t.Fatalf("first active tab count=%d want=1", got)
	}
	if got := m.updatePageActivity("web-tab-00000002", true); got != 2 {
		t.Fatalf("two active tabs count=%d want=2", got)
	}
	if got := m.updatePageActivity("web-tab-00000001", false); got != 1 {
		t.Fatalf("hiding first tab count=%d want=1", got)
	}
	if got := m.activePageCount(time.Now()); got != 1 {
		t.Fatalf("remaining visible tab count=%d want=1", got)
	}
	if got := m.updatePageActivity("web-tab-00000002", false); got != 0 {
		t.Fatalf("all tabs hidden count=%d want=0", got)
	}
}

func TestPageActivityLeaseExpires(t *testing.T) {
	now := time.Now()
	m := &integrationManager{pageLeases: map[string]time.Time{"web-tab-00000001": now.Add(-time.Second)}}
	if got := m.activePageCount(now); got != 0 {
		t.Fatalf("expired page leases count=%d want=0", got)
	}
}

func TestProviderPollingRequiresActivePage(t *testing.T) {
	m := &integrationManager{}
	if m.startProviderStatePollIfNeeded(context.Background(), time.Now()) {
		t.Fatal("polling must not start with no visible page")
	}
	m.updatePageActivity("web-tab-00000001", true)
	if !m.startProviderStatePollIfNeeded(context.Background(), time.Now()) {
		t.Fatal("polling should start immediately after a visible page becomes active")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		m.activityMu.Lock()
		running := m.pollRunning
		m.activityMu.Unlock()
		if !running {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("test poll did not finish")
}

func TestStateSyncActivityEndpoint(t *testing.T) {
	a := testApp(t)
	req := httptest.NewRequest(http.MethodPost, "/api/state-sync/activity", strings.NewReader(`{"clientId":"web-tab-00000001","active":true}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	a.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("activity endpoint status=%d body=%s", resp.Code, resp.Body.String())
	}
	if got := a.integrations.activePageCount(time.Now()); got != 1 {
		t.Fatalf("active page count=%d want=1", got)
	}
}

func TestStateSyncActivityEndpointRequiresLogin(t *testing.T) {
	a := testApp(t)
	a.authDisabled = false
	req := httptest.NewRequest(http.MethodPost, "/api/state-sync/activity", strings.NewReader(`{"clientId":"web-tab-00000001","active":true}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	a.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated activity endpoint status=%d want=%d", resp.Code, http.StatusUnauthorized)
	}
}
