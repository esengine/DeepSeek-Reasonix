package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/netclient"
	"reasonix/internal/provider"
	"reasonix/internal/serve"
)

func TestRemoteModelOfferCapacityPreservesOwnedRoutes(t *testing.T) {
	p := &credentialProxy{routes: map[string]*credProxyRoute{}}
	scope := credentialProxyScope("host", "workspace")
	upstream := mustParseURL(t, "http://127.0.0.1:8123")
	reserve := func(id string) error {
		_, err := p.resolveAndSetRoute("same-version", "p/m", func() (proxyUpstream, error) {
			return proxyUpstream{url: upstream, scope: scope, offerID: id}, nil
		})
		return err
	}
	for i := range 64 {
		if err := reserve(fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	owned := p.routes["same-version"]
	if err := reserve("overflow"); err == nil || p.routes["same-version"] != owned || len(owned.holds) != 64 {
		t.Fatal("excess offer displaced an existing owner")
	}
	if err := reserve("0"); err != nil {
		t.Fatal("idempotent reservation rejected", err)
	}
	app := &App{credProxy: p}
	app.finishCredentialProxyOffer("host", "workspace", "0")
	if err := reserve("replacement"); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteModelSourceRefreshesAutonomousHTTPRunAndRetiresOldRoute(t *testing.T) {
	isolateDesktopUserDirs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	keys := make(chan string, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		keys <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()
	app := NewApp()
	defer app.closeCredentialProxy()
	view := ProviderView{Name: "source", Kind: "openai", BaseURL: upstream.URL, Models: []string{"m"}, NoProxy: true}
	if _, err := app.SaveProviderWithKey(view, "old-source-key"); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadModelRuntimeSnapshot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	port, err := app.credentialProxyPort()
	if err != nil {
		t.Fatal(err)
	}
	bundle, ref, err := app.buildRemoteModelSettings("source-host", "source-workspace", "source/m", port, cfg)
	if err != nil {
		t.Fatal(err)
	}
	bc := serve.NewBroadcaster()
	opts := boot.Options{Model: ref, ModelSettings: bundle, WorkspaceRoot: t.TempDir(), SessionDir: t.TempDir(), Sink: bc}
	old, err := boot.Build(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	old.EnsureSessionPath()
	srv := serve.New(old, bc, config.ServeConfig{AuthMode: "none"})
	srv.SetControllerBuildOptions(opts)
	defer srv.Close()
	statusRequest := httptest.NewRequest(http.MethodGet, "/model-settings", nil)
	statusRequest.Host = "127.0.0.1"
	statusResponse := httptest.NewRecorder()
	srv.Handler().ServeHTTP(statusResponse, statusRequest)
	var initialOwnership remoteModelSettingsStatus
	if err := json.Unmarshal(statusResponse.Body.Bytes(), &initialOwnership); err != nil || !app.pinCredentialProxyOwnership("source-host", "source-workspace", initialOwnership) {
		t.Fatal("could not establish source ownership", err)
	}
	app.finishCredentialProxyOffer("source-host", "source-workspace", bundle.OfferID)
	if err := old.RunTurn(ctx, "first remote run"); err != nil {
		t.Fatal(err)
	}
	if got := <-keys; got != "Bearer old-source-key" {
		t.Fatal("initial remote route used wrong key")
	}
	if _, err := app.SaveProviderWithKey(view, "new-source-key"); err != nil {
		t.Fatal(err)
	}
	frames, unsubscribe := bc.SubscribeAll()
	defer unsubscribe()
	request := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(`{"input":"autonomous next run"}`)).WithContext(ctx)
	request.Host = "127.0.0.1"
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("autonomous submit: %d %s", response.Code, response.Body)
	}
	select {
	case got := <-keys:
		if got != "Bearer new-source-key" {
			t.Fatal("autonomous remote run retained the old key")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for {
		select {
		case frame := <-frames:
			var message struct {
				Kind string `json:"kind"`
			}
			_ = json.Unmarshal(frame, &message)
			if message.Kind == "turn_done" {
				app.credProxy.mu.Lock()
				retired := app.credProxy.routes[bundle.SourceToken] == nil
				holds := 0
				for _, route := range app.credProxy.routes {
					holds += len(route.holds)
				}
				app.credProxy.mu.Unlock()
				if !retired || holds != 0 {
					t.Fatalf("ownership did not release old route/offer: retired=%v holds=%d", retired, holds)
				}
				return
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestRemoteModelOwnershipRetiresOldRouteAfterInFlightRequest(t *testing.T) {
	isolateDesktopUserDirs(t)
	started, release := make(chan string, 1), make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- r.Header.Get("Authorization")
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, "old request completed")
	}))
	defer upstream.Close()
	app := NewApp()
	defer app.closeCredentialProxy()
	view := ProviderView{Name: "owned", Kind: "openai", BaseURL: upstream.URL, Models: []string{"m"}, NoProxy: true}
	if _, err := app.SaveProviderWithKey(view, "old-owned-key"); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadModelRuntimeSnapshot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old, err := app.applyCredentialProxySnapshot("host", "workspace", "owned/m", cfg, "old")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.SaveProviderWithKey(view, "new-owned-key"); err != nil {
		t.Fatal(err)
	}
	nextCfg, err := config.LoadModelRuntimeSnapshot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	next, err := app.applyCredentialProxySnapshot("host", "workspace", "owned/m", nextCfg, "new")
	if err != nil {
		t.Fatal(err)
	}
	p := app.credProxy
	request := httptest.NewRequest(http.MethodPost, "http://proxy/v1/chat/completions", strings.NewReader(`{"model":"m"}`)).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer "+old.token)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { response := httptest.NewRecorder(); p.ServeHTTP(response, request); done <- response }()
	select {
	case key := <-started:
		if key != "Bearer old-owned-key" {
			t.Fatal("old request switched its credential")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	status := remoteModelSettingsStatus{Version: 1, ModelSettingsOwnership: config.ModelSettingsOwnership{OwnershipIncarnation: "serve", OwnershipSeq: 1}, OwnedRevisions: []string{"old", "new"}}
	app.pinCredentialProxyOwnership("host", "workspace", status)
	app.reconcileCredentialProxyGenerations("host", "workspace", status)
	p.mu.Lock()
	preserved := p.routes[old.token] != nil && !p.routes[old.token].retired
	p.mu.Unlock()
	if !preserved {
		t.Fatal("detached owner lost its route")
	}
	status.OwnershipSeq++
	status.OwnedRevisions = []string{"new"}
	app.reconcileCredentialProxyGenerations("host", "workspace", status)
	p.mu.Lock()
	retained := p.routes[old.token] != nil && p.routes[old.token].retired && p.routes[next.token] != nil
	p.mu.Unlock()
	if !retained {
		t.Fatal("in-flight route removed early or new route retired")
	}
	rejected := httptest.NewRecorder()
	p.ServeHTTP(rejected, request.Clone(ctx))
	if rejected.Code != http.StatusUnauthorized {
		t.Fatal("retired route accepted another request")
	}
	close(release)
	select {
	case response := <-done:
		if response.Code != 200 || response.Body.String() != "old request completed" {
			t.Fatalf("in-flight completion: %d", response.Code)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	p.mu.Lock()
	released := p.routes[old.token] == nil && p.routes[next.token] != nil
	p.mu.Unlock()
	if !released {
		t.Fatal("completed old request retained its retired route")
	}
}

func TestRemoteModelSnapshotPreservesWirePrefixAndKeepsKeysLocal(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	t.Chdir(root)
	for _, kind := range []string{"openai", "anthropic", "responses"} {
		t.Run(kind, func(t *testing.T) {
			requests := make(chan []byte, 8)
			headers := make(chan http.Header, 8)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				requests <- body
				headers <- r.Header.Clone()
				w.Header().Set("Content-Type", "text/event-stream")
				switch kind {
				case "openai":
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				case "anthropic":
					fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"role\":\"assistant\",\"usage\":{\"input_tokens\":1}}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
				case "responses":
					fmt.Fprint(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
				}
			}))
			defer upstream.Close()
			app := NewApp()
			defer app.closeCredentialProxy()
			name, model := "wire-"+kind, "wire-model"
			view := ProviderView{Name: name, Kind: kind, BaseURL: upstream.URL, Models: []string{model}, NoProxy: true, Headers: map[string]string{"X-Test-Private": "test-private-header"}}
			// Generic custom headers are currently implemented by the chat and
			// messages clients; Responses has its own identity-header contract.
			if kind == "responses" {
				view.Headers = nil
			}
			if _, err := app.SaveProviderWithKey(view, "test-real-credential"); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.LoadModelRuntimeSnapshot(root)
			if err != nil {
				t.Fatal(err)
			}
			ref := name + "/" + model
			port, err := app.credentialProxyPort()
			if err != nil {
				t.Fatal(err)
			}
			bundle, remoteRef, err := app.buildRemoteModelSettings("host", "workspace", ref, port, cfg)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := json.Marshal(bundle)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(wire), "test-real-credential") || strings.Contains(string(wire), "test-private-header") {
				t.Fatal("remote bundle exposed a desktop credential")
			}
			remoteCfg := config.Default()
			if err := bundle.Apply(remoteCfg, root); err != nil {
				t.Fatal(err)
			}
			send := func(c *config.Config, ref string) []byte {
				t.Helper()
				p, err := boot.NewLocalProviderResolver(c, netclient.ProxySpec{Mode: netclient.ModeOff}).Resolve(provider.Selection{Ref: ref})
				if err != nil {
					t.Fatal(err)
				}
				stream, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleSystem, Content: "stable prefix\n"}, {Role: provider.RoleUser, Content: "hello"}}, Tools: []provider.ToolSchema{{Name: "example", Description: "stable schema", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)}}})
				if err != nil {
					t.Fatal(err)
				}
				for chunk := range stream {
					if chunk.Err != nil {
						t.Fatal(chunk.Err)
					}
				}
				return <-requests
			}
			before, after := send(cfg, ref), send(remoteCfg, remoteRef)
			var direct, proxied map[string]json.RawMessage
			if err := json.Unmarshal(before, &direct); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(after, &proxied); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"system", "messages", "input", "instructions", "tools"} {
				if string(direct[field]) != string(proxied[field]) {
					t.Fatalf("%s changed through snapshot proxy\ndirect=%s\nproxy=%s", field, direct[field], proxied[field])
				}
			}
			for i := range 2 {
				h := <-headers
				key := h.Get("Authorization")
				if kind == "anthropic" {
					key = h.Get("x-api-key")
				} else {
					key = strings.TrimPrefix(key, "Bearer ")
				}
				if key != "test-real-credential" || h.Get("X-Test-Private") != view.Headers["X-Test-Private"] || h.Get(netclient.ModelProxyOriginalURLHeader) != "" {
					t.Fatalf("upstream auth/headers incorrect for request %d", i)
				}
			}
		})
	}
}

func TestRemoteModelSettingsOldServeDoesNotReceiveMutation(t *testing.T) {
	mutations := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			mutations++
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	if _, err := remoteModelSettingsRequest(context.Background(), server.Client(), server.URL, "session", nil); err == nil || !strings.Contains(err.Error(), "newer remote Serve") {
		t.Fatalf("capability error: %v", err)
	}
	if mutations != 0 {
		t.Fatal("old remote received a mutation")
	}
}
