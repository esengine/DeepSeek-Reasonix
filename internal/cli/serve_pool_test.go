package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/config"
)

func TestReadDesktopProjectRoots(t *testing.T) {
	home := config.ReasonixHomeDir()
	if home == "" {
		t.Fatal("ReasonixHomeDir empty under isolated test env")
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	rootA := filepath.Join(t.TempDir(), "a")
	rootB := filepath.Join(t.TempDir(), "b")
	type project struct {
		Root string `json:"root"`
	}
	b, err := json.Marshal(struct {
		Projects []project `json:"projects"`
	}{Projects: []project{{Root: rootA}, {Root: rootB}, {Root: rootA}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "desktop-projects.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	roots := readDesktopProjectRoots()
	if len(roots) != 2 {
		t.Fatalf("roots = %v, want 2 (dedup)", roots)
	}
}

func TestReadDesktopProjectRootsAbsent(t *testing.T) {
	_ = os.Remove(filepath.Join(config.ReasonixHomeDir(), "desktop-projects.json"))
	roots := readDesktopProjectRoots()
	if roots == nil || len(roots) != 0 {
		t.Fatalf("roots = %v, want empty slice", roots)
	}
}

func TestServePoolFlagRejectedForWeb(t *testing.T) {
	// `reasonix web --pool` must be rejected (pool is serve-only).
	code := runServeWithOptions([]string{"--pool"}, serveRunOptions{command: "web"})
	if code == 0 {
		t.Fatal("web --pool accepted; want nonzero exit")
	}
}
