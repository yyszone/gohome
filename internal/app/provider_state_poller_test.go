package app

import (
	"strings"
	"testing"
)

func TestSummarizeProviderDeviceStateIssuesExplainsUnknownMIoTModel(t *testing.T) {
	devices := []Device{
		{
			Name:       "小米手环3 NFC版",
			Model:      "hmpace.bracelet.v3nfc",
			StateIssue: "MIoT 规格库未收录型号 hmpace.bracelet.v3nfc",
		},
		{
			Name:  "总插座",
			Model: "chuangmi.plug.m1",
		},
	}

	summary, count := summarizeProviderDeviceStateIssues(devices, 5)
	if count != 1 {
		t.Fatalf("issue count = %d, want 1", count)
	}
	for _, expected := range []string{
		"小米手环3 NFC版（hmpace.bracelet.v3nfc）",
		"型号暂未收录到 MIoT 规格库",
		"不是设备离线",
		"不是账号登录失败",
	} {
		if !strings.Contains(summary, expected) {
			t.Errorf("summary %q does not contain %q", summary, expected)
		}
	}
}

func TestSummarizeProviderDeviceStateIssuesIncludesOtherDeviceErrors(t *testing.T) {
	devices := []Device{
		{Name: "路由器", Model: "xiaomi.router.rb03", StateIssue: "MIoT prop/get 请求超时"},
		{Name: "扫地机器人", Model: "xiaomi.vacuum.c103", StateIssue: "读取 MIoT 设备规格失败"},
	}

	summary, count := summarizeProviderDeviceStateIssues(devices, 1)
	if count != 2 {
		t.Fatalf("issue count = %d, want 2", count)
	}
	if !strings.Contains(summary, "路由器（xiaomi.router.rb03）：MIoT prop/get 请求超时") {
		t.Fatalf("first issue missing from summary: %s", summary)
	}
	if !strings.Contains(summary, "另有 1 台设备未展开") {
		t.Fatalf("truncated issue count missing from summary: %s", summary)
	}
}

func TestSummarizeProviderDeviceStateIssuesEmptyWhenAllDevicesSucceed(t *testing.T) {
	summary, count := summarizeProviderDeviceStateIssues([]Device{{Name: "总插座"}, {Name: "小冰箱"}}, 5)
	if summary != "" || count != 0 {
		t.Fatalf("summary=%q count=%d, want empty/0", summary, count)
	}
}

func TestProviderDeviceStateLogMessageDoesNotImplyAccountFailure(t *testing.T) {
	message := providerDeviceStateLogMessage("xiaomi", []Device{{
		Name:       "小米手环3 NFC版",
		Model:      "hmpace.bracelet.v3nfc",
		StateIssue: "MIoT 规格库未收录型号 hmpace.bracelet.v3nfc",
	}})
	for _, expected := range []string{
		"[小米 / 米家]",
		"有 1 台设备实时状态未同步",
		"账号连接正常",
		"其他设备继续同步",
		"小米手环3 NFC版",
		"型号暂未收录到 MIoT 规格库",
	} {
		if !strings.Contains(message, expected) {
			t.Errorf("log message %q does not contain %q", message, expected)
		}
	}
	if strings.Contains(message, "账号同步失败") || strings.Contains(message, "登录失效") {
		t.Fatalf("device capability warning looks like an account failure: %s", message)
	}
}
