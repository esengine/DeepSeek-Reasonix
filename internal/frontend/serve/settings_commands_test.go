package serve

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func submitSlash(t *testing.T, base, input string) int {
	t.Helper()
	res, err := http.Post(base+"/submit", "application/json", strings.NewReader(`{"input":"`+input+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	return res.StatusCode
}

func replayText(bc *Broadcaster) string {
	frames, _ := bc.Replay(0)
	var b strings.Builder
	for _, f := range frames {
		b.Write(f)
	}
	return b.String()
}

func TestSettingsVerbsReachTheFrontendThroughSubmit(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	seed := `config_version = 6
default_model = "relay/m1"

[[providers]]
name        = "relay"
kind        = "openai"
base_url    = "https://relay.example.com/v1"
models      = ["m1"]
default     = "m1"
api_key_env = "RELAY_API_KEY"
`
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, ModelRef: "relay/m1"})
	defer ctrl.Close()
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	defer srv.Close()

	if code := submitSlash(t, srv.URL, "/currency usd"); code >= 300 {
		t.Fatalf("/currency status = %d", code)
	}
	if got := bc.DisplayCurrency(); got != "USD" {
		t.Fatalf("session ledger currency = %q, want USD", got)
	}
	cfg, err := config.Load()
	if err != nil || cfg.DisplayCurrencyPref() != "USD" {
		t.Fatalf("stored preference = %v, %v", cfg.DisplayCurrencyPref(), err)
	}

	if code := submitSlash(t, srv.URL, "/sandbox"); code >= 300 {
		t.Fatalf("/sandbox status = %d", code)
	}
	if text := replayText(bc); !strings.Contains(text, "OS bash sandbox") {
		t.Fatalf("no sandbox notice reached the stream: %s", text)
	}
}

func TestReloadIsRefusedNotTreatedAsUnknownWhenNothingCanRebuild(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc})
	defer ctrl.Close()
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	defer srv.Close()

	if code := submitSlash(t, srv.URL, "/reload"); code != http.StatusConflict {
		t.Fatalf("/reload status = %d, want 409", code)
	}
}
