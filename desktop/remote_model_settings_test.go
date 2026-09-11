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

// A Serve older than the model-settings protocol answers unknown GET paths
// through its catch-all "GET /" route with status 200 and the HTML index, so
// the status probe must classify that document as a capability rejection
// instead of surfacing a JSON decode error (issue #9996).
func TestRemoteModelSettingsLegacyServeHTMLIndexIsCapabilityRejection(t *testing.T) {
	mutations := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<!DOCTYPE html><html><body>Reasonix</body></html>"))
			return
		}
		mutations++
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}))
	defer server.Close()
	for _, body := range []any{nil, map[string]any{"version": 1, "ref": "p/m", "settings": map[string]any{"revision": "r"}}} {
		_, err := remoteModelSettingsRequest(context.Background(), server.Client(), server.URL, "session", body)
		if err == nil || !strings.Contains(err.Error(), "newer remote Serve") {
			t.Fatalf("capability error: %v", err)
		}
		if !isRemoteModelSettingsUnsupported(err) {
			t.Fatalf("HTML index response was not classified as unsupported: %v", err)
		}
		if strings.Contains(err.Error(), "invalid character") {
			t.Fatalf("raw JSON decode error escaped the capability probe: %v", err)
		}
	}
	if mutations != 1 {
		t.Fatalf("legacy serve received %d mutations", mutations)
	}
}

// Only document-shaped bodies map to the legacy-Serve rejection; a corrupt or
// truncated status payload from a capable Serve stays a decode error.
func TestRemoteModelSettingsNonDocumentDecodeFailureStaysError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not-json{"))
	}))
	defer server.Close()
	_, err := remoteModelSettingsRequest(context.Background(), server.Client(), server.URL, "session", nil)
	if err == nil || !strings.Contains(err.Error(), "decode remote model settings status") {
		t.Fatalf("expected decode error, got %v", err)
	}
	if isRemoteModelSettingsUnsupported(err) {
		t.Fatal("non-document payload was misclassified as an unsupported Serve")
	}
}

type unsupportedModelSettingsKernel struct {
	remoteKernel
	switches int
}

func (k *unsupportedModelSettingsKernel) SwitchCredentialProxyModel(context.Context, string, string, string, string, string) error {
	k.switches++
	return &remoteModelSettingsRejection{message: remoteModelSettingsUpgradeHint, unsupported: true}
}

// Turn admission must not fail every send against a reused legacy Serve that
// credential mode itself still supports: the unsupported protocol is recorded
// per tab generation and the run is admitted without a revision, until a
// reconnect or serve replacement probes the protocol again.
func TestEnsureRemoteModelSettingsAdmitsLegacyServeTurns(t *testing.T) {
	isolateDesktopUserDirs(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{}"))
	}))
	defer upstream.Close()
	app := NewApp()
	defer app.closeCredentialProxy()
	view := ProviderView{Name: "legacy", Kind: "openai", BaseURL: upstream.URL, Models: []string{"m"}, NoProxy: true}
	if _, err := app.SaveProviderWithKey(view, "legacy-key"); err != nil {
		t.Fatal(err)
	}
	if err := editUserConfig(func(c *config.Config) error {
		return c.UpsertRemoteHost(config.RemoteHostEntry{Name: "legacy-host", Host: "127.0.0.1", CredentialMode: "local-proxy"})
	}); err != nil {
		t.Fatal(err)
	}
	tab := &remoteTab{
		id: "legacy-tab", ref: RemoteTabRef{HostID: "legacy-host", Workspace: "ws"},
		state: "ready", client: &http.Client{}, model: "legacy/m", gen: 1,
		routing: remoteTabSessionRouting{currentPath: "/proj/session"},
	}
	app.remoteTabs = map[string]*remoteTab{tab.id: tab}
	kernel := &unsupportedModelSettingsKernel{}
	app.remoteMu.Lock()
	app.remoteRuntime = kernel
	app.remoteMu.Unlock()

	revision, admittedGen, err := app.ensureRemoteModelSettings(tab.id)
	if err != nil || revision != "" || admittedGen != 1 {
		t.Fatalf("legacy Serve turn admission failed: revision=%q gen=%d err=%v", revision, admittedGen, err)
	}
	if kernel.switches != 1 {
		t.Fatalf("expected one protocol probe, got %d", kernel.switches)
	}
	app.remoteTabMu.Lock()
	recorded, failed := tab.settings.unsupportedGen == 1, tab.settings.failure
	app.remoteTabMu.Unlock()
	if !recorded || failed != "" {
		t.Fatalf("unsupported generation not recorded cleanly: gen=%d failure=%q", tab.settings.unsupportedGen, failed)
	}

	// The remembered verdict admits later turns without re-probing the Serve.
	if revision, admittedGen, err = app.ensureRemoteModelSettings(tab.id); err != nil || revision != "" || admittedGen != 1 || kernel.switches != 1 {
		t.Fatalf("repeat admission re-probed legacy Serve: revision=%q gen=%d err=%v switches=%d", revision, admittedGen, err, kernel.switches)
	}

	// A new tab generation (reconnect or replaced Serve) probes once more.
	app.remoteTabMu.Lock()
	tab.gen = 2
	app.remoteTabMu.Unlock()
	if revision, admittedGen, err = app.ensureRemoteModelSettings(tab.id); err != nil || revision != "" || admittedGen != 2 || kernel.switches != 2 {
		t.Fatalf("new generation did not re-probe the protocol: revision=%q gen=%d err=%v switches=%d", revision, admittedGen, err, kernel.switches)
	}
}

type reconnectingUnsupportedKernel struct {
	remoteKernel
	switches  int
	reconnect func()
}

func (k *reconnectingUnsupportedKernel) SwitchCredentialProxyModel(context.Context, string, string, string, string, string) error {
	k.switches++
	if k.reconnect != nil {
		reconnect := k.reconnect
		k.reconnect = nil
		reconnect()
	}
	return &remoteModelSettingsRejection{message: remoteModelSettingsUpgradeHint, unsupported: true}
}

// A reconnect that replaces the probe target mid-flight must not admit against
// the retired fence: the replacement Serve may speak the protocol, so the
// unsupported verdict is only remembered when the probed connection is still
// current, and a replaced generation is re-probed instead.
func TestEnsureRemoteModelSettingsReprobesReplacedConnection(t *testing.T) {
	isolateDesktopUserDirs(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{}"))
	}))
	defer upstream.Close()
	app := NewApp()
	defer app.closeCredentialProxy()
	view := ProviderView{Name: "legacy", Kind: "openai", BaseURL: upstream.URL, Models: []string{"m"}, NoProxy: true}
	if _, err := app.SaveProviderWithKey(view, "legacy-key"); err != nil {
		t.Fatal(err)
	}
	if err := editUserConfig(func(c *config.Config) error {
		return c.UpsertRemoteHost(config.RemoteHostEntry{Name: "legacy-host", Host: "127.0.0.1", CredentialMode: "local-proxy"})
	}); err != nil {
		t.Fatal(err)
	}
	tab := &remoteTab{
		id: "legacy-tab", ref: RemoteTabRef{HostID: "legacy-host", Workspace: "ws"},
		state: "ready", client: &http.Client{}, model: "legacy/m", gen: 1,
		routing: remoteTabSessionRouting{currentPath: "/proj/session"},
	}
	app.remoteTabs = map[string]*remoteTab{tab.id: tab}
	kernel := &reconnectingUnsupportedKernel{reconnect: func() {
		app.remoteTabMu.Lock()
		tab.gen++
		app.remoteTabMu.Unlock()
	}}
	app.remoteMu.Lock()
	app.remoteRuntime = kernel
	app.remoteMu.Unlock()

	revision, admittedGen, err := app.ensureRemoteModelSettings(tab.id)
	if err != nil || revision != "" || admittedGen != 2 {
		t.Fatalf("replaced connection admission failed: revision=%q gen=%d err=%v", revision, admittedGen, err)
	}
	if kernel.switches != 2 {
		t.Fatalf("expected the replacement generation to be re-probed, switches=%d", kernel.switches)
	}
	app.remoteTabMu.Lock()
	recorded := tab.settings.unsupportedGen == 2
	app.remoteTabMu.Unlock()
	if !recorded {
		t.Fatalf("verdict was not recorded on the current generation: unsupportedGen=%d gen=%d", tab.settings.unsupportedGen, tab.gen)
	}
}

// The application status must not leave legacy generations pending forever:
// a Serve without the protocol never applies snapshots, so the target reports
// not_required and the receipt stops waiting and polling.
func TestAppendRemoteModelSettingsReportsLegacyTargetNotRequired(t *testing.T) {
	isolateDesktopUserDirs(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{}"))
	}))
	defer upstream.Close()
	app := NewApp()
	defer app.closeCredentialProxy()
	view := ProviderView{Name: "legacy", Kind: "openai", BaseURL: upstream.URL, Models: []string{"m"}, NoProxy: true}
	if _, err := app.SaveProviderWithKey(view, "legacy-key"); err != nil {
		t.Fatal(err)
	}
	if err := editUserConfig(func(c *config.Config) error {
		return c.UpsertRemoteHost(config.RemoteHostEntry{Name: "legacy-host", Host: "127.0.0.1", CredentialMode: "local-proxy"})
	}); err != nil {
		t.Fatal(err)
	}
	tab := &remoteTab{
		id: "legacy-tab", ref: RemoteTabRef{HostID: "legacy-host", Workspace: "ws"},
		model: "legacy/m", gen: 3,
	}
	tab.settings.unsupportedGen = 3
	app.remoteTabs = map[string]*remoteTab{tab.id: tab}

	result := emptyModelSettingsResult()
	app.appendRemoteModelSettingsStatus(&result)
	if len(result.Targets) != 1 || result.Targets[0].Application != "not_required" {
		t.Fatalf("legacy target not reported as not_required: %+v", result.Targets)
	}
	if result.Application == "pending" {
		t.Fatal("legacy target drove the receipt into a pending application")
	}
}

// Fresh and restored tabs run generation 0 with an unrecorded verdict; they
// must stay pending in the application status instead of claiming
// not_required before any capability probe has run.
func TestAppendRemoteModelSettingsFreshTabStaysPending(t *testing.T) {
	isolateDesktopUserDirs(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{}"))
	}))
	defer upstream.Close()
	app := NewApp()
	defer app.closeCredentialProxy()
	view := ProviderView{Name: "legacy", Kind: "openai", BaseURL: upstream.URL, Models: []string{"m"}, NoProxy: true}
	if _, err := app.SaveProviderWithKey(view, "legacy-key"); err != nil {
		t.Fatal(err)
	}
	if err := editUserConfig(func(c *config.Config) error {
		return c.UpsertRemoteHost(config.RemoteHostEntry{Name: "legacy-host", Host: "127.0.0.1", CredentialMode: "local-proxy"})
	}); err != nil {
		t.Fatal(err)
	}
	tab := &remoteTab{
		id: "fresh-tab", ref: RemoteTabRef{HostID: "legacy-host", Workspace: "ws"},
		model: "legacy/m",
	}
	app.remoteTabs = map[string]*remoteTab{tab.id: tab}

	result := emptyModelSettingsResult()
	app.appendRemoteModelSettingsStatus(&result)
	if len(result.Targets) != 1 || result.Targets[0].Application != "pending" {
		t.Fatalf("fresh tab not reported as pending: %+v", result.Targets)
	}
	if result.Application != "pending" {
		t.Fatalf("fresh tab did not keep the receipt pending: %q", result.Application)
	}
}

// SubmitRemoteTab must only deliver an unrevisioned turn to the connection the
// legacy admission was recorded for: a replaced generation re-admits against
// the new target before the request leaves, and the Serve never receives the
// optional expected-model-settings header on this path.
func TestSubmitRemoteTabFencesLegacyAdmissionToItsTarget(t *testing.T) {
	isolateDesktopUserDirs(t)
	submits := make(chan string, 4)
	serve := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/submit" {
			submits <- r.Header.Get(expectedModelSettingsHeader)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "unexpected path", http.StatusNotFound)
	}))
	defer serve.Close()
	app := NewApp()
	defer app.closeCredentialProxy()
	view := ProviderView{Name: "legacy", Kind: "openai", BaseURL: serve.URL, Models: []string{"m"}, NoProxy: true}
	if _, err := app.SaveProviderWithKey(view, "legacy-key"); err != nil {
		t.Fatal(err)
	}
	if err := editUserConfig(func(c *config.Config) error {
		return c.UpsertRemoteHost(config.RemoteHostEntry{Name: "legacy-host", Host: "127.0.0.1", CredentialMode: "local-proxy"})
	}); err != nil {
		t.Fatal(err)
	}
	tab := &remoteTab{
		id: "legacy-tab", ref: RemoteTabRef{HostID: "legacy-host", Workspace: "ws"},
		state: "ready", client: serve.Client(), base: serve.URL, model: "legacy/m", gen: 1,
		routing: remoteTabSessionRouting{currentPath: "/proj/session"},
	}
	app.remoteTabs = map[string]*remoteTab{tab.id: tab}
	kernel := &unsupportedModelSettingsKernel{}
	app.remoteMu.Lock()
	app.remoteRuntime = kernel
	app.remoteMu.Unlock()

	if err := app.SubmitRemoteTab(tab.id, "first turn"); err != nil {
		t.Fatalf("legacy submit failed: %v", err)
	}
	// A reconnect replaced the target generation; the next submit re-admits
	// against it before delivering the turn.
	app.remoteTabMu.Lock()
	tab.gen = 2
	app.remoteTabMu.Unlock()
	if err := app.SubmitRemoteTab(tab.id, "second turn"); err != nil {
		t.Fatalf("submit after reconnect failed: %v", err)
	}
	if kernel.switches != 2 {
		t.Fatalf("replaced generation was not re-admitted, switches=%d", kernel.switches)
	}
	close(submits)
	seen := 0
	for header := range submits {
		seen++
		if header != "" {
			t.Fatalf("legacy submit carried a model-settings revision header: %q", header)
		}
	}
	if seen != 2 {
		t.Fatalf("expected both turns delivered, got %d", seen)
	}
}
