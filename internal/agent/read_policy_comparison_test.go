package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// comparisonWriter is a declarer-only writer: it records executions so the
// comparison can count how many writes the host actually let through.
type comparisonWriter struct {
	path       string
	executions int
}

func (w *comparisonWriter) Name() string            { return "write_file" }
func (w *comparisonWriter) Description() string     { return "comparison writer" }
func (w *comparisonWriter) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (w *comparisonWriter) ReadOnly() bool          { return false }

func (w *comparisonWriter) Execute(context.Context, json.RawMessage) (string, error) {
	w.executions++
	return "written", nil
}

func (w *comparisonWriter) DeclareEvidenceTarget(context.Context, json.RawMessage) (tool.EvidenceTargetInfo, error) {
	return tool.EvidenceTargetInfo{Path: w.path, WholeFile: true}, nil
}

// policyRun is one measured run of a fixed task under one policy.
type policyRun struct {
	rounds           int
	reads            int
	hostInstructions int
	writes           int
	blocked          bool
}

// conversationMetrics counts what the host actually demanded and the model
// actually called. It reads the stored transcript because ModelMessages strips
// the host origin from provider requests.
func conversationMetrics(msgs []provider.Message) (reads, hostInstructions int) {
	for _, msg := range msgs {
		if msg.Origin == provider.MessageOriginHost {
			hostInstructions++
		}
		for _, call := range msg.ToolCalls {
			if call.Name == "read_file" {
				reads++
			}
		}
	}
	return reads, hostInstructions
}

// runPreviewTask scripts "preview a large file, then answer" and measures it.
// The script carries the pages a legacy implicit-full read would force, so a
// policy that does not demand them simply stops earlier.
func runPreviewTask(t *testing.T, legacy bool) policyRun {
	t.Helper()
	path := makeShortPagedReadFixture(t, "comparison-large.txt", 2105, 2050, "COMPARISON_TAIL")
	read := incompleteReadBuiltin(t)
	prov := &scriptedProvider{name: "preview", turns: [][]provider.Chunk{
		{toolCallChunk("preview-1", "read_file", fmt.Sprintf(`{"path":%q}`, path)), {Type: provider.ChunkDone}},
		{toolCallChunk("preview-2", "read_file", fmt.Sprintf(`{"path":%q,"offset":2000,"limit":2000}`, path)), {Type: provider.ChunkDone}},
		textTurn("Previewed the file."),
	}}
	opts := Options{ContextWindow: 64_000}
	opts.ReadPipeline.LegacyImplicitFullReads = legacy
	a := newIncompleteReadTestAgentWithOptions(prov, read, NewSession("sys"), event.Discard, opts)
	if err := a.Run(context.Background(), "Show me the top of that file."); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reads, hostInstructions := conversationMetrics(a.sess.conversation.Snapshot())
	return policyRun{rounds: len(prov.requests), reads: reads, hostInstructions: hostInstructions}
}

// runOverwriteTask scripts "overwrite an existing file, then answer" and
// measures whether the host let the write through.
func runOverwriteTask(t *testing.T, legacy bool) policyRun {
	t.Helper()
	path := makeShortPagedReadFixture(t, "comparison-overwrite.txt", 12, 0, "")
	read := incompleteReadBuiltin(t)
	writer := &comparisonWriter{path: path}
	prov := &scriptedProvider{name: "overwrite", turns: [][]provider.Chunk{
		{toolCallChunk("write-1", "write_file", fmt.Sprintf(`{"path":%q,"content":"new\n"}`, path)), {Type: provider.ChunkDone}},
		textTurn("Wrote the file."),
	}}
	opts := Options{ContextWindow: 64_000}
	opts.ReadPipeline.LegacyEvidenceGates = legacy
	a := newIncompleteReadTestAgentWithOptions(prov, read, NewSession("sys"), event.Discard, opts, writer)
	if err := a.Run(context.Background(), "Rewrite that file from scratch."); err != nil {
		t.Fatalf("Run: %v", err)
	}
	blocked := false
	for _, msg := range a.sess.conversation.Snapshot() {
		if strings.Contains(msg.Content, "evidence required") {
			blocked = true
		}
	}
	return policyRun{rounds: len(prov.requests), writes: writer.executions, blocked: blocked}
}

// TestFixedTaskSetComparesReadPolicies is the deterministic half of the policy
// comparison: the same fixed tasks run under the new default and under the
// legacy rollback switches, and the recorded metrics must show the intended
// difference. It needs no live provider and no network.
func TestFixedTaskSetComparesReadPolicies(t *testing.T) {
	newPreview := runPreviewTask(t, false)
	oldPreview := runPreviewTask(t, true)
	t.Logf("preview task: new rounds=%d reads=%d hostInstructions=%d | legacy rounds=%d reads=%d hostInstructions=%d",
		newPreview.rounds, newPreview.reads, newPreview.hostInstructions,
		oldPreview.rounds, oldPreview.reads, oldPreview.hostInstructions)
	if newPreview.hostInstructions != 0 {
		t.Fatalf("a bounded preview must not demand continuation: %+v", newPreview)
	}
	if oldPreview.hostInstructions == 0 {
		t.Fatalf("the legacy rollback must still demand the whole file: %+v", oldPreview)
	}

	newOverwrite := runOverwriteTask(t, false)
	oldOverwrite := runOverwriteTask(t, true)
	t.Logf("overwrite task: new writes=%d blocked=%v | legacy writes=%d blocked=%v",
		newOverwrite.writes, newOverwrite.blocked, oldOverwrite.writes, oldOverwrite.blocked)
	if newOverwrite.writes != 0 || !newOverwrite.blocked {
		t.Fatalf("an unread overwrite must be blocked by default: %+v", newOverwrite)
	}
	if oldOverwrite.writes != 1 || oldOverwrite.blocked {
		t.Fatalf("the legacy rollback must still allow the overwrite: %+v", oldOverwrite)
	}
}
