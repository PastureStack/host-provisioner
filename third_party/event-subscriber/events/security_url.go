package events

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var eventSubscriptionURLPattern = regexp.MustCompile(`^wss?://(?:\[[0-9A-Fa-f:.%]+\]|[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)(?::[0-9]{1,5})?(?:/[A-Za-z0-9._~!$&'()*+,;=:@%/-]*)?(?:\?[A-Za-z0-9._~!$&'()*+,;=:@%/?-]*)?$`)

func buildEventSubscriptionURL(apiURL string) (string, error) {
	base, err := url.Parse(apiURL)
	if err != nil || base.Opaque != "" || base.User != nil || base.Hostname() == "" {
		return "", fmt.Errorf("event API URL must contain a valid origin")
	}
	switch strings.ToLower(base.Scheme) {
	case "http":
		base.Scheme = "ws"
	case "https":
		base.Scheme = "wss"
	default:
		return "", fmt.Errorf("unsupported event API URL scheme %q", base.Scheme)
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/subscribe"
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""
	return validateEventSubscriptionURL(apiURL, base.String(), nil)
}

func validateEventSubscriptionURL(apiURL, candidateURL string, data url.Values) (string, error) {
	if !eventSubscriptionURLPattern.MatchString(candidateURL) {
		return "", fmt.Errorf("event subscription URL has an unsupported format")
	}
	base, err := url.Parse(apiURL)
	if err != nil || base.Opaque != "" || base.User != nil || base.Hostname() == "" {
		return "", fmt.Errorf("event API URL must contain a valid origin")
	}
	target, err := url.Parse(candidateURL)
	if err != nil || target.Opaque != "" || target.User != nil || target.Hostname() == "" {
		return "", fmt.Errorf("event subscription URL must contain a valid origin")
	}
	expectedScheme := "ws"
	if strings.EqualFold(base.Scheme, "https") {
		expectedScheme = "wss"
	} else if !strings.EqualFold(base.Scheme, "http") {
		return "", fmt.Errorf("unsupported event API URL scheme %q", base.Scheme)
	}
	if !strings.EqualFold(target.Scheme, expectedScheme) || !strings.EqualFold(target.Host, base.Host) {
		return "", fmt.Errorf("event subscription URL crosses the configured origin")
	}
	query := target.Query()
	for key, values := range data {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	target.RawQuery = query.Encode()
	if !eventSubscriptionURLPattern.MatchString(target.String()) {
		return "", fmt.Errorf("event subscription URL failed validation")
	}
	return target.String(), nil
}
