package utils

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const UnsupportedProxyProtocolMessage = "Unsupported proxy protocol. SOCKS and PAC proxy URLs are not supported; use an HTTP or HTTPS proxy URL."

var proxyHostWithPortPattern = regexp.MustCompile(`^(.+):(\d+)$`)

var defaultProxyPorts = map[string]int{
	"ftp":    21,
	"gopher": 70,
	"http":   80,
	"https":  443,
	"ws":     80,
	"wss":    443,
}

func ResolveHTTPProxyURLForTarget(targetURL string) (*url.URL, error) {
	proxy, err := proxyForURL(targetURL)
	if err != nil || proxy == "" {
		return nil, err
	}
	proxyURL, err := url.Parse(proxy)
	if err != nil {
		return nil, fmt.Errorf("Invalid proxy URL %q: %w", proxy, err)
	}
	if proxyURL.Scheme != "http" && proxyURL.Scheme != "https" {
		return nil, fmt.Errorf("%s Got %s:", UnsupportedProxyProtocolMessage, proxyURL.Scheme)
	}
	return proxyURL, nil
}

func CreateHTTPProxyClientForTarget(targetURL string) (*http.Client, error) {
	proxyURL, err := ResolveHTTPProxyURLForTarget(targetURL)
	if err != nil || proxyURL == nil {
		return nil, err
	}
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}, nil
}

func proxyForURL(targetURL string) (string, error) {
	parsedURL, err := url.Parse(targetURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return "", nil
	}
	protocol := parsedURL.Scheme
	hostname := parsedURL.Hostname()
	port := targetPort(parsedURL)
	if !shouldProxyHostname(hostname, port) {
		return "", nil
	}
	proxy := proxyEnv(protocol + "_proxy")
	if proxy == "" {
		proxy = proxyEnv("all_proxy")
	}
	if proxy != "" && !strings.Contains(proxy, "://") {
		proxy = protocol + "://" + proxy
	}
	return proxy, nil
}

func targetPort(targetURL *url.URL) int {
	if port := targetURL.Port(); port != "" {
		if parsed, err := strconv.Atoi(port); err == nil {
			return parsed
		}
	}
	return defaultProxyPorts[targetURL.Scheme]
}

func proxyEnv(key string) string {
	if value := os.Getenv(strings.ToLower(key)); value != "" {
		return value
	}
	return os.Getenv(strings.ToUpper(key))
}

func shouldProxyHostname(hostname string, port int) bool {
	noProxy := strings.ToLower(proxyEnv("no_proxy"))
	if noProxy == "" {
		return true
	}
	if noProxy == "*" {
		return false
	}
	for _, item := range regexp.MustCompile(`[,\s]`).Split(noProxy, -1) {
		if item == "" {
			continue
		}
		if !proxyEntryAllowsHostname(item, hostname, port) {
			return false
		}
	}
	return true
}

func proxyEntryAllowsHostname(entry string, hostname string, port int) bool {
	match := proxyHostWithPortPattern.FindStringSubmatch(entry)
	proxyHostname := entry
	proxyPort := 0
	if len(match) == 3 {
		proxyHostname = match[1]
		proxyPort, _ = strconv.Atoi(match[2])
	}
	if proxyPort != 0 && proxyPort != port {
		return true
	}
	if !strings.HasPrefix(proxyHostname, ".") && !strings.HasPrefix(proxyHostname, "*") {
		return hostname != proxyHostname
	}
	if strings.HasPrefix(proxyHostname, "*") {
		proxyHostname = strings.TrimPrefix(proxyHostname, "*")
	}
	return !strings.HasSuffix(hostname, proxyHostname)
}
