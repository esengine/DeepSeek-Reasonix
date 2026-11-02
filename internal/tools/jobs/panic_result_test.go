package jobs

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestJobNilPanicCannotPublishSuccess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"0", "1"} {
		t.Run("panicnil="+mode, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestJobNilPanicChild$", "-test.count=1", "-test.v")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "GODEBUG=") && !strings.HasPrefix(entry, "REASONIX_JOB_NIL_PANIC=") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env, "GODEBUG=panicnil="+mode, "REASONIX_JOB_NIL_PANIC=1")
			out, err := cmd.CombinedOutput()
			t.Logf("explicit panicnil=%s child:\n%s", mode, out)
			if err != nil {
				t.Errorf("compiled job probe failed: %v", err)
			}
		})
	}
}
