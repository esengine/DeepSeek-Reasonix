package boot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Assembly notices must not reach the transcript: a fact about which model was
// resolved is not a turn in the conversation the runtime is about to have. The
// audience is what says so, report is the only thing that stamps it, and an
// emitter that goes around report loses the stamp silently — the notice simply
// shows up in the flow, which is exactly the bug this replaced.
func TestAssemblyNoticesGoThroughReport(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no sources found: %v", err)
	}
	found := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || file == "report.go" {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for line := range strings.SplitSeq(string(src), "\n") {
			if !strings.Contains(line, "event.Notice") {
				continue
			}
			found++
			if strings.Contains(line, ".Emit(") {
				t.Errorf("%s emits a notice directly: %s", file, strings.TrimSpace(line))
			}
		}
	}
	if found == 0 {
		t.Fatal("no notice emission found at all — this test has stopped watching anything")
	}
}
