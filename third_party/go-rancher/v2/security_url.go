package client

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var (
	rancherHTTPURLPattern      = regexp.MustCompile(`^https?://(?:\[[0-9A-Fa-f:.%]+\]|[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)(?::[0-9]{1,5})?(?:/[A-Za-z0-9._~!$&'()*+,;=:@%/-]*)?(?:\?[A-Za-z0-9._~!$&'()*+,;=:@%/?-]*)?$`)
	rancherWebSocketURLPattern = regexp.MustCompile(`^wss?://(?:\[[0-9A-Fa-f:.%]+\]|[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)(?::[0-9]{1,5})?(?:/[A-Za-z0-9._~!$&'()*+,;=:@%/-]*)?(?:\?[A-Za-z0-9._~!$&'()*+,;=:@%/?-]*)?$`)
)

func rancherOrigin(raw string) (string, string, string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || u.Hostname() == "" {
		return "", "", "", fmt.Errorf("Rancher URL must contain a valid origin")
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "ws":
		scheme = "http"
	case "wss":
		scheme = "https"
	case "http", "https":
	default:
		return "", "", "", fmt.Errorf("Rancher URL has an unsupported scheme")
	}
	port := u.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return scheme, strings.ToLower(u.Hostname()), port, nil
}

func validateRancherRequestURL(baseURL, candidateURL string, websocketRequest bool) (string, error) {
	pattern := rancherHTTPURLPattern
	if websocketRequest {
		pattern = rancherWebSocketURLPattern
	}
	if !pattern.MatchString(candidateURL) {
		return "", fmt.Errorf("Rancher request URL has an unsupported format")
	}
	baseScheme, baseHost, basePort, err := rancherOrigin(baseURL)
	if err != nil {
		return "", err
	}
	targetScheme, targetHost, targetPort, err := rancherOrigin(candidateURL)
	if err != nil {
		return "", err
	}
	if baseScheme != targetScheme || baseHost != targetHost || basePort != targetPort {
		return "", fmt.Errorf("Rancher request URL crosses the configured origin")
	}
	return candidateURL, nil
}

func newRancherRequest(baseURL, method, candidateURL string, body io.Reader) (*http.Request, error) {
	target, err := validateRancherRequestURL(baseURL, candidateURL, false)
	if err != nil {
		return nil, err
	}
	if !rancherHTTPURLPattern.MatchString(target) {
		return nil, fmt.Errorf("Rancher request URL failed validation")
	}
	return http.NewRequest(method, target, body)
}

func safeRancherURLForLog(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[invalid URL]"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
