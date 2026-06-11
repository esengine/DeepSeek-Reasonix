package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestNormalizeBotInstallTarget(t *testing.T) {
	cases := []struct {
		provider     string
		domain       string
		wantProvider string
		wantDomain   string
	}{
		{provider: "lark", wantProvider: "feishu", wantDomain: "lark"},
		{provider: "feishu", domain: "lark", wantProvider: "feishu", wantDomain: "lark"},
		{provider: "wechat", wantProvider: "weixin", wantDomain: "weixin"},
		{provider: "weixin", domain: "anything", wantProvider: "weixin", wantDomain: "weixin"},
		{provider: "unknown", domain: "unknown", wantProvider: "feishu", wantDomain: "feishu"},
	}
	for _, tc := range cases {
		gotProvider, gotDomain := normalizeBotInstallTarget(tc.provider, tc.domain)
		if gotProvider != tc.wantProvider || gotDomain != tc.wantDomain {
			t.Fatalf("normalizeBotInstallTarget(%q,%q) = %q,%q; want %q,%q", tc.provider, tc.domain, gotProvider, gotDomain, tc.wantProvider, tc.wantDomain)
		}
	}
}

func TestFeishuInstallUsesReturnedTenantBrandAndStoresSecret(t *testing.T) {
	isolateDesktopUserDirs(t)
	withRewrittenHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/v1/app/registration" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		switch r.Form.Get("action") {
		case "init":
			writeJSON(t, w, map[string]any{"ok": true})
		case "begin":
			if r.Form.Get("archetype") != "PersonalAgent" || r.Form.Get("auth_method") != "client_secret" {
				http.Error(w, "wrong begin form", http.StatusBadRequest)
				return
			}
			writeJSON(t, w, map[string]any{
				"device_code":               "dev-feishu",
				"verification_uri_complete": "https://accounts.example/verify",
				"user_code":                 "CODE",
				"interval":                  3,
				"expire_in":                 300,
			})
		case "poll":
			if r.Form.Get("device_code") != "dev-feishu" {
				http.Error(w, "wrong device code", http.StatusBadRequest)
				return
			}
			writeJSON(t, w, map[string]any{
				"client_id":     "cli-1",
				"client_secret": "secret-1",
				"user_info":     map[string]any{"tenant_brand": "lark"},
			})
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
		}
	}))

	app := NewApp()
	start, err := app.StartBotConnectionInstall("feishu", "feishu")
	if err != nil {
		t.Fatalf("StartBotConnectionInstall: %v", err)
	}
	if !start.OK || start.InstallID == "" || start.URL == "" || start.DeviceCode != "dev-feishu" {
		t.Fatalf("start result = %+v, want ok lark-capable QR result", start)
	}

	poll, err := app.PollBotConnectionInstall(start.InstallID)
	if err != nil {
		t.Fatalf("PollBotConnectionInstall: %v", err)
	}
	if !poll.Done {
		t.Fatalf("poll result = %+v, want done", poll)
	}
	if poll.Connection.Provider != "feishu" || poll.Connection.Domain != "lark" || poll.Connection.ID != "feishu-lark" {
		t.Fatalf("connection = %+v, want feishu-lark from tenant_brand", poll.Connection)
	}
	if poll.Connection.WorkspaceRoot != "" {
		t.Fatalf("connection workspaceRoot = %q, want empty global default", poll.Connection.WorkspaceRoot)
	}
	if poll.Connection.Credential.AppID != "cli-1" || poll.Connection.Credential.AppSecretEnv != "LARK_BOT_APP_SECRET" || !poll.Connection.Credential.SecretSet {
		t.Fatalf("credential = %+v, want stored Lark secret", poll.Connection.Credential)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if !cfg.Bot.Enabled || !cfg.Bot.Feishu.Enabled || cfg.Bot.Feishu.Domain != "lark" || cfg.Bot.Feishu.Mode != "websocket" || !cfg.Bot.Feishu.RequireMention {
		t.Fatalf("saved feishu config = %+v, want enabled websocket lark with mention gating", cfg.Bot.Feishu)
	}
}

func TestWeixinInstallStoresSavedAccountAndConnection(t *testing.T) {
	isolateDesktopUserDirs(t)
	withRewrittenHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ilink/bot/get_bot_qrcode":
			if r.URL.Query().Get("bot_type") != "3" {
				http.Error(w, "missing bot type", http.StatusBadRequest)
				return
			}
			writeJSON(t, w, map[string]any{
				"qrcode":             "qr-weixin",
				"qrcode_img_content": "data:image/png;base64,abc",
			})
		case "/ilink/bot/get_qrcode_status":
			if r.URL.Query().Get("qrcode") != "qr-weixin" {
				http.Error(w, "wrong qr", http.StatusBadRequest)
				return
			}
			writeJSON(t, w, map[string]any{
				"status":        "confirmed",
				"ilink_bot_id":  "weixin-account",
				"bot_token":     "token-1",
				"ilink_user_id": "user-1",
				"baseurl":       "https://ilinkai.weixin.qq.com",
			})
		default:
			http.NotFound(w, r)
		}
	}))

	app := NewApp()
	start, err := app.StartBotConnectionInstall("weixin", "")
	if err != nil {
		t.Fatalf("StartBotConnectionInstall: %v", err)
	}
	if !start.OK || start.Provider != "weixin" || start.Domain != "weixin" || start.URL != "data:image/png;base64,abc" || start.DeviceCode != "qr-weixin" {
		t.Fatalf("start result = %+v, want weixin QR result", start)
	}

	poll, err := app.PollBotConnectionInstall(start.InstallID)
	if err != nil {
		t.Fatalf("PollBotConnectionInstall: %v", err)
	}
	if !poll.Done {
		t.Fatalf("poll result = %+v, want done", poll)
	}
	if poll.Connection.Provider != "weixin" || poll.Connection.Domain != "weixin" || poll.Connection.Credential.AccountID != "weixin-account" {
		t.Fatalf("connection = %+v, want weixin account connection", poll.Connection)
	}
	if poll.Connection.WorkspaceRoot != "" {
		t.Fatalf("connection workspaceRoot = %q, want empty global default", poll.Connection.WorkspaceRoot)
	}
	if poll.Connection.Credential.TokenEnv != "WEIXIN_BOT_TOKEN" || !poll.Connection.Credential.SecretSet {
		t.Fatalf("credential = %+v, want saved account to count as configured token", poll.Connection.Credential)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if !cfg.Bot.Enabled || !cfg.Bot.Weixin.Enabled || cfg.Bot.Weixin.AccountID != "weixin-account" || cfg.Bot.Weixin.TokenEnv != "WEIXIN_BOT_TOKEN" {
		t.Fatalf("saved weixin config = %+v, want enabled saved account", cfg.Bot.Weixin)
	}
}

func TestRememberBotConnectionRemoteStoresStableScope(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	if _, err := app.upsertBotConnection(config.BotConnectionConfig{
		ID:       "feishu-lark",
		Provider: "feishu",
		Domain:   "lark",
		Label:    "kun",
		Enabled:  true,
		Status:   "connected",
	}, nil); err != nil {
		t.Fatalf("upsert global connection: %v", err)
	}
	if err := app.rememberBotConnectionRemote("feishu-lark", "ou_global"); err != nil {
		t.Fatalf("remember global remote: %v", err)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if got := cfg.Bot.Connections[0].SessionMappings[0]; got.Scope != "global" || got.WorkspaceRoot != "" || got.RemoteID != "ou_global" {
		t.Fatalf("global mapping = %+v, want scope=global without workspace", got)
	}

	if _, err := app.upsertBotConnection(config.BotConnectionConfig{
		ID:            "weixin-project",
		Provider:      "weixin",
		Domain:        "weixin",
		Label:         "project",
		Enabled:       true,
		Status:        "connected",
		WorkspaceRoot: "/tmp/reasonix-project",
	}, nil); err != nil {
		t.Fatalf("upsert project connection: %v", err)
	}
	if err := app.rememberBotConnectionRemote("weixin-project", "wxid_project"); err != nil {
		t.Fatalf("remember project remote: %v", err)
	}
	cfg = config.LoadForEdit(config.UserConfigPath())
	var projectMapping config.BotConnectionSessionMapping
	for _, conn := range cfg.Bot.Connections {
		if conn.ID == "weixin-project" && len(conn.SessionMappings) == 1 {
			projectMapping = conn.SessionMappings[0]
		}
	}
	if projectMapping.Scope != "project" || projectMapping.WorkspaceRoot != "/tmp/reasonix-project" || projectMapping.RemoteID != "wxid_project" {
		t.Fatalf("project mapping = %+v, want project scope and workspace", projectMapping)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("write json: %v", err)
	}
}

func withRewrittenHTTP(t *testing.T, handler http.Handler) {
	t.Helper()
	server := httptest.NewServer(handler)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	previous := http.DefaultTransport
	http.DefaultTransport = rewriteHTTPTransport{target: target, next: previous}
	t.Cleanup(func() {
		http.DefaultTransport = previous
		server.Close()
	})
}

type rewriteHTTPTransport struct {
	target *url.URL
	next   http.RoundTripper
}

func (r rewriteHTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = r.target.Scheme
	clone.URL.Host = r.target.Host
	clone.Host = r.target.Host
	if r.next == nil {
		r.next = http.DefaultTransport
	}
	clone.URL.Path = "/" + strings.TrimLeft(clone.URL.Path, "/")
	return r.next.RoundTrip(clone)
}
