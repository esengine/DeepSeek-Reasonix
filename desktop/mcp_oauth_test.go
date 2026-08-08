package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/plugin"
)

func TestAuthenticateMCPServerUsesPrivateStateAndReconnects(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	srv := desktopMCPHTTPServer(t)
	defer srv.Close()
	entry := config.PluginEntry{Name: "oauth", Type: "http", URL: srv.URL, Source: config.MCPSourceUserConfig}
	if _, err := config.InstallUserPluginForRoot(dir, entry, true); err != nil {
		t.Fatal(err)
	}

	previousAuthorize, previousOpen := desktopAuthorizeHTTPMCP, desktopOpenMCPAuthorizationURL
	opened := ""
	desktopAuthorizeHTTPMCP = func(_ context.Context, spec plugin.Spec, openURL func(string) error) error {
		if spec.Name != "oauth" || spec.StateDir == "" || strings.HasPrefix(filepath.Clean(spec.StateDir), filepath.Clean(dir)+string(filepath.Separator)) {
			t.Fatalf("OAuth spec must use private Reasonix state: %+v", spec)
		}
		if spec.OAuthHTTPClient == nil {
			t.Fatal("desktop OAuth did not receive the configured proxy-aware HTTP client")
		}
		return openURL("https://auth.example.test/authorize")
	}
	desktopOpenMCPAuthorizationURL = func(_ *App, rawURL string) error { opened = rawURL; return nil }
	t.Cleanup(func() {
		desktopAuthorizeHTTPMCP, desktopOpenMCPAuthorizationURL = previousAuthorize, previousOpen
	})

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()
	if err := app.AuthenticateMCPServer("oauth"); err != nil {
		t.Fatalf("AuthenticateMCPServer: %v", err)
	}
	if opened != "https://auth.example.test/authorize" {
		t.Fatalf("opened URL = %q", opened)
	}
	for _, server := range app.MCPServers() {
		if server.Name == "oauth" {
			if server.Status != "connected" {
				t.Fatalf("server after authorization = %+v", server)
			}
			return
		}
	}
	t.Fatal("authorized server missing from desktop view")
}

func TestAuthenticateMCPServerRejectsIneligibleConfigurations(t *testing.T) {
	tests := []struct {
		name  string
		entry config.PluginEntry
	}{
		{name: "stdio", entry: config.PluginEntry{Name: "server", Type: "stdio", Command: "server-mcp"}},
		{name: "static authentication", entry: config.PluginEntry{
			Name: "server", Type: "http", URL: "https://mcp.example.test/mcp",
			Headers: map[string]string{"Authorization": "Bearer configured"},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateDesktopUserDirs(t)
			dir := robustTempDir(t)
			t.Chdir(dir)
			tc.entry.Source = config.MCPSourceUserConfig
			if _, err := config.InstallUserPluginForRoot(dir, tc.entry, true); err != nil {
				t.Fatal(err)
			}

			called := false
			previous := desktopAuthorizeHTTPMCP
			desktopAuthorizeHTTPMCP = func(context.Context, plugin.Spec, func(string) error) error {
				called = true
				return nil
			}
			t.Cleanup(func() { desktopAuthorizeHTTPMCP = previous })

			app := NewApp()
			app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
			defer app.activeCtrl().Close()
			if err := app.AuthenticateMCPServer("server"); err == nil || !strings.Contains(err.Error(), "Streamable HTTP") {
				t.Fatalf("AuthenticateMCPServer error = %v", err)
			}
			if called {
				t.Fatal("ineligible server reached the OAuth implementation")
			}
		})
	}
}
