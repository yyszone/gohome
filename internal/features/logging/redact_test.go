package logging

import (
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	got := Redact(`failed password=abc serviceToken=xyz {"ssecurity":"secret"}`)
	for _, secret := range []string{"abc", "xyz", "secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("secret leaked: %s", got)
		}
	}
}
