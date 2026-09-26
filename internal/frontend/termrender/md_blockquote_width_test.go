package termrender

import (
	"strings"
	"testing"
)

func TestBlockquoteRowsFitTheRenderWidth(t *testing.T) {
	quote := "结论：月娘算是一个\"好人\"，但她的好是被动的、无力的。她没害人，但她也没救人。她的\"好\"更像是一种性格上的消极，而不是道德上的主动选择。"
	cases := map[string]string{
		"top level":     "> " + quote,
		"inside a list": "- item\n\n  > " + quote,
		"nested quote":  "> > " + quote,
		"latin words":   "> " + strings.Repeat("wrapping words ", 20),
	}
	for name, input := range cases {
		for _, width := range []int{20, 41, 80} {
			out := NewMarkdownRenderer(width).Render(input)
			for row := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
				if w := VisibleWidth(row); w > width {
					t.Errorf("%s at width %d: row is %d columns: %q", name, width, w, row)
				}
			}
		}
	}
}
