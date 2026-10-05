package event

import "testing"

type recordSink struct{ got []Event }

func (r *recordSink) Emit(e Event) { r.got = append(r.got, e) }

const shellDiff = "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n"

func TestMarkEmbeddedDiffs(t *testing.T) {
	shell := &ShellExecution{Kind: "bash"}
	cases := []struct {
		name    string
		enabled bool
		ev      Event
		want    bool
	}{
		{"shell result that is a whole diff", true, Event{Kind: ToolResult, Tool: Tool{Execution: shell, Output: shellDiff}}, true},
		{"shell result that is prose", true, Event{Kind: ToolResult, Tool: Tool{Execution: shell, Output: "ok\n"}}, false},
		{"non-shell result with diff output", true, Event{Kind: ToolResult, Tool: Tool{Output: shellDiff}}, false},
		{"shell result that failed", true, Event{Kind: ToolResult, Tool: Tool{Execution: shell, Output: shellDiff, Err: "boom"}}, false},
		{"lossy bound is not tagged", true, Event{Kind: ToolResult, Tool: Tool{Execution: shell, Output: shellDiff, Bound: OutputBound{Kind: BoundTruncated, KeptBytes: 8}}}, false},
		{"spilled bound still tagged", true, Event{Kind: ToolResult, Tool: Tool{Execution: shell, Output: shellDiff, Bound: OutputBound{Kind: BoundSpilled, Path: "/tmp/x"}}}, true},
		{"dispatch is never tagged", true, Event{Kind: ToolDispatch, Tool: Tool{Execution: shell, Output: shellDiff}}, false},
		{"disabled is a pass-through", false, Event{Kind: ToolResult, Tool: Tool{Execution: shell, Output: shellDiff}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recordSink{}
			MarkEmbeddedDiffs(rec, tc.enabled).Emit(tc.ev)
			if len(rec.got) != 1 {
				t.Fatalf("emitted %d events, want 1", len(rec.got))
			}
			if got := rec.got[0].Tool.OutputDiff; got != tc.want {
				t.Fatalf("OutputDiff = %v, want %v", got, tc.want)
			}
		})
	}
}

// A nil sink is returned unchanged so a caller can wrap unconditionally.
func TestMarkEmbeddedDiffsNilSink(t *testing.T) {
	if MarkEmbeddedDiffs(nil, true) != nil {
		t.Fatal("nil sink did not pass through")
	}
}
