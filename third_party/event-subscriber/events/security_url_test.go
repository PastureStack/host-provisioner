package events

import (
	"net/url"
	"testing"
)

func TestEventSubscriptionURLBindsConfiguredOrigin(t *testing.T) {
	base, err := buildEventSubscriptionURL("https://api.example.test/v2-beta")
	if err != nil {
		t.Fatal(err)
	}
	query := url.Values{"eventNames": {"ping", "host.change"}}
	target, err := validateEventSubscriptionURL("https://api.example.test/v2-beta", base, query)
	if err != nil {
		t.Fatal(err)
	}
	if !eventSubscriptionURLPattern.MatchString(target) {
		t.Fatalf("validated target did not match the request policy: %s", target)
	}
	if _, err := validateEventSubscriptionURL(
		"https://api.example.test/v2-beta",
		"wss://metadata.example.test/v2-beta/subscribe",
		nil,
	); err == nil {
		t.Fatal("cross-origin event subscription URL was accepted")
	}
}
