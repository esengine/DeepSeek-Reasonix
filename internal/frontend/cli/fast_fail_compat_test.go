package cli

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// 1.x prints usage and exits 0 for a bare `reasonix` with no terminal; session
// options with nothing to apply them to stay a usage error.
func TestBareReasonixWithoutTerminalExitsZero(t *testing.T) {
	var code int
	stdout, _ := captureCLIOutput(t, func() { code = Run(nil, "test") })
	if code != 0 || !strings.Contains(stdout, "reasonix") {
		t.Fatalf("bare reasonix exited %d, want 0 with usage:\n%s", code, stdout)
	}
	captureCLIOutput(t, func() { code = Run([]string{"--model", "m"}, "test") })
	if code != 2 {
		t.Fatalf("session options without a terminal exited %d, want 2", code)
	}
}

// 1.x failed a run against an unreachable model service in well under a
// second; retrying a refused connection made it take about 92 seconds.
func TestRunFailsFastWhenTheModelServiceRefuses(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	isolateCLIConfigHome(t)
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("RUN_FLAGS_FAKE_KEY", "k")
	writeTestFile(t, filepath.Join(home, "config.toml"), fmt.Sprintf("default_model = \"fake\"\n\n[[providers]]\nname = \"fake\"\nkind = \"openai\"\nbase_url = %q\nmodel = \"fake-model\"\napi_key_env = \"RUN_FLAGS_FAKE_KEY\"\n", "http://"+addr+"/v1"), 0o644)
	t.Chdir(testenv.TempDir(t))

	start := time.Now()
	var code int
	_, stderr := captureCLIOutput(t, func() { code = Run([]string{"run", "hi"}, "test") })
	if code != 1 {
		t.Fatalf("exit %d, want 1\nstderr:\n%s", code, stderr)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("run took %s to report an unreachable model service", elapsed)
	}
}
