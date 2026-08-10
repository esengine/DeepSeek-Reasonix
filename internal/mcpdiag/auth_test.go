package mcpdiag

import "testing"

func TestDiagnoseAuthRequiredFromFailure(t *testing.T) {
	got := DiagnoseAuth("http", "failed", "connect: 401 unauthorized", "https://mcp.example.com/mcp", false)
	if got.Status != AuthRequired {
		t.Fatalf("status = %q, want %q", got.Status, AuthRequired)
	}
	if got.URL != "https://mcp.example.com/mcp" {
		t.Fatalf("url = %q", got.URL)
	}
}

func TestDiagnoseAuthPossibleForDeferredHTTPWithoutAuthConfig(t *testing.T) {
	got := DiagnoseAuth("streamable-http", "deferred", "", "https://mcp.example.com/mcp", false)
	if got.Status != AuthPossible {
		t.Fatalf("status = %q, want %q", got.Status, AuthPossible)
	}
	if got.URL == "" {
		t.Fatal("possible remote auth should keep the server URL")
	}
}

func TestDiagnoseAuthRejectsIneligibleNativeOAuth(t *testing.T) {
	for _, tc := range []struct {
		name           string
		transport      string
		url            string
		authConfigured bool
	}{
		{name: "stdio", transport: "stdio"},
		{name: "legacy sse", transport: "sse", url: "https://mcp.example.com/sse"},
		{name: "static auth", transport: "http", url: "https://mcp.example.com/mcp", authConfigured: true},
		{name: "invalid url", transport: "http", url: "not-a-url"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DiagnoseAuth(tc.transport, "failed", "authentication required", tc.url, tc.authConfigured)
			if got.Status != AuthNone || got.URL != "" {
				t.Fatalf("diagnosis = %+v, want no native OAuth action", got)
			}
		})
	}
}

func TestHasAuthConfig(t *testing.T) {
	if !HasAuthConfig(map[string]string{"Authorization": "Bearer ${TOKEN}"}, nil, "") {
		t.Fatal("authorization header should count as auth config")
	}
	if !HasAuthConfig(nil, map[string]string{"DIDA_TOKEN": "${DIDA_TOKEN}"}, "") {
		t.Fatal("auth-like env key should count as auth config")
	}
	if HasAuthConfig(nil, map[string]string{"DEBUG": "1"}, "https://mcp.example.com/mcp") {
		t.Fatal("unrelated env should not count as auth config")
	}
	for _, tc := range []struct {
		name    string
		headers map[string]string
		url     string
	}{
		{name: "url userinfo", url: "https://user:pass@mcp.example.com/mcp"},
		{name: "signed query", url: "https://mcp.example.com/mcp?sig=abc"},
		{name: "api key query", url: "https://mcp.example.com/mcp?key=abc"},
		{name: "subscription header", headers: map[string]string{"X-Subscription-Key": "abc"}, url: "https://mcp.example.com/mcp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !HasAuthConfig(tc.headers, nil, tc.url) {
				t.Fatalf("HasAuthConfig(%v, %q) = false, want true", tc.headers, tc.url)
			}
		})
	}
}

func TestCanUseHTTPMCPOAuthOnlyAllowsSecureOrLoopbackHTTP(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
		want bool
	}{
		{name: "https", url: "https://mcp.example.com/mcp", want: true},
		{name: "localhost", url: "http://localhost:8787/mcp", want: true},
		{name: "ipv4 loopback", url: "http://127.0.0.1:8787/mcp", want: true},
		{name: "ipv6 loopback", url: "http://[::1]:8787/mcp", want: true},
		{name: "remote http", url: "http://10.0.0.8/mcp", want: false},
		{name: "userinfo", url: "https://user:pass@mcp.example.com/mcp", want: false},
		{name: "signed query", url: "https://mcp.example.com/mcp?sig=abc", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanUseHTTPMCPOAuth("http", tc.url, false); got != tc.want {
				t.Fatalf("CanUseHTTPMCPOAuth(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

func TestClearAuthConfigRemovesOnlyAuthMaterial(t *testing.T) {
	headers, env, rawURL, changed := ClearAuthConfig(
		map[string]string{
			"Authorization": "Bearer ${TOKEN}",
			"X-Org":         "team",
		},
		map[string]string{
			"DIDA_TOKEN": "${DIDA_TOKEN}",
			"DEBUG":      "1",
		},
		"https://mcp.example.com/mcp?access_token=abc&workspace=main",
	)
	if !changed {
		t.Fatal("ClearAuthConfig should report changed")
	}
	if _, ok := headers["Authorization"]; ok {
		t.Fatalf("auth header should be removed: %v", headers)
	}
	if headers["X-Org"] != "team" {
		t.Fatalf("ordinary header should be preserved: %v", headers)
	}
	if _, ok := env["DIDA_TOKEN"]; ok {
		t.Fatalf("auth env should be removed: %v", env)
	}
	if env["DEBUG"] != "1" {
		t.Fatalf("ordinary env should be preserved: %v", env)
	}
	if rawURL != "https://mcp.example.com/mcp?workspace=main" {
		t.Fatalf("url = %q", rawURL)
	}
	_, _, rawURL, changed = ClearAuthConfig(nil, nil, "https://user:pass@mcp.example.com/mcp")
	if !changed || rawURL != "https://mcp.example.com/mcp" {
		t.Fatalf("userinfo URL clear = (%q, %v), want credential-free URL", rawURL, changed)
	}
}
