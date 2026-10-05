package cli

import (
	"os"
	"strings"
	"testing"
)

// 1.x prints setup usage for -h/--help (exit 0) and refuses any other dashed
// argument (exit 2); none of them may become the path the config is written to.
// The ACP registry advertises `reasonix setup` as the login method.
func TestSetupHelpAndUnknownFlagsMatch1x(t *testing.T) {
	cases := []struct {
		arg      string
		wantCode int
		stream   string
	}{
		{"--help", 0, "stdout"},
		{"-h", 0, "stdout"},
		{"--not-a-flag", 2, "stderr"},
	}
	for _, tc := range cases {
		t.Run(tc.arg, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			var code int
			stdout, stderr := captureCLIOutput(t, func() {
				code = Run([]string{"setup", tc.arg}, "test")
			})
			if code != tc.wantCode {
				t.Fatalf("reasonix setup %s exited %d, want %d\nstdout:\n%s\nstderr:\n%s", tc.arg, code, tc.wantCode, stdout, stderr)
			}
			out := map[string]string{"stdout": stdout, "stderr": stderr}[tc.stream]
			if !strings.Contains(out, "usage: reasonix setup [--local|-l] [path]") {
				t.Fatalf("usage missing from %s:\n%s", tc.stream, out)
			}
			if strings.Contains(stdout+stderr, "Wrote") {
				t.Fatalf("setup %s reported writing a file:\n%s%s", tc.arg, stdout, stderr)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				t.Fatalf("setup %s left %q in the working directory", tc.arg, e.Name())
			}
		})
	}
}
