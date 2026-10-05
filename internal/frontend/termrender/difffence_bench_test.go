package termrender

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

// BenchmarkRenderStreamingDiffFence replays the workload the hunk-by-hunk path
// exists for: a multi-file ```diff fence whose deltas arrive one line at a time,
// so every line redraws the whole growing body. The two arms price the memo —
// "memo-off" re-highlights every settled hunk each frame, "memo-on" highlights
// each once — with the cache dropped per iteration so both start cold.
func BenchmarkRenderStreamingDiffFence(b *testing.B) {
	defer func(prev colorprofile.Profile) { activeColorProfile = prev }(activeColorProfile)
	activeColorProfile = colorprofile.ANSI256
	defer func(prev bool) { activeDiffFences = prev }(activeDiffFences)
	activeDiffFences = true
	restore := SetDiffFormatterForTest(nil)
	defer restore()
	SetDiffFormatNotify(nil)
	defer func(prev bool) { diffHunkMemo = prev }(diffHunkMemo)

	lines := streamingDiffLines(4, 15)
	for _, arm := range []struct {
		name string
		memo bool
	}{
		{"memo-on", true},
		{"memo-off", false},
	} {
		b.Run(arm.name, func(b *testing.B) {
			diffHunkMemo = arm.memo
			r := NewMarkdownRenderer(120)
			b.ResetTimer()
			for range b.N {
				resetBuiltinDiffCache()
				var sb strings.Builder
				sb.WriteString("```diff\n")
				for _, ln := range lines {
					sb.WriteString(ln)
					sb.WriteByte('\n')
					_ = r.Render(sb.String())
				}
			}
		})
	}
}

// resetBuiltinDiffCache drops the per-hunk memo so a benchmark iteration starts
// from an empty cache.
func resetBuiltinDiffCache() {
	builtinDiffMu.Lock()
	builtinDiffCache = map[builtinDiffKey][]string{}
	builtinDiffOrder = nil
	builtinDiffMu.Unlock()
}

// streamingDiffLines builds a diff of files×hunks line by line, the shape a
// model streams into a fenced block.
func streamingDiffLines(files, hunks int) []string {
	var lines []string
	for f := range files {
		lines = append(lines,
			fmt.Sprintf("--- a/f%d.go", f),
			fmt.Sprintf("+++ b/f%d.go", f),
		)
		for h := range hunks {
			lines = append(lines,
				fmt.Sprintf("@@ -%d,3 +%d,3 @@", h*10+1, h*10+1),
				" ctx",
				"-old",
				"+new",
			)
		}
	}
	return lines
}
