package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	_ "reasonix/internal/model/openai"
	"reasonix/internal/state/stats"
)

// The date-range endpoint must survive the real boot path, not only the direct
// handler setup: a historical range reaches the same stats file the UI reads.
func TestUsageDateRangeThroughBootBuild(t *testing.T) {
	home := testenv.TempDir(t)
	workspace := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	seed := `default_model = "boot/openai-compatible"

[[providers]]
name = "boot"
kind = "openai"
base_url = "https://example.com/v1"
model = "openai-compatible"
`
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	day := time.Now().AddDate(0, -2, 0)
	line := map[string]any{"ts": day.Format(time.RFC3339), "model": "deepseek-flash/deepseek-flash", "source": "desktop", "total": 100}
	encoded, err := json.Marshal(line)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config.StatsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.StatsDir(), day.Format("2006-01-02")+".jsonl"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	bc := NewBroadcaster()
	ctrl, err := boot.Build(t.Context(), boot.Options{Sink: event.Discard, Home: home, WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(ctrl.Close)
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	t.Cleanup(srv.Close)

	target := srv.URL + "/usage?from=" + day.Format("2006-01-02") + "&to=" + day.Format("2006-01-02")
	resp, err := http.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /usage = %d, want 200", resp.StatusCode)
	}
	var got stats.RangeStats
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.From != day.Format("2006-01-02") || got.To != day.Format("2006-01-02") || got.Tokens != 100 {
		t.Fatalf("range = %s..%s tokens=%d, want that exact historical day", got.From, got.To, got.Tokens)
	}
}
