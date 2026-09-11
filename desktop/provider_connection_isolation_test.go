package main

import (
	"reasonix/internal/config"
	"testing"
)

func TestIndependentConnectionCredentials(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := &App{}
	if _, err := app.AddProviderConnection("", "deepseek-flash", "personal-test-key"); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := app.loadDesktopUserConfigForView()
	if err != nil {
		t.Fatal(err)
	}
	var first string
	for _, p := range cfg.Providers {
		if p.Name != "deepseek-flash" && p.APIKey() == "personal-test-key" {
			first = p.Name
			break
		}
	}
	if first == "" {
		t.Fatal("new connection not found")
	}
	if _, err := app.AddProviderConnection("", first, ""); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = app.loadDesktopUserConfigForView()
	if err != nil {
		t.Fatal(err)
	}
	var second string
	for _, p := range cfg.Providers {
		if p.Name != first && len(p.Name) > len(first) && p.Name[:len(first)] == first {
			second = p.Name
			if p.APIKey() != "" {
				t.Fatal("copy inherited secret")
			}
			break
		}
	}
	if second == "" {
		t.Fatal("copy missing")
	}
	if _, err := app.SetConnectionKey(second, "work-test-key"); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = app.loadDesktopUserConfigForView()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cfg.Providers {
		if p.Name == first && p.APIKey() != "personal-test-key" {
			t.Fatal("first key changed")
		}
		if p.Name == second && p.APIKey() != "work-test-key" {
			t.Fatal("second key missing")
		}
	}
	if _, err := app.SetConnectionKey(second, ""); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = app.loadDesktopUserConfigForView()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cfg.Providers {
		if p.Name == first && p.APIKey() != "personal-test-key" {
			t.Fatal("clear changed sibling")
		}
		if p.Name == second && p.APIKey() != "" {
			t.Fatal("clear failed")
		}
	}
}

func TestLegacyConnectionKeyDetachesSharedSecret(t *testing.T) {
	isolateDesktopUserDirs(t)
	if err := upsertDotEnv("DEEPSEEK_API_KEY", "legacy-test-key"); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	if _, err := app.SetConnectionKey("deepseek-flash", "private-test-key"); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := app.loadDesktopUserConfigForView()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cfg.Providers {
		if p.Name == "deepseek-flash" && (p.APIKeyEnv == "DEEPSEEK_API_KEY" || p.APIKey() != "private-test-key") {
			t.Fatal("legacy connection was not detached")
		}
	}
	if _, err := app.AddProviderConnection("", "deepseek-pro", ""); err != nil {
		t.Fatal(err)
	}
	for _, p := range config.Default().Providers {
		if p.Name == "deepseek-pro" && p.APIKey() != "legacy-test-key" {
			t.Fatal("shared sibling credential changed")
		}
	}

}

func TestNewConnectionURLDoesNotChangeTemplate(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := &App{}
	if _, err := a.AddProviderConnectionWithURL("", "deepseek-flash", "", "https://gateway.example/v1"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddProviderConnection("", "deepseek-flash", ""); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil {
		t.Fatal(err)
	}
	custom, defaults := 0, 0
	for _, p := range cfg.Providers {
		if p.Name == "deepseek-flash" {
			continue
		}
		if p.BaseURL == "https://gateway.example/v1" {
			custom++
		} else if p.BaseURL == config.Default().Providers[0].BaseURL {
			defaults++
		}
	}
	if custom != 1 || defaults == 0 {
		t.Fatalf("custom=%d defaults=%d", custom, defaults)
	}
	if _, err := a.AddProviderConnectionWithURL("", "deepseek-flash", "", "file:///tmp/key"); err == nil {
		t.Fatal("invalid URL accepted")
	}
}

func TestNewConnectionProtocolOverride(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := &App{}
	for _, kind := range []string{"openai", "responses", "anthropic"} {
		if _, err := a.AddProviderConnectionWithOptions("glm-cn", "", "", "https://gateway.example/v1", kind); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.AddProviderConnection("glm-cn", "", ""); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	defaults := false
	for _, p := range cfg.Providers {
		if p.BaseURL == "https://gateway.example/v1" {
			kinds[p.Kind] = true
			if p.RequestURL != "" || p.ChatURL != "" || p.ModelsURL != "" {
				t.Fatal("stale endpoint")
			}
		}
		if p.BaseURL == "https://open.bigmodel.cn/api/paas/v4" && p.Kind == "openai" {
			defaults = true
		}
	}
	if len(kinds) != 3 || !defaults {
		t.Fatalf("protocols=%v defaults=%v", kinds, defaults)
	}
	if _, err := a.AddProviderConnectionWithOptions("glm-cn", "", "", "", "invalid"); err == nil {
		t.Fatal("invalid protocol accepted")
	}
}

func TestDocumentedProtocolConnectionOptions(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := &App{}
	for _, tc := range []struct{ id, kind, url string }{
		{"qiniu", "anthropic", "https://api.qnaigc.com"},
		{"mimo-api", "responses", "https://api.xiaomimimo.com/v1"},
	} {
		if _, err := a.AddProviderConnectionWithOptions(tc.id, "", "", tc.url, tc.kind); err != nil {
			t.Fatal(err)
		}
	}
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil {
		t.Fatal(err)
	}
	bearer, stateless := false, false
	for _, p := range cfg.Providers {
		if p.Kind == "anthropic" && p.BaseURL == "https://api.qnaigc.com" {
			bearer = p.AuthHeader
		}
		if p.Kind == "responses" && p.BaseURL == "https://api.xiaomimimo.com/v1" {
			stateless = p.ResponsesMode == "stateless"
		}
	}
	if !bearer || !stateless {
		t.Fatalf("bearer=%v stateless=%v", bearer, stateless)
	}
}
