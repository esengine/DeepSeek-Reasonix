package cli

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func quietRunFixture(t *testing.T) *bytes.Buffer {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	isolateCLIConfigHome(t)
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("RUN_FLAGS_FAKE_KEY", "k")
	// 1.x also warns on stderr when the bash sandbox is enforced on a host that
	// has none (Linux without bwrap). That warning is the user's to act on, so
	// the sandbox is off here and the host's backend does not decide the test.
	writeTestFile(t, filepath.Join(home, "config.toml"), fmt.Sprintf("default_model = \"fake\"\n\n[sandbox]\nbash = \"off\"\n\n[[providers]]\nname = \"fake\"\nkind = \"openai\"\nbase_url = %q\nmodel = \"fake-model\"\napi_key_env = \"RUN_FLAGS_FAKE_KEY\"\n", srv.URL), 0o644)
	t.Chdir(testenv.TempDir(t))
	// The default slog handler writes through the log package, whose output
	// was bound to the real stderr at init; capture it there.
	var logs bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&logs)
	prevLevel := slog.SetLogLoggerLevel(slog.LevelInfo)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
		slog.SetLogLoggerLevel(prevLevel)
	})
	return &logs
}

// 1.x's `run` and `-p` leave stderr empty on a clean run, `-c` included:
// assembly timing, the prompt refiner's construction and the resume cache
// state are diagnostics, printed only under --debug.
func TestHeadlessRunKeepsStderrClean(t *testing.T) {
	logs := quietRunFixture(t)
	for _, argv := range [][]string{{"run", "hi"}, {"-p", "hi"}, {"run", "-c", "hi"}, {"-c", "-p", "hi"}} {
		var code int
		_, stderr := captureCLIOutput(t, func() { code = Run(argv, "test") })
		if code != 0 {
			t.Fatalf("Run(%q) exited %d\nstderr:\n%s", argv, code, stderr)
		}
		if all := strings.TrimSpace(stderr + logs.String()); all != "" {
			t.Fatalf("Run(%q) wrote to stderr, 1.x writes nothing:\n%s", argv, all)
		}
	}

	var code int
	captureCLIOutput(t, func() { code = Run([]string{"run", "--debug", "-c", "hi"}, "test") })
	for _, want := range []string{"boot: assembly timing", "controller: resume cache state"} {
		if code != 0 || !strings.Contains(logs.String(), want) {
			t.Fatalf("run --debug exited %d and did not log %q:\n%s", code, want, logs.String())
		}
	}
}
