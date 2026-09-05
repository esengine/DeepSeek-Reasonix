package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/netclient"
	"reasonix/internal/provider"
)

// Exercises the public settings contract, persistent config, discovery cache,
// runtime factory and real HTTP serializers without external keys or probes.
func TestIDOnlyRelayImageInputSettingsToWire(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Chdir(t.TempDir())
	for _, kind := range []string{"openai", "anthropic", "responses"} {
		t.Run(kind, func(t *testing.T) {
			captured := make(chan []byte, 16)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					fmt.Fprint(w, `{"data":[{"id":"relay-model"}]}`)
					return
				}
				body, _ := io.ReadAll(r.Body)
				captured <- body
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
			defer srv.Close()
			app := NewApp()
			view := ProviderView{Name: "image-test-" + kind, Kind: kind, BaseURL: srv.URL, Models: []string{"relay-model"}, NoProxy: true}
			discover := func() []ProviderModelCapabilityView {
				t.Helper()
				v, err := app.FetchProviderModelCatalog(view)
				if err != nil {
					t.Fatal(err)
				}
				return v
			}
			facts := discover()
			if len(facts) != 1 || facts[0].State != "unknown" || facts[0].InputModalities == nil {
				t.Fatalf("ID-only = %+v", facts)
			}
			// A frontend-provided capability is not a discovery fact or a writable override.
			view.ModelCapabilities = []ProviderModelCapabilityView{{Model: "relay-model", State: "supported", Source: "adapter", InputModalities: []string{"text", "image"}}}
			load := func() *config.Config {
				t.Helper()
				c, err := config.LoadForRoot(".")
				if err != nil {
					t.Fatal(err)
				}
				return c
			}
			send := func(c *config.Config, withImage bool) map[string]any {
				t.Helper()
				p, err := boot.NewLocalProviderResolver(c, netclient.ProxySpec{Mode: netclient.ModeOff}).Resolve(provider.Selection{Ref: view.Name + "/relay-model"})
				if err != nil {
					t.Fatal(err)
				}
				messages := []provider.Message{{Role: provider.RoleSystem, Content: "stable system prefix"}, {Role: provider.RoleUser, Content: "describe"}}
				if withImage {
					messages[1].Images = []string{"data:image/png;base64,aGVsbG8="}
				}
				stream, err := p.Stream(context.Background(), provider.Request{Messages: messages, Tools: []provider.ToolSchema{{Name: "test_tool", Description: "stable tool", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}}})
				if err != nil {
					t.Fatal(err)
				}
				for chunk := range stream {
					if chunk.Err != nil {
						t.Fatal(chunk.Err)
					}
				}
				var body map[string]any
				if err := json.Unmarshal(<-captured, &body); err != nil {
					t.Fatal(err)
				}
				return body
			}
			if err := app.SaveProvider(view); err != nil {
				t.Fatal(err)
			}
			unknown := send(load(), true)
			if strings.Contains(fmt.Sprint(unknown), "aGVsbG8=") {
				t.Fatal("unknown sent native image")
			}
			textBefore := send(load(), false)
			on := true
			view.ModelOverrides = []ProviderModelOverrideView{{Model: "relay-model", Vision: &on, ContextWindow: 123456, MaxOutputTokens: 4321}}
			if err := app.SaveProvider(view); err != nil {
				t.Fatal(err)
			}
			enabled := send(load(), true)
			serialized, _ := json.Marshal(enabled)
			if !strings.Contains(string(serialized), "aGVsbG8=") {
				t.Fatalf("enabled image missing: %s", serialized)
			}
			switch kind {
			case "openai":
				if !strings.Contains(string(serialized), `"type":"image_url"`) {
					t.Fatal("Chat image block missing")
				}
			case "anthropic":
				if !strings.Contains(string(serialized), `"type":"image"`) {
					t.Fatal("Messages image block missing")
				}
			case "responses":
				if !strings.Contains(string(serialized), `"type":"input_image"`) {
					t.Fatal("Responses image block missing")
				}
			}
			// Simulated restart: a fresh app and resolver read saved user choices;
			// ID-only refresh cannot turn an override into a discovered positive fact.
			app = NewApp()
			facts = discover()
			if facts[0].State != "supported" || facts[0].AutomaticState != "unknown" {
				t.Fatalf("refresh lost separation: %+v", facts)
			}
			saved := load()
			entry, _ := saved.ResolveModel(view.Name + "/relay-model")
			if config.NewModelCapabilityResolver().Resolve(entry).State != config.CapabilitySupported {
				t.Fatal("restart lost setting")
			}
			off := false
			view.ModelOverrides[0].Vision = &off
			if err := app.SaveProvider(view); err != nil {
				t.Fatal(err)
			}
			disabled := send(load(), true)
			if strings.Contains(fmt.Sprint(disabled), "aGVsbG8=") {
				t.Fatal("disabled sent native image")
			}
			textOff := send(load(), false)
			view.ModelOverrides[0].Vision = &on
			if err := app.SaveProvider(view); err != nil {
				t.Fatal(err)
			}
			textOn := send(load(), false)
			if !reflect.DeepEqual(textOn, textOff) {
				t.Fatalf("image toggle changed text request: on=%v off=%v", textOn, textOff)
			}
			if !reflect.DeepEqual(textBefore["tools"], textOn["tools"]) {
				t.Fatal("tool schema changed")
			}
			view.ModelOverrides[0].Vision = nil
			if err := app.SaveProvider(view); err != nil {
				t.Fatal(err)
			}
			entry, _ = load().ResolveModel(view.Name + "/relay-model")
			if got := config.NewModelCapabilityResolver().Resolve(entry); got.State != config.CapabilityUnknown {
				t.Fatalf("auto = %+v", got)
			}
			if entry.ContextWindow != 123456 || entry.MaxOutputTokens != 4321 {
				t.Fatal("auto erased unrelated overrides")
			}
		})
	}
}

func TestImageInputSaveRebuildsActiveAndNextTurnRefreshesOtherTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	t.Chdir(root)
	app := NewApp()
	view := ProviderView{Name: "relay", Kind: "openai", BaseURL: "http://127.0.0.1:1", Models: []string{"relay-model"}}
	if err := app.SaveProvider(view); err != nil {
		t.Fatal(err)
	}
	build := func() *control.Controller {
		t.Helper()
		ctrl, err := boot.Build(context.Background(), boot.Options{Model: "relay/relay-model", WorkspaceRoot: root, Sink: event.Discard})
		if err != nil {
			t.Fatal(err)
		}
		return ctrl
	}
	active, other := build(), build()
	app.ctx = context.Background()
	app.readyHook = func() {}
	app.setTestCtrl(active, "relay/relay-model")
	app.activeTab().WorkspaceRoot = root
	otherTab := &WorkspaceTab{ID: "other-image", Scope: "global", WorkspaceRoot: root, model: "relay/relay-model", Label: "relay/relay-model", Ctrl: other, Ready: true, disabledMCP: map[string]ServerView{}}
	otherTab.sink = &tabEventSink{tabID: otherTab.ID, app: app, ctx: context.Background()}
	app.tabs[otherTab.ID] = otherTab
	installNoopRuntimeEvents(app, otherTab.sink)
	t.Cleanup(func() {
		for _, tab := range app.tabs {
			if tab.Ctrl != nil {
				tab.Ctrl.Close()
			}
			tab.releaseSessionLease()
		}
	})
	on := true
	view.ModelOverrides = []ProviderModelOverrideView{{Model: "relay-model", Vision: &on}}
	if err := app.SaveProvider(view); err != nil {
		t.Fatal(err)
	}
	if app.activeCtrl() == active || !app.Meta().ImageInputEnabled {
		t.Fatal("save did not rebuild and publish active capability")
	}
	if app.MetaForTab(otherTab.ID).ImageInputEnabled || otherTab.Ctrl != other {
		t.Fatal("other idle tab displayed saved config before rebuilding")
	}
	admission, current, err := app.beginTabTurn(otherTab.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	admission.abort()
	if current == other || !app.MetaForTab(otherTab.ID).ImageInputEnabled {
		t.Fatal("other tab admitted next turn with stale capability")
	}
}

func TestDiscoveryRejectsChangedIdentityWhileWaiting(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Chdir(t.TempDir())
	started, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		fmt.Fprint(w, `{"data":[{"id":"relay-model","vision":true}]}`)
	}))
	defer srv.Close()
	app := NewApp()
	view := ProviderView{Name: "relay", Kind: "openai", BaseURL: srv.URL, Models: []string{"relay-model"}}
	if err := app.SaveProvider(view); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := app.FetchProviderModelCatalog(view); result <- err }()
	<-started
	view.RequestURL = srv.URL + "/other/chat/completions"
	if err := app.SaveProvider(view); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-result; err == nil {
		t.Fatal("stale discovery accepted after route edit")
	}
}

func TestDiscoveryNewestSuccessfulRequestWins(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Chdir(t.TempDir())
	started, release := make(chan struct{}), make(chan struct{})
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
			<-release
			fmt.Fprint(w, `{"data":[{"id":"relay-model","vision":true}]}`)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"relay-model"}]}`)
	}))
	defer srv.Close()
	app := NewApp()
	view := ProviderView{Name: "relay", Kind: "openai", BaseURL: srv.URL, NoProxy: true}
	old := make(chan []ProviderModelCapabilityView, 1)
	errors := make(chan error, 1)
	go func() { models, err := app.FetchProviderModelCatalog(view); old <- models; errors <- err }()
	<-started
	newer, err := app.FetchProviderModelCatalog(view)
	close(release)
	if err != nil || len(newer) != 1 || newer[0].State != "unknown" {
		t.Fatalf("newer: %+v %v", newer, err)
	}
	older := <-old
	if err := <-errors; err != nil || len(older) != 1 || older[0].State != "unknown" {
		t.Fatalf("late positive response replaced unknown: %+v %v", older, err)
	}
}

func TestDiscoveryRejectsChangedCredentialsWhileWaiting(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Chdir(t.TempDir())
	started, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		fmt.Fprint(w, `{"data":[{"id":"relay-model","vision":true}]}`)
	}))
	defer srv.Close()
	app := NewApp()
	view := ProviderView{Name: "relay", Kind: "openai", BaseURL: srv.URL, NoProxy: true, APIKeyEnv: "REASONIX_IMAGE_TEST_KEY"}
	if _, err := app.SaveProviderKey(view.APIKeyEnv, "local-test-before"); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := app.FetchProviderModelCatalog(view); result <- err }()
	<-started
	if _, err := app.SaveProviderKey(view.APIKeyEnv, "local-test-after"); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-result; err == nil {
		t.Fatal("stale discovery accepted after credentials changed")
	}
}

func TestDiscoveryFailurePreservesSuccessfulCache(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Chdir(t.TempDir())
	var mode atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode.Load() == 1 {
			http.Error(w, "temporary failure", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"relay-model","vision":true}]}`)
	}))
	defer srv.Close()
	app := NewApp()
	view := ProviderView{Name: "relay", Kind: "openai", BaseURL: srv.URL, NoProxy: true, Models: []string{"relay-model"}}
	if _, err := app.FetchProviderModelCatalog(view); err != nil {
		t.Fatal(err)
	}
	mode.Store(1)
	if _, err := app.FetchProviderModelCatalog(view); err == nil {
		t.Fatal("expected discovery error")
	}
	e := config.ProviderEntry{Name: view.Name, Kind: view.Kind, BaseURL: view.BaseURL, NoProxy: true, Model: "relay-model"}
	if got := config.NewModelCapabilityResolver().Resolve(&e); got.State != config.CapabilitySupported {
		t.Fatalf("failure erased success: %+v", got)
	}
}

func TestBatchDiscoveryPersistsFactsForNoProxyIdentity(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Chdir(t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"relay-model","vision":true}]}`)
	}))
	defer srv.Close()

	view := ProviderView{Name: "relay", Kind: "openai", BaseURL: srv.URL, NoProxy: true, Models: []string{"relay-model"}}
	got := NewApp().FetchAllProviderModelCatalogs([]ProviderView{view})
	if len(got[view.Name]) != 1 || got[view.Name][0].State != "supported" {
		t.Fatalf("batch catalog = %+v", got)
	}
	entry := config.ProviderEntry{Name: view.Name, Kind: view.Kind, BaseURL: view.BaseURL, NoProxy: true, Model: "relay-model"}
	if capability := config.NewModelCapabilityResolver().Resolve(&entry); capability.State != config.CapabilitySupported {
		t.Fatalf("batch result was not stored under the no_proxy identity: %+v", capability)
	}
}
