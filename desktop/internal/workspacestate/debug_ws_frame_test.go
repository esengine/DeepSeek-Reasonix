package workspacestate

import (
	"strings"
	"testing"
)

// deepestConflictFrame walks the live stack for lifecycle.go / store.go frames.
// It exists in both build flavours (debug_ws_frame.go under the debugpprof tag,
// debug_ws_frame_noop.go otherwise), so this test runs untagged too. It must
// never panic, and every segment it does report has to be "file:line" so a
// logged conflict stays attributable to the guard that raised it.
func TestDeepestConflictFrameIsWellFormed(t *testing.T) {
	frame := deepestConflictFrame()
	if frame == "" {
		// No lifecycle.go/store.go frame on this stack is a valid result — the
		// no-op build always returns "".
		return
	}
	for _, part := range strings.Split(frame, "<-") {
		if part == "" {
			t.Fatalf("empty frame segment in %q", frame)
		}
		file, line, ok := strings.Cut(part, ":")
		if !ok || file == "" || line == "" {
			t.Fatalf("frame segment %q is not file:line", part)
		}
		for _, r := range line {
			if r < '0' || r > '9' {
				t.Fatalf("frame segment %q has a non-numeric line", part)
			}
		}
	}
}
