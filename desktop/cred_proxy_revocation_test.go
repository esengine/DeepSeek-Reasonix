package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/config"
)

func TestClearProviderKeyRevokesCredentialProxyRoute(t *testing.T) {
	isolateDesktopUserDirs(t)
	const keyEnv = "TEST_PROXY_CLEAR_KEY"
	setDesktopTestCredential(t, keyEnv, "sk-before-clear")
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	firstRef, _ := configureCredentialProxyTestModels(t, upstream.URL, keyEnv)
	a := &App{}
	t.Cleanup(a.closeCredentialProxy)
	route, err := a.applyCredentialProxyModel("box", "~/app", firstRef)
	if err != nil {
		t.Fatal(err)
	}
	if status := credentialProxyStatus(t, route.port, route.token); status != http.StatusOK {
		t.Fatalf("initial route status = %d, want 200", status)
	}
	if err := a.ClearProviderKey(keyEnv); err != nil {
		t.Fatal(err)
	}
	if status := credentialProxyStatus(t, route.port, route.token); status != http.StatusUnauthorized {
		t.Fatalf("cleared route status = %d, want 401", status)
	}
	if got := upstreamCalls.Load(); got != 1 {
		t.Fatalf("revoked token reached upstream: calls=%d, want 1", got)
	}
}

func TestDeleteProviderRevokesOnlyItsCredentialProxyRoutes(t *testing.T) {
	isolateDesktopUserDirs(t)
	const keyEnv = "TEST_PROXY_PROVIDER_DELETE_KEY"
	setDesktopTestCredential(t, keyEnv, "sk-shared")
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	firstRef, secondRef := configureCredentialProxyTestModels(t, upstream.URL, keyEnv)
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Desktop.ProviderAccess = []string{"proxy-first", "proxy-second"}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	t.Cleanup(a.closeCredentialProxy)
	first, err := a.applyCredentialProxyModel("box", "~/app", firstRef)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.applyCredentialProxyModel("box", "~/app", secondRef)
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []credentialProxyRouteInfo{first, second} {
		if status := credentialProxyStatus(t, route.port, route.token); status != http.StatusOK {
			t.Fatalf("initial route status = %d, want 200", status)
		}
	}
	if err := a.DeleteProvider("proxy-first"); err != nil {
		t.Fatal(err)
	}
	if status := credentialProxyStatus(t, first.port, first.token); status != http.StatusUnauthorized {
		t.Fatalf("deleted provider route status = %d, want 401", status)
	}
	if status := credentialProxyStatus(t, second.port, second.token); status != http.StatusOK {
		t.Fatalf("remaining provider route status = %d, want 200", status)
	}
	if got := upstreamCalls.Load(); got != 3 {
		t.Fatalf("upstream calls = %d, want 3", got)
	}
}

func TestCredentialProxyRevocationSerializesWithRoutePublish(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	parsed := mustParseURL(t, upstream.URL)
	p := &credentialProxy{routes: map[string]*credProxyRoute{}}
	resolving := make(chan struct{})
	release := make(chan struct{})
	published := make(chan error, 1)
	go func() {
		_, err := p.resolveAndSetRoute("virtual-tok", "provider/model", func() (proxyUpstream, error) {
			close(resolving)
			<-release
			return proxyUpstream{
				url: parsed, apiKey: "sk-old", model: "model",
				kind: "openai", apiKeyEnv: "TEST_KEY", provider: "provider",
			}, nil
		})
		published <- err
	}()
	<-resolving
	revoked := make(chan struct{})
	go func() {
		p.revokeRoutes(func(route *credProxyRoute) bool { return route.apiKeyEnv == "TEST_KEY" })
		close(revoked)
	}()
	close(release)
	if err := <-published; err != nil {
		t.Fatal(err)
	}
	<-revoked

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/v1/chat/completions", strings.NewReader(`{"model":"model"}`))
	req.Header.Set("Authorization", "Bearer virtual-tok")
	recorder := httptest.NewRecorder()
	p.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func credentialProxyStatus(t *testing.T, port int, token string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", port), strings.NewReader(`{"model":"placeholder"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}
