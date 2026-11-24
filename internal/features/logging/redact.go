// Package logging contains shared sanitization helpers for persisted log lines.
package logging

import "regexp"

var querySecrets = regexp.MustCompile(`(?i)(password|passwd|serviceToken|ssecurity|accessToken|refreshToken|token|ticket|context|captcha)=([^&\s,}]+)`)
var jsonSecrets = regexp.MustCompile(`(?i)("(?:password|passwd|serviceToken|ssecurity|accessToken|refreshToken|token|ticket|context|captcha)"\s*:\s*")[^"]*(")`)

// Redact removes credentials and one-time authentication values before logging.
func Redact(message string) string {
	message = querySecrets.ReplaceAllString(message, "$1=[REDACTED]")
	return jsonSecrets.ReplaceAllString(message, "$1[REDACTED]$2")
}
