package boot

import (
	"errors"
	"testing"
	"time"

	"reasonix/internal/base/netclient"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
)

// A hand-edited idle_timeout_seconds never passes UpsertProvider's validation,
// so the bound has to hold where the value is read: an unbounded
// time.Duration(v)*time.Second wraps int64 negative and the pre-header deadline
// vanishes.
func TestIdleTimeoutBoundHoldsOnHandEditedConfig(t *testing.T) {
	home, ws := testenv.TempDir(t), testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	writeFile(t, home, "config.toml", `config_version = 1
default_model = "gateway/my-model"

[[providers]]
name                 = "gateway"
kind                 = "openai"
base_url             = "http://localhost:8021/v1"
models               = ["my-model"]
api_key_env          = "GATEWAY_API_KEY"
idle_timeout_seconds = 10000000000
`)

	cfg, err := config.LoadForRoot(ws)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	e, ok := cfg.Provider("gateway")
	if !ok {
		t.Fatal("gateway provider missing after load")
	}
	got := provider.IdleTimeoutFromExtra(providerConfig(e, netclient.ProxySpec{}).Extra)
	if got != provider.MaxIdleTimeoutSeconds*time.Second {
		t.Fatalf("idle timeout = %v, want the %ds ceiling", got, provider.MaxIdleTimeoutSeconds)
	}
}

func TestNewProviderRefusesASourceThatDoesNotAnswerConversation(t *testing.T) {
	entry := &config.ProviderEntry{Name: "laya", Kind: "typesafe", Model: "typed-decisions", BaseURL: "http://127.0.0.1:8700"}
	_, err := NewProviderWithProxy(entry, netclient.ProxySpec{})
	var mismatch *config.AnswersMismatchError
	if !errors.As(err, &mismatch) || mismatch.Has != config.AnswersDecision || mismatch.Ref != "laya/typed-decisions" {
		t.Fatalf("err = %v, want a typed answers mismatch naming laya/typed-decisions", err)
	}
}
