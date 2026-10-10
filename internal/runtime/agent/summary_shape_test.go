package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

func readOnlyWorkSession() *sessionstore.Session {
	sess := &sessionstore.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
	}}
	for round := range 6 {
		id := string(rune('a' + round))
		sess.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: "read_file", Arguments: `{"path":"f.go"}`}}})
		sess.Add(provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "read_file", Content: strings.Repeat("package f // line\n", 200)})
	}
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
	return sess
}

// The summarizer is offered no tools, so an answer that is not a briefing under
// the required headings is the model carrying on the agent's work. A read-only
// fold has no changed file for the coverage guard to compare, which is why such
// an answer used to be installed as the digest.
func TestSummaryThatIsNotABriefingIsNeverInstalled(t *testing.T) {
	for name, reply := range map[string]string{
		"pseudo tool call": "<｜｜DSML｜｜ calls>\n<｜｜DSML｜｜ invoke name=\"read_file\">\n</｜｜DSML｜｜ invoke>",
		"prose":            "I'll add the doc comment to the next package.",
	} {
		t.Run(name, func(t *testing.T) {
			sess := readOnlyWorkSession()
			a := New(&fakeProvider{reply: reply, verbatim: true}, tool.NewRegistry(), sess,
				Options{ContextWindow: 20000, CompactRatio: 0.8, RecentKeep: 2, ArchiveDir: testenv.TempDir(t)}, event.Discard)
			outcome, _, err := a.window().compactToProjection(context.Background(), CompactionTriggerManual, "", compactionScope{ignoreThreshold: true, ignoreEconomics: true}, false)
			if outcome == CompactionInstalled {
				t.Fatalf("a reply that is not a briefing was installed as the digest: %v", visibleContext(a))
			}
			if got := CompactionFailureCode(err); got != FailSummaryNotDigest {
				t.Fatalf("failure code %q (err %v), want %q", got, err, FailSummaryNotDigest)
			}
			if !transientSummaryFailure(string(FailSummaryNotDigest)) {
				t.Fatal("a model answering off-task is variance, so a later attempt must be allowed")
			}
		})
	}
}

// Under hard pressure the fold has to free space, so the same reply degrades to
// the host's own statement that no summary exists instead of failing the turn.
func TestSummaryThatIsNotABriefingDegradesWhenTheFoldMustFree(t *testing.T) {
	sess := readOnlyWorkSession()
	a := New(&fakeProvider{reply: "<invoke name=\"read_file\"></invoke>", verbatim: true}, tool.NewRegistry(), sess,
		Options{ContextWindow: 20000, CompactRatio: 0.8, RecentKeep: 2, ArchiveDir: testenv.TempDir(t)}, event.Discard)
	outcome, _, err := a.window().compactToProjection(context.Background(), CompactionTriggerOverflow, "", compactionScope{ignoreThreshold: true, ignoreEconomics: true}, true)
	if err != nil || outcome != CompactionInstalled {
		t.Fatalf("outcome %v, err %v", outcome, err)
	}
	for _, m := range visibleContext(a) {
		if strings.Contains(m.Content, "invoke name") {
			t.Fatalf("the off-task reply reached the projection: %q", m.Content)
		}
	}
}

// The transcript is one user message that ends on the last tool output, which
// reads as a conversation waiting for its next move. The request has to end on
// what it wants.
func TestSummaryRequestEndsWithItsInstruction(t *testing.T) {
	sess := readOnlyWorkSession()
	prov := &fakeProvider{reply: "## Goal\nread the files"}
	a := New(prov, tool.NewRegistry(), sess,
		Options{ContextWindow: 20000, CompactRatio: 0.8, RecentKeep: 2, ArchiveDir: testenv.TempDir(t)}, event.Discard)
	if outcome, reason, err := a.window().compactToProjection(context.Background(), CompactionTriggerManual, "", compactionScope{ignoreThreshold: true, ignoreEconomics: true}, false); err != nil || outcome != CompactionInstalled {
		t.Fatalf("outcome %v, reason %q, err %v", outcome, reason, err)
	}
	last := prov.got[len(prov.got)-1]
	if last.Role != provider.RoleUser || !strings.HasSuffix(last.Content, summaryClosingInstruction) {
		t.Fatalf("summary request ends with %q, want the closing instruction", last.Content[max(0, len(last.Content)-200):])
	}
}

func TestAnyAtxHeadingIsTheDigestShape(t *testing.T) {
	for text, want := range map[string]bool{
		"# Goal\nx": true, "## Goal\nx": true, "###### x": true, "preamble\n### Files\n- a": true,
		"####### seven": false, "#nospace": false, "plain prose": false, "": false,
	} {
		if got := hasDigestHeading(text); got != want {
			t.Errorf("hasDigestHeading(%q) = %v, want %v", text, got, want)
		}
	}
}
