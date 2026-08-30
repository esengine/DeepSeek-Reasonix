package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/plugin"
)

func TestMCPAppBindingSurvivesActiveTabSwitchAndClosesOriginalHost(t *testing.T) {
	app := NewApp()
	hostA := plugin.NewHostWithProfile(plugin.HostProfileDesktopApps)
	hostB := plugin.NewHostWithProfile(plugin.HostProfileDesktopApps)
	ctrlA := control.New(control.Options{Host: hostA})
	ctrlB := control.New(control.Options{Host: hostB})
	app.mu.Lock()
	app.tabs = map[string]*WorkspaceTab{
		"tab-a": {ID: "tab-a", Ctrl: ctrlA},
		"tab-b": {ID: "tab-b", Ctrl: ctrlB},
	}
	app.tabOrder = []string{"tab-a", "tab-b"}
	app.activeTabID = "tab-b"
	app.mu.Unlock()

	inst := hostA.RegisterAppInstance("srv", "tool", 3, "call", "ui://x/a.html")
	if !hostA.BindAppResource(inst.Token, "<html>a</html>", "text/html", "digest-a", nil) {
		t.Fatal("bind resource")
	}
	app.mcpAppsSandbox.bind(inst.Token, mcpAppBinding{
		tabID: "tab-a", server: "srv", host: hostA, ctrl: ctrlA,
	})

	if _, err := app.MCPAppResourceDigestForTab("tab-b", inst.Token); err == nil {
		t.Fatal("cross-tab digest lookup was accepted")
	}
	if digest, err := app.MCPAppResourceDigestForTab("tab-a", inst.Token); err != nil || digest != "digest-a" {
		t.Fatalf("digest = %q, err = %v", digest, err)
	}
	if err := app.MCPCloseAppInstanceForTab("tab-b", inst.Token); err == nil {
		t.Fatal("cross-tab close was accepted")
	}
	if _, ok := hostA.LookupAppInstance(inst.Token); !ok {
		t.Fatal("wrong-tab close released original instance")
	}
	if err := app.MCPCloseAppInstanceForTab("tab-a", inst.Token); err != nil {
		t.Fatal(err)
	}
	if _, ok := hostA.LookupAppInstance(inst.Token); ok {
		t.Fatal("original host instance was not released")
	}
}

func TestMCPAppSandboxRelayIsBidirectionalAndEmbeddable(t *testing.T) {
	origin := &mcpAppOrigin{nonce: "test-nonce"}
	req := httptest.NewRequest(http.MethodGet, "/sandbox?nonce=test-nonce", nil)
	rec := httptest.NewRecorder()
	origin.serveRelayPage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "" {
		t.Fatalf("outer relay cannot be embedded: X-Frame-Options=%q", got)
	}
	body := rec.Body.String()
	for _, required := range []string{
		"event.source === window.parent",
		"inner.contentWindow.postMessage(event.data",
		"event.source !== inner.contentWindow",
		"window.parent.postMessage(event.data",
		"new TextEncoder().encode(text).byteLength",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("relay missing %q", required)
		}
	}
}

func TestMCPAppSandboxServesOnlyTheBoundDigest(t *testing.T) {
	app := NewApp()
	host := plugin.NewHostWithProfile(plugin.HostProfileDesktopApps)
	inst := host.RegisterAppInstance("srv", "tool", 3, "call", "ui://x/a.html")
	if !host.BindAppResource(inst.Token, "<html>frozen</html>", "text/html", "digest-a", map[string][]string{
		"connectDomains":  {"https://api.example.test", "https://*.blocked.test"},
		"resourceDomains": {"https://cdn.example.test"},
	}) {
		t.Fatal("bind resource")
	}
	app.mcpAppsSandbox.bind(inst.Token, mcpAppBinding{tabID: "tab-a", server: "srv", host: host})
	handler := (&mcpAppOrigin{server: "srv"}).serveResource(app)

	bad := httptest.NewRecorder()
	handler(bad, httptest.NewRequest(http.MethodGet, "/resource?token="+inst.Token+"&digest=other", nil))
	if bad.Code != http.StatusBadGateway {
		t.Fatalf("wrong digest status = %d", bad.Code)
	}

	good := httptest.NewRecorder()
	handler(good, httptest.NewRequest(http.MethodGet, "/resource?token="+inst.Token+"&digest=digest-a", nil))
	if good.Code != http.StatusOK || good.Body.String() != "<html>frozen</html>" {
		t.Fatalf("bound resource status/body = %d %q", good.Code, good.Body.String())
	}
	if got := good.Header().Get("X-App-Sha256"); got != "digest-a" {
		t.Fatalf("digest header = %q", got)
	}
	csp := good.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "connect-src https://api.example.test") || strings.Contains(csp, "blocked.test") {
		t.Fatalf("CSP = %q", csp)
	}
	if !strings.Contains(csp, "script-src 'unsafe-inline' https://cdn.example.test") {
		t.Fatalf("resourceDomains not mapped into CSP: %q", csp)
	}
}

func TestValidatedMCPAppLinkRejectsUnsafeTargets(t *testing.T) {
	for _, raw := range []string{
		"javascript:alert(1)",
		"file:///tmp/secret",
		"https://user:pass@example.test/private",
		"//example.test/no-scheme",
	} {
		if _, err := validatedAppLink(raw); err == nil {
			t.Fatalf("unsafe URL accepted: %q", raw)
		}
	}
	if got, err := validatedAppLink("https://example.test/path?q=1"); err != nil || got.Host != "example.test" {
		t.Fatalf("safe URL rejected: %v, %v", got, err)
	}
}
