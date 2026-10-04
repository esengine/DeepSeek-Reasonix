package termrender

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"reasonix/internal/contract/event"
)

// BenchmarkDiffBlockPreview prices the writer-tool preview against the shape it
// replaced. Both arms draw the same rows (TestDiffBodyFoldSeamSameRows); the
// fold only decides whether the tail is laid out. "fold-on" colourises the kept
// rows, "fold-off" highlights the whole body and drops the tail after. fold-on
// is flat in the diff size, fold-off grows with it.
func BenchmarkDiffBlockPreview(b *testing.B) {
	defer func(prev colorprofile.Profile) { activeColorProfile = prev }(activeColorProfile)
	activeColorProfile = colorprofile.ANSI256
	defer func(prev bool) { diffPreviewFold = prev }(diffPreviewFold)

	for _, n := range []int{24, 199, 2000} {
		d := benchPreviewDiff(n)
		b.Run(fmt.Sprintf("lines-%d", n), func(b *testing.B) {
			for _, arm := range []struct {
				name string
				fold bool
			}{
				{"fold-on", true},
				{"fold-off", false},
			} {
				b.Run(arm.name, func(b *testing.B) {
					diffPreviewFold = arm.fold
					b.ReportAllocs()
					b.ResetTimer()
					for range b.N {
						_ = DiffBlock("write_file", `{"path":"db/maestro/simulatecontrol.js"}`, d, 120, 24)
					}
				})
			}
		})
	}
}

// benchPreviewDiff builds a one-hunk write of n added JS lines, the shape a
// writer call previews.
func benchPreviewDiff(n int) event.FileDiff {
	var sb strings.Builder
	sb.WriteString("--- a/db/maestro/simulatecontrol.js\n+++ b/db/maestro/simulatecontrol.js\n")
	fmt.Fprintf(&sb, "@@ -1,0 +1,%d @@\n", n)
	for i := range n {
		fmt.Fprintf(&sb, "+  const x%d = producer(%d); // a reasonably long javascript line here\n", i, i)
	}
	return event.FileDiff{Diff: sb.String(), Added: n}
}
