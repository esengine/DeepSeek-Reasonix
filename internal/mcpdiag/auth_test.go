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

func TestDiagnoseAuthPossibleForDeferredRemoteWithoutAuthConfig(t *testing.T) {
	got := DiagnoseAuth("sse", "deferred", "", "https://mcp.example.com/sse", false)
	if got.Status != AuthPossible {
		t.Fatalf("status = %q, want %q", got.Status, AuthPossible)
	}
	if got.URL == "" {
		t.Fatal("possible remote auth should keep the server URL")
	}
}

func TestDiagnoseAuthSkipsRemoteWithStaticAuth(t *testing.T) {
	got := DiagnoseAuth("http", "deferred", "", "https://mcp.example.com/mcp", true)
	if got.Status != AuthNone {
		t.Fatalf("status = %q, want %q", got.Status, AuthNone)
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
}
