package tui_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/frontend/serve"
	"reasonix/internal/frontend/tui"
	"reasonix/internal/state/history"
)

const setupKeyEnv = "REASONIX_TUI_SETUP_E2E_KEY"

// A first run with no key reports the setup as owed through the in-process
// kernel; testing and saving a key then clear it without rewriting the user's
// config file.
func TestFirstRunSetupThroughTheInProcessKernel(t *testing.T) {
	home := testenv.TempDir(t)
	for _, k := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME"} {
		t.Setenv(k, home)
	}
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv(setupKeyEnv, "")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = history.CloseSharedCatalog(ctx)
	})
	dir := testenv.TempDir(t)
	t.Chdir(dir)
	kind := "tui-setup-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	model := &scriptedModel{}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return model, nil })
	userConfig := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `default_model = "first-run/m1"
unknown_future_key = "kept"

[codegraph]
enabled = false

[[providers]]
name = "first-run"
kind = "` + kind + `"
model = "m1"
api_key_env = "` + setupKeyEnv + `"
`
	if err := os.WriteFile(userConfig, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	bc := serve.NewBroadcaster()
	ctrl, err := boot.Build(context.Background(), boot.Options{Sink: bc})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	hub := serve.NewHub(serve.HubOptions{})
	t.Cleanup(hub.Shutdown)
	if _, err := hub.Adopt(serve.New(ctrl, bc, config.ServeConfig{}), bc); err != nil {
		t.Fatal(err)
	}
	hub.EnableProviderSetupInProcess()

	c := &tui.Client{HTTP: hub.InProcessClient(), Base: "http://reasonix.local/rt/r1"}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	state, err := c.SetupState(ctx)
	if err != nil || !state.Required {
		t.Fatalf("setup state = %+v, %v; want required", state, err)
	}
	list, err := c.Connections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, cn := range list.Connections {
		if cn.Name == "first-run" {
			found = cn.Active && cn.KeyRequired && cn.Models == 1
		}
	}
	if !found {
		t.Fatalf("connections = %+v, want first-run active and key required", list)
	}
	if err := c.TestConnection(ctx, "first-run", "sk-test-key"); err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if config.CredentialStored(setupKeyEnv) {
		t.Fatal("testing a key stored it")
	}
	if err := c.TestConnection(ctx, "missing", "sk"); tui.Code(err) != "provider.unknown" {
		t.Fatalf("unknown provider test = %v, want provider.unknown", err)
	}
	model.mu.Lock()
	probed := model.calls
	model.mu.Unlock()
	if err := c.Submit(ctx, "hello"); tui.Code(err) != tui.CodeKeyMissing {
		t.Fatalf("keyless submit = %v, want %s", err, tui.CodeKeyMissing)
	}
	model.mu.Lock()
	sent := model.calls
	model.mu.Unlock()
	if sent != probed {
		t.Fatalf("the provider saw %d requests for a keyless turn", sent-probed)
	}
	if err := c.SaveConnection(ctx, "first-run", "sk-test-key", list.Revision); err != nil {
		t.Fatalf("save connection: %v", err)
	}
	if got := config.ResolveCredentialForRootGlobalFirst(".", setupKeyEnv); !got.Set || got.Value != "sk-test-key" {
		t.Fatalf("stored credential = %+v", got)
	}
	after, err := os.ReadFile(userConfig)
	if err != nil || string(after) != body {
		t.Fatalf("config.toml changed by saving a key:\n%s", after)
	}
	if state, _ = c.SetupState(ctx); state.Required {
		t.Fatal("setup still owed after the key was saved")
	}
	if strings.Contains(string(after), "sk-test-key") {
		t.Fatal("the key leaked into config.toml")
	}
	if err := c.Submit(ctx, "hello"); err != nil {
		t.Fatalf("submit after the key was saved: %v", err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		model.mu.Lock()
		sent = model.calls
		model.mu.Unlock()
		if sent > probed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no request reached the provider after the key was saved")
		}
	}
}
