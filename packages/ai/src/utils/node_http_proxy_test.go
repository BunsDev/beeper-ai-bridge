package utils

import "testing"

func TestResolveHTTPProxyURLForTargetUsesProtocolProxy(t *testing.T) {
	t.Setenv("https_proxy", "proxy.example:8080")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("all_proxy", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("no_proxy", "")
	t.Setenv("NO_PROXY", "")
	proxyURL, err := ResolveHTTPProxyURLForTarget("https://api.openai.com/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL == nil || proxyURL.String() != "https://proxy.example:8080" {
		t.Fatalf("unexpected proxy URL %v", proxyURL)
	}
}

func TestResolveHTTPProxyURLForTargetHonorsNoProxy(t *testing.T) {
	t.Setenv("https_proxy", "https://proxy.example:8080")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("all_proxy", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("no_proxy", ".openai.com")
	t.Setenv("NO_PROXY", "")
	proxyURL, err := ResolveHTTPProxyURLForTarget("https://api.openai.com/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL != nil {
		t.Fatalf("expected no proxy, got %v", proxyURL)
	}
}

func TestResolveHTTPProxyURLForTargetRejectsUnsupportedProtocol(t *testing.T) {
	t.Setenv("https_proxy", "socks5://proxy.example:1080")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("all_proxy", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("no_proxy", "")
	t.Setenv("NO_PROXY", "")
	_, err := ResolveHTTPProxyURLForTarget("https://api.openai.com/v1/responses")
	if err == nil {
		t.Fatal("expected unsupported proxy protocol error")
	}
}
