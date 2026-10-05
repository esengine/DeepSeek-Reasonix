package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
)

// A headless run never moves to another provider on its own: with a saved
// default nothing configured serves, it exits with the unknown-model error and
// sends nothing, even though a keyed provider is configured.
func TestRunWithStaleDefaultModelFailsWithoutSending(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"))
	}))
	t.Cleanup(srv.Close)
	isolateCLIConfigHome(t)
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("RUN_STALE_DEFAULT_KEY", "k")
	writeTestFile(t, filepath.Join(home, "config.toml"), fmt.Sprintf("default_model = \"deepseek-v4-flash\"\n\n[[providers]]\nname = \"other\"\nkind = \"openai\"\nbase_url = %q\nmodel = \"other-model\"\napi_key_env = \"RUN_STALE_DEFAULT_KEY\"\n", srv.URL), 0o644)
	t.Chdir(testenv.TempDir(t))

	var code int
	_, stderr := captureCLIOutput(t, func() { code = Run([]string{"run", "hi"}, "test") })
	if code == 0 {
		t.Fatalf("run exited 0 on a stale default_model\n%s", stderr)
	}
	if !strings.Contains(stderr, `unknown model "deepseek-v4-flash"`) {
		t.Fatalf("stderr does not name the stale default:\n%s", stderr)
	}
	if n := requests.Load(); n != 0 {
		t.Fatalf("run sent %d request(s) to another provider", n)
	}
}
