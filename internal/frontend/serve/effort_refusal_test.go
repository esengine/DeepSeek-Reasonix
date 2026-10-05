package serve

import (
	"encoding/json"
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

// An endpoint with no effort vocabulary refusing a level is an answer about the
// request, not a broken host. Reporting it as 500 told the user their machine
// had failed and left them nothing to act on — and a third-party relay declares
// no vocabulary at all (TestModelsOfferOnlyTheEffortLevelsTheEndpointHas), so
// this was every effort switch such a user made.
func TestEffortRefusalIsNotAnInternalError(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	seed := `config_version = 6
default_model = "relay/claude-sonnet-4-5"

[[providers]]
name        = "relay"
kind        = "openai"
base_url    = "https://relay.example.com/v1"
models      = ["claude-sonnet-4-5"]
default     = "claude-sonnet-4-5"
api_key_env = "RELAY_API_KEY"
`
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, ModelRef: "relay/claude-sonnet-4-5"})
	defer ctrl.Close()
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	defer srv.Close()

	res, err := http.Post(srv.URL+"/effort", "application/json", strings.NewReader(`{"effort":"high"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusInternalServerError {
		t.Fatal("a level this endpoint does not have came back as an internal server error")
	}
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", res.StatusCode)
	}
	var reason Reason
	if err := json.NewDecoder(res.Body).Decode(&reason); err != nil {
		t.Fatalf("decode reason: %v", err)
	}
	if reason.Code != "effort.not_configurable" {
		t.Fatalf("code = %q, want effort.not_configurable", reason.Code)
	}
	// The message has to name the way out; the provider panel cannot declare a
	// vocabulary, so the config block is where the user has to go.
	if !strings.Contains(reason.Message, "supported_efforts") {
		t.Fatalf("message = %q, want it to name what would give this endpoint a ladder", reason.Message)
	}
}
