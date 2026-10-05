package serve

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

func welcomeSeen(t *testing.T, base string) bool {
	t.Helper()
	resp, err := http.Get(base + "/welcome")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /welcome = %d", resp.StatusCode)
	}
	var out struct {
		Seen bool `json:"seen"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Seen
}

// TestWelcomeIsOwedOnceThenNeverAgain covers the whole contract: a fresh
// machine is owed the sequence, acknowledging it writes through to the config,
// and the answer survives a reload — which is why this lives in the config and
// not in browser storage.
func TestWelcomeIsOwedOnceThenNeverAgain(t *testing.T) {
	srv := newRichProviderServer(t)

	if welcomeSeen(t, srv.URL) {
		t.Fatal("a fresh machine was told it had already seen the sequence")
	}

	resp, err := http.Post(srv.URL+"/welcome", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /welcome = %d", resp.StatusCode)
	}

	if !welcomeSeen(t, srv.URL) {
		t.Fatal("the acknowledgement did not stick")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Desktop.Welcomed {
		t.Fatal("welcomed never reached the config file")
	}
	// The switch owns one field and must not disturb the rest of the file.
	if entry, ok := cfg.Provider("rich"); !ok || entry.ContextWindow != 131072 {
		t.Fatalf("marking the sequence seen flattened unrelated config: %+v", entry)
	}
}
