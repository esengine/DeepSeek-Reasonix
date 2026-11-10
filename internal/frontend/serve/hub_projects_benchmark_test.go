package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func BenchmarkProjectTree100Roots(b *testing.B) {
	home := testenv.TempDir(b)
	b.Setenv("REASONIX_HOME", home)
	b.Setenv("REASONIX_STATE_HOME", home)
	roots := make([]string, 100)
	for i := range roots {
		roots[i] = filepath.Join(home, fmt.Sprintf("project-%03d", i))
		if err := os.Mkdir(roots[i], 0o755); err != nil {
			b.Fatal(err)
		}
	}
	body, _ := json.Marshal(workspaceList{Paths: roots})
	if err := os.WriteFile(filepath.Join(config.MemoryUserDir(), "serve-workspaces.json"), body, 0o644); err != nil {
		b.Fatal(err)
	}
	h := NewHub(HubOptions{})
	read := func() {
		rec := httptest.NewRecorder()
		h.tree(rec, httptest.NewRequest(http.MethodGet, "/tree", nil))
		if rec.Code != http.StatusOK {
			b.Fatalf("tree: %d", rec.Code)
		}
	}
	start := time.Now()
	read()
	cold := time.Since(start)
	b.ReportAllocs()
	for b.Loop() {
		read()
	}
	b.ReportMetric(float64(cold.Nanoseconds())/1e6, "first-ms")
}
