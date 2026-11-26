package app

import (
	"fmt"
	"net/url"
	"strings"
)

// validateCameraURL accepts browser-playable HTTP(S) sources only. RTSP and
// Xiaomi private P2P URLs need a separate gateway and cannot be played by a
// normal browser video element directly.
func validateCameraURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > 2048 {
		return "", fmt.Errorf("摄像头视频地址不能超过 2048 个字符")
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || u == nil || u.Host == "" {
		return "", fmt.Errorf("请输入完整的视频 HTTP/HTTPS 地址")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("浏览器预览只支持 HTTP/HTTPS；RTSP 需要转为 HLS 或 MJPEG")
	}
	if u.User != nil {
		return "", fmt.Errorf("视频地址不能包含用户名或密码")
	}
	return u.String(), nil
}
