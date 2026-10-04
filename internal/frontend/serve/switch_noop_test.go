package serve

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func TestServeSwitchEffortNoopsSameLevelBeforeBuild(t *testing.T) {
	writeServeEffortSelectionConfig(t, "high")
	bc := NewBroadcaster()
	identity := serveProviderIdentity(t, "alternate/shared-chat")
	ctrl := control.New(control.Options{
		Sink:                bc,
		ModelRef:            "alternate/shared-chat",
		Effort:              "high",
		ProviderFingerprint: identity.Fingerprint,
		SessionDir:          testenv.TempDir(t),
	})
	defer ctrl.Close()
	server := New(ctrl, bc, config.ServeConfig{})
	built := 0
	server.buildController = func(_ context.Context, ref string) (*control.Controller, error) {
		built++
		return control.New(control.Options{
			Sink:       bc,
			ModelRef:   ref,
			Effort:     "low",
			SessionDir: testenv.TempDir(t),
		}), nil
	}

	if err := server.switchEffort(context.Background(), "high"); err != nil {
		t.Fatalf("same-level switchEffort: %v", err)
	}
	if built != 0 {
		t.Fatalf("same-level switch rebuilt controller %d times", built)
	}
	if err := server.switchEffort(context.Background(), "ultra"); err == nil {
		t.Fatal("unsupported effort was accepted")
	}
	if built != 0 {
		t.Fatalf("invalid effort reached builder %d times", built)
	}
}

// A same effective effort still has to be persisted: the running controller may
// have started from a different config.toml value (or an implicit default), so
// returning before the write would make the picker and the next start disagree.
func TestServeSwitchEffortPersistsSameLevelWithoutRebuild(t *testing.T) {
	writeServeEffortSelectionConfig(t, "low")
	bc := NewBroadcaster()
	identity := serveProviderIdentity(t, "alternate/shared-chat")
	ctrl := control.New(control.Options{
		Sink:                bc,
		ModelRef:            "alternate/shared-chat",
		Effort:              "high",
		ProviderFingerprint: identity.Fingerprint,
		SessionDir:          testenv.TempDir(t),
	})
	defer ctrl.Close()
	server := New(ctrl, bc, config.ServeConfig{})
	built := 0
	server.buildController = func(context.Context, string) (*control.Controller, error) {
		built++
		return control.New(control.Options{Sink: bc}), nil
	}

	if err := server.switchEffort(context.Background(), "high"); err != nil {
		t.Fatalf("same-effective-level switchEffort: %v", err)
	}
	if built != 0 {
		t.Fatalf("same-effective-level switch rebuilt controller %d times", built)
	}
	edit := config.LoadForEdit(config.UserConfigPath())
	entry, ok := edit.Provider("alternate")
	if !ok {
		t.Fatal("alternate provider missing after effort switch")
	}
	if entry.Effort != "high" {
		t.Fatalf("persisted effort = %q, want high", entry.Effort)
	}
}

func TestServeSwitchEffortPersistsExplicitDefaultWithoutRebuild(t *testing.T) {
	writeServeDefaultEffortSelectionConfig(t)
	bc := NewBroadcaster()
	identity := serveProviderIdentity(t, "alternate/shared-chat")
	ctrl := control.New(control.Options{
		Sink:                bc,
		ModelRef:            "alternate/shared-chat",
		Effort:              identity.Effort,
		ProviderFingerprint: identity.Fingerprint,
		SessionDir:          testenv.TempDir(t),
	})
	defer ctrl.Close()
	server := New(ctrl, bc, config.ServeConfig{})
	built := 0
	server.buildController = func(context.Context, string) (*control.Controller, error) {
		built++
		return control.New(control.Options{Sink: bc}), nil
	}

	if err := server.switchEffort(context.Background(), identity.Effort); err != nil {
		t.Fatalf("persist explicit provider default: %v", err)
	}
	if built != 0 {
		t.Fatalf("explicit provider default rebuilt controller %d times", built)
	}
	edit := config.LoadForEdit(config.UserConfigPath())
	entry, ok := edit.Provider("alternate")
	if !ok {
		t.Fatal("alternate provider missing after effort switch")
	}
	if entry.Effort != "high" {
		t.Fatalf("persisted effort = %q, want explicit high", entry.Effort)
	}
}

// The model ref can stay the same while the resolved provider entry changes.
// A provider edit or credential rotation must not be hidden by a ref-only no-op.
func TestServeSwitchModelRebuildsWhenResolvedProviderChanges(t *testing.T) {
	writeServeEffortSelectionConfig(t, "high")
	bc := NewBroadcaster()
	identity := serveProviderIdentity(t, "alternate/shared-chat")
	ctrl := control.New(control.Options{
		Sink:                bc,
		ModelRef:            "alternate/shared-chat",
		Effort:              "high",
		ProviderFingerprint: identity.Fingerprint,
		SessionDir:          testenv.TempDir(t),
	})
	defer ctrl.Close()
	server := New(ctrl, bc, config.ServeConfig{})
	built := 0
	server.buildController = func(_ context.Context, ref string) (*control.Controller, error) {
		built++
		return control.New(control.Options{
			Sink:       bc,
			ModelRef:   ref,
			Effort:     "high",
			SessionDir: testenv.TempDir(t),
		}), nil
	}

	path := config.UserConfigPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(raw), "http://127.0.0.1:1/v1", "http://127.0.0.1:2/v1", 1)
	if changed == string(raw) {
		t.Fatal("test config did not contain the expected base URL")
	}
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := server.switchModelRequested(context.Background(), "alternate/shared-chat"); err != nil {
		t.Fatalf("same-ref model switch after provider edit: %v", err)
	}
	if built != 1 {
		t.Fatalf("same-ref provider edit rebuilt %d times, want 1", built)
	}
}

func TestServeModelAndEffortNoopWhileRunning(t *testing.T) {
	writeServeEffortSelectionConfig(t, "high")
	bc := NewBroadcaster()
	identity := serveProviderIdentity(t, "alternate/shared-chat")
	ctrl := control.New(control.Options{
		Runner:              blockingRunner{},
		Sink:                bc,
		ModelRef:            "alternate/shared-chat",
		Effort:              "high",
		ProviderFingerprint: identity.Fingerprint,
		SessionDir:          testenv.TempDir(t),
	})
	defer ctrl.Close()
	server := New(ctrl, bc, config.ServeConfig{})
	built := 0
	server.buildController = func(context.Context, string) (*control.Controller, error) {
		built++
		return control.New(control.Options{Sink: bc}), nil
	}
	srv := httptest.NewServer(operatorHandler(server))
	defer srv.Close()

	ctrl.SubmitHTTP("keep running")
	waitRunning(t, ctrl)
	for _, tc := range []struct {
		path string
		body string
	}{
		{path: "/model", body: `{"ref":"alternate/shared-chat"}`},
		{path: "/effort", body: `{"effort":"high"}`},
	} {
		resp, err := http.Post(srv.URL+tc.path, "application/json", strings.NewReader(tc.body))
		if err != nil {
			t.Fatalf("POST %s: %v", tc.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("POST %s while running = %d, want 204", tc.path, resp.StatusCode)
		}
	}
	if built != 0 {
		t.Fatalf("same-selection switch while running rebuilt controller %d times", built)
	}
	ctrl.Cancel()
	waitNotRunning(t, ctrl)
}

func TestServeSubmitNoopsSameModelAndEffortBeforeBuild(t *testing.T) {
	writeServeEffortSelectionConfig(t, "high")
	bc := NewBroadcaster()
	identity := serveProviderIdentity(t, "alternate/shared-chat")
	ctrl := control.New(control.Options{
		Sink:                bc,
		ModelRef:            "alternate/shared-chat",
		Effort:              "high",
		ProviderFingerprint: identity.Fingerprint,
		SessionDir:          testenv.TempDir(t),
	})
	defer ctrl.Close()
	server := New(ctrl, bc, config.ServeConfig{})
	built := 0
	server.buildController = func(context.Context, string) (*control.Controller, error) {
		built++
		return control.New(control.Options{Sink: bc}), nil
	}
	srv := httptest.NewServer(operatorHandler(server))
	defer srv.Close()

	for _, input := range []string{"/model alternate/shared-chat", "/effort high"} {
		body := fmt.Sprintf(`{"input":%q}`, input)
		resp, err := http.Post(srv.URL+"/submit", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST /submit %q: %v", input, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("POST /submit %q = %d, want 204", input, resp.StatusCode)
		}
	}
	if built != 0 {
		t.Fatalf("same-selection CLI switches rebuilt controller %d times", built)
	}
}

func TestServeSubmitRefreshesSameModelAfterProviderEdit(t *testing.T) {
	writeServeEffortSelectionConfig(t, "high")
	bc := NewBroadcaster()
	identity := serveProviderIdentity(t, "alternate/shared-chat")
	ctrl := control.New(control.Options{
		Sink:                bc,
		ModelRef:            "alternate/shared-chat",
		Effort:              "high",
		ProviderFingerprint: identity.Fingerprint,
		SessionDir:          testenv.TempDir(t),
	})
	defer ctrl.Close()
	server := New(ctrl, bc, config.ServeConfig{})
	built := 0
	server.buildController = func(_ context.Context, ref string) (*control.Controller, error) {
		built++
		return control.New(control.Options{
			Sink:       bc,
			ModelRef:   ref,
			Effort:     "high",
			SessionDir: testenv.TempDir(t),
		}), nil
	}
	srv := httptest.NewServer(operatorHandler(server))
	defer srv.Close()

	path := config.UserConfigPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(raw), "http://127.0.0.1:1/v1", "http://127.0.0.1:2/v1", 1)
	if changed == string(raw) {
		t.Fatal("test config did not contain the expected base URL")
	}
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Post(srv.URL+"/submit", "application/json", strings.NewReader(`{"input":"/model alternate/shared-chat"}`))
	if err != nil {
		t.Fatalf("POST /submit /model: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /submit /model = %d, want 204", resp.StatusCode)
	}
	if built != 1 {
		t.Fatalf("submit same model after provider edit rebuilt %d times, want 1", built)
	}
}

func writeServeEffortSelectionConfig(t *testing.T, effort string) {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	cfgPath := config.UserConfigPath()
	if cfgPath == "" {
		t.Fatal("user config path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`default_model = "alternate/shared-chat"

[[providers]]
name = "alternate"
kind = "openai"
base_url = "http://127.0.0.1:1/v1"
models = ["shared-chat"]
default = "shared-chat"
supported_efforts = ["low", "high"]
effort = %q
`, effort)
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeServeDefaultEffortSelectionConfig(t *testing.T) {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	cfgPath := config.UserConfigPath()
	if cfgPath == "" {
		t.Fatal("user config path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `default_model = "alternate/shared-chat"

[[providers]]
name = "alternate"
kind = "openai"
base_url = "http://127.0.0.1:1/v1"
models = ["shared-chat"]
default = "shared-chat"
supported_efforts = ["low", "high"]
default_effort = "high"
`
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func serveProviderIdentity(t *testing.T, ref string) boot.ProviderBuildIdentity {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg.ResolveModel(ref)
	if !ok {
		t.Fatalf("resolve %s", ref)
	}
	return boot.ResolveProviderBuildIdentity(entry, cfg.NetworkProxySpec(), nil)
}
