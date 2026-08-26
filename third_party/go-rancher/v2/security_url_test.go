package client

import (
	"strings"
	"testing"
)

func TestValidateRancherRequestURLBindsConfiguredOrigin(t *testing.T) {
	base := "https://api.example.test:8443/v2-beta"
	accepted, err := validateRancherRequestURL(base, "https://api.example.test:8443/v2-beta/projects?limit=10", false)
	if err != nil || accepted == "" {
		t.Fatalf("same-origin URL rejected: %q, %v", accepted, err)
	}
	for _, candidate := range []string{
		"https://metadata.example.test/latest",
		"https://user:secret@api.example.test:8443/v2-beta",
		"file:///etc/passwd",
	} {
		if _, err := validateRancherRequestURL(base, candidate, false); err == nil {
			t.Fatalf("unsafe URL accepted: %s", candidate)
		}
	}
}

func TestSafeRancherURLForLogRemovesSecrets(t *testing.T) {
	value := safeRancherURLForLog("https://user:password@example.test/path?token=secret#fragment")
	for _, secret := range []string{"user", "password", "token", "secret", "fragment"} {
		if strings.Contains(value, secret) {
			t.Fatalf("safe URL still contains %q: %s", secret, value)
		}
	}
}
