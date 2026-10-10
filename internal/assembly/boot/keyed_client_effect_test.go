package boot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

type systemOneCaller struct{ round atomic.Int32 }

func (p *systemOneCaller) Name() string { return "boot-keyed-client" }

func (p *systemOneCaller) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	if p.round.Add(1) == 1 {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "s1", Name: "system_one",
			Arguments: `{"state":"s","questions":{"q":{"type":"noul","instructions":"is it?"}}}`,
		}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// The System One call and the balance read both carry the provider key, so a
// redirect off the configured address must not be followed by either.
func TestEffectKeyCarryingBuildClientsRefuseACrossOriginRedirect(t *testing.T) {
	var reached atomic.Int32
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Add(1) }))
	defer elsewhere.Close()
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
	}))
	defer home.Close()

	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	t.Setenv("KEYED_EFFECT_KEY", "secret")
	caller := &systemOneCaller{}
	provider.Register("typesafe", func(provider.Config) (provider.Provider, error) { return caller, nil })
	provider.Register("boot-keyed-chat", func(provider.Config) (provider.Provider, error) { return caller, nil })
	userCfg := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userCfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userCfg, []byte(`
default_model = "chat/m"

[agent]
decision_model = "decider/system-one"

[[providers]]
name = "chat"
kind = "boot-keyed-chat"
models = ["m"]
default = "m"
api_key_env = "KEYED_EFFECT_KEY"
balance_url = "`+home.URL+`/balance"

[[providers]]
name = "decider"
kind = "typesafe"
balance_url = "`+home.URL+`/balance"
base_url = "`+home.URL+`"
models = ["system-one"]
api_key_env = "KEYED_EFFECT_KEY"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "ask system one"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reading := ctrl.Balance(context.Background())
	if reading.Err == nil || !strings.Contains(reading.Err.Error(), "redirect refused") {
		t.Fatalf("balance reading = %+v, want a refused redirect", reading)
	}
	if n := reached.Load(); n != 0 {
		t.Fatalf("the redirect target received %d request(s)", n)
	}
}
