package serve

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"testing"
	"testing/fstest"
)

const initProbeEnv = "REASONIX_SERVE_INIT_PROBE"

func TestInitProbeHelper(t *testing.T) {
	if os.Getenv(initProbeEnv) == "" {
		t.Skip("helper for TestImportingServeStaysCheap")
	}
}

// Every command links this package, so its package initialisation is paid by
// `--version` and the TUI as much as by `serve`.
func TestImportingServeStaysCheap(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a process")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestInitProbeHelper$")
	cmd.Env = append(os.Environ(), initProbeEnv+"=1", "GODEBUG=inittrace=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("probe run: %v\n%s", err, out)
	}
	m := regexp.MustCompile(`init reasonix/internal/frontend/serve @[\d.]+ ms, ([\d.]+) ms clock`).FindSubmatch(out)
	if m == nil {
		t.Skipf("no init trace line for the package:\n%s", out)
	}
	ms, _ := strconv.ParseFloat(string(m[1]), 64)
	const budget = 15.0
	if ms > budget {
		t.Fatalf("package init took %.1f ms, budget %.0f ms: something at init reaches the OS (mime registry, files, network)", ms, budget)
	}
}

func TestPageServesTheManifestWithItsOwnType(t *testing.T) {
	page := fstest.MapFS{"app.webmanifest": {Data: []byte(`{}`)}}
	h := withPage(http.NotFoundHandler(), page)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app.webmanifest", nil))
	if got := rec.Header().Get("Content-Type"); got != "application/manifest+json" {
		t.Fatalf("Content-Type = %q, want application/manifest+json", got)
	}
}
