package agent

import (
	"context"
	"strings"
	"testing"
)

// The delegate exists because something had to look at the picture. Left
// unsaid, it reads the path in its task and calls read_file — which reads text
// and refuses, so the whole delegation ends in an error about NUL bytes.
func TestSubagentIsToldTheImagesRideTheMessage(t *testing.T) {
	bare := SubagentImageNote(context.Background())
	if bare != "" {
		t.Fatalf("a task with no attachment must carry no note, got %q", bare)
	}

	ctx := WithSubagentImageCandidates(context.Background(), []string{"data:image/png;base64,AAA", "data:image/png;base64,BBB"})
	note := SubagentImageNote(ctx)
	if !strings.Contains(note, "2 image(s)") {
		t.Fatalf("note = %q, want the count", note)
	}
	for _, want := range []string{"attached to this message", "Never call read_file on an image", "hexdump"} {
		if !strings.Contains(note, want) {
			t.Fatalf("note = %q, want it to rule out %q", note, want)
		}
	}
}
