package serve

import "testing"

const historyDiffText = "diff --git a/x.go b/x.go\n" +
	"--- a/x.go\n" +
	"+++ b/x.go\n" +
	"@@ -1 +1 @@\n" +
	"-old\n" +
	"+new\n"

// A rebuilt transcript re-derives the whole-diff tag the live sink set, so a
// shell `git diff` renders as a diff after reopening, not as flat text.
func TestMarkHistoryDiffsRebuildsTheTag(t *testing.T) {
	msgs := []historyMessage{
		{Role: "tool", ToolName: "bash", Content: historyDiffText},
		{Role: "tool", ToolName: "bash", Content: "ok\n"},
		{Role: "tool", ToolName: "bash", Content: historyDiffText, ToolFailed: true},
		{Role: "tool", ToolName: "read_file", Content: historyDiffText},
		{Role: "assistant", Content: historyDiffText},
	}
	markHistoryDiffs(msgs, true)
	if !msgs[0].OutputDiff {
		t.Fatal("a rebuilt shell diff result was not tagged")
	}
	for _, i := range []int{1, 2, 3, 4} {
		if msgs[i].OutputDiff {
			t.Fatalf("message %d should not be tagged: %+v", i, msgs[i])
		}
	}
}

func TestMarkHistoryDiffsDisabled(t *testing.T) {
	msgs := []historyMessage{{Role: "tool", ToolName: "bash", Content: historyDiffText}}
	markHistoryDiffs(msgs, false)
	if msgs[0].OutputDiff {
		t.Fatal("detection disabled still tagged the result")
	}
}
