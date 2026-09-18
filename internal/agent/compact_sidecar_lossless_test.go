package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
)

// TestSidecarWireBytesSurviveRoundTrip pins the lossless projection inverse:
// the frozen main-request bytes stored in the sidecar must survive the JSON
// round trip byte-exact, so a resumed process can replay the same prefix.
func TestSidecarWireBytesSurviveRoundTrip(t *testing.T) {
	wire := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "u1"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "t1", Name: "read", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "t1", Name: "read", Content: "result"},
		{Role: provider.RoleUser, Content: "u2"},
	}
	tools := []provider.ToolSchema{
		{Name: "search", Description: "search the web", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)},
	}
	st := CompactionState{LastWireMessages: wire, LastWireTools: tools}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back CompactionState
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.LastWireTools) != len(tools) {
		t.Fatalf("round-trip tools = %d, want %d", len(back.LastWireTools), len(tools))
	}
	for i := range tools {
		if back.LastWireTools[i].Name != tools[i].Name || back.LastWireTools[i].Description != tools[i].Description ||
			string(back.LastWireTools[i].Parameters) != string(tools[i].Parameters) {
			t.Fatalf("tool %d diverged after round trip: %+v vs %+v", i, back.LastWireTools[i], tools[i])
		}
	}
	if len(back.LastWireMessages) != len(wire) {
		t.Fatalf("round-trip messages = %d, want %d", len(back.LastWireMessages), len(wire))
	}
	for i := range wire {
		m, w := back.LastWireMessages[i], wire[i]
		if m.Role != w.Role || m.Content != w.Content || m.ToolCallID != w.ToolCallID || m.Name != w.Name {
			t.Fatalf("message %d diverged after round trip: %+v vs %+v", i, m, w)
		}
		if len(m.ToolCalls) != len(w.ToolCalls) {
			t.Fatalf("message %d tool calls diverged: %d vs %d", i, len(m.ToolCalls), len(w.ToolCalls))
		}
		for j := range w.ToolCalls {
			if m.ToolCalls[j].ID != w.ToolCalls[j].ID || m.ToolCalls[j].Name != w.ToolCalls[j].Name || m.ToolCalls[j].Arguments != w.ToolCalls[j].Arguments {
				t.Fatalf("message %d tool call %d diverged", i, j)
			}
		}
	}
}

// TestLoadProjectionSidecarRestoresWireBytes simulates a resume: a sidecar
// written by a parent process (with LastWireMessages) is loaded by a fresh
// agent, and the frozen prefix is restored so the first post-resume compaction
// replays the provider-cached unit instead of falling back to system-only.
func TestLoadProjectionSidecarRestoresWireBytes(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	wire := []provider.Message{
		{Role: provider.RoleSystem, Content: "system prompt"},
		{Role: provider.RoleUser, Content: "user one"},
		{Role: provider.RoleAssistant, Content: "assistant one"},
		{Role: provider.RoleUser, Content: "user two"},
	}
	frozenTools := []provider.ToolSchema{
		{Name: "read", Description: "read a file", Parameters: json.RawMessage(`{"type":"object"}`)},
	}
	canonical := append([]provider.Message(nil), wire...)
	covered := len(canonical)
	st := CompactionState{
		SchemaVersion:    compactionStateSchemaCurrent,
		LastWireMessages: wire,
		LastWireTools:    frozenTools,
		LastReceipt:      &ContextMaintenanceReceipt{Status: "applied"},
		PromptCacheKey:   "lineage",
		Projection: ContextProjection{
			Messages: []provider.Message{
				{Role: provider.RoleSystem, Content: "system prompt"},
				{Role: provider.RoleUser, Content: "SUMMARY: everything was folded"},
			},
			CoveredCount:      covered,
			CoveredPrefixHash: coveredPrefixHash(canonical, covered),
		},
	}
	if err := SaveCompactionState(sessionPath, st); err != nil {
		t.Fatalf("save sidecar: %v", err)
	}

	// Fresh process: new agent, no frozen bytes.
	prov := &countingProvider{reply: "digest"}
	a := newFoldAgent(t, 200000, prov)
	if got := a.savedMainRequest(); got != nil {
		t.Fatalf("fresh agent must start without frozen bytes, got %d messages", len(got.messages))
	}
	a.sess.conversation = &Session{Messages: canonical}
	a.LoadProjectionSidecar(sessionPath)

	restored := a.savedMainRequest()
	if restored == nil {
		t.Fatal("restored wire bytes missing")
	}
	if len(restored.messages) != len(wire) {
		t.Fatalf("restored wire bytes = %d messages, want %d", len(restored.messages), len(wire))
	}
	for i := range wire {
		if restored.messages[i].Role != wire[i].Role || restored.messages[i].Content != wire[i].Content {
			t.Fatalf("restored message %d diverged: %+v vs %+v", i, restored.messages[i], wire[i])
		}
	}
	if len(restored.tools) != len(frozenTools) {
		t.Fatalf("restored tools = %d, want %d", len(restored.tools), len(frozenTools))
	}
	for i := range frozenTools {
		if restored.tools[i].Name != frozenTools[i].Name || string(restored.tools[i].Parameters) != string(frozenTools[i].Parameters) {
			t.Fatalf("restored tool %d diverged: %+v vs %+v", i, restored.tools[i], frozenTools[i])
		}
	}
	// Compaction telemetry compares wire_fp against the summary prefix hash;
	// restoring the bytes without the fingerprint emits an empty wire_fp and
	// silently loses that comparison.
	if got, want := a.sess.wireFP(), providerVisibleFingerprint(wire); got != want || got == "" {
		t.Fatalf("restored wire fingerprint = %q, want %q", got, want)
	}

	// First post-resume compaction must replay the restored bytes, not the
	// cropped fallback.
	prefix, extra, anchors := a.summaryFoldPlan(canonical, 1, len(canonical)-1)
	if len(prefix) != len(wire) {
		t.Fatalf("first post-resume prefix = %d messages, want the restored %d", len(prefix), len(wire))
	}
	for i := range wire {
		if prefix[i].Content != wire[i].Content {
			t.Fatalf("prefix message %d = %q, want restored %q", i, prefix[i].Content, wire[i].Content)
		}
	}
	if len(extra) != 0 || anchors == "" {
		t.Fatalf("extra=%d anchors=%q, want fold located by anchors, not re-sent", len(extra), anchors)
	}
}

// TestLoadProjectionSidecarWithoutWireBytesFallsBack covers old sidecars that
// predate the lossless field: no restore, cropped fallback stays.
func TestLoadProjectionSidecarWithoutWireBytesFallsBack(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	canonical := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "u1"},
		{Role: provider.RoleAssistant, Content: "a1"},
		{Role: provider.RoleUser, Content: "u2"},
	}
	st := CompactionState{
		SchemaVersion:  compactionStateSchemaCurrent,
		LastReceipt:    &ContextMaintenanceReceipt{Status: "applied"},
		PromptCacheKey: "lineage",
		Projection: ContextProjection{
			Messages: []provider.Message{
				{Role: provider.RoleSystem, Content: "system"},
				{Role: provider.RoleUser, Content: "SUMMARY"},
			},
			CoveredCount:      3,
			CoveredPrefixHash: coveredPrefixHash(canonical, 3),
		},
	}
	if err := SaveCompactionState(sessionPath, st); err != nil {
		t.Fatalf("save sidecar: %v", err)
	}
	a := newFoldAgent(t, 200000, &countingProvider{reply: "digest"})
	a.sess.conversation = &Session{Messages: canonical}
	a.LoadProjectionSidecar(sessionPath)
	if got := a.savedMainRequest(); got != nil {
		t.Fatalf("legacy sidecar must not restore bytes, got %d messages", len(got.messages))
	}
	os.Remove(sessionPath)
}

// TestSidecarPrettyPrintRoundTripPreservesRawBytes covers the second half of
// the byte-exact inverse: the sidecar writer pretty-prints the outer
// structure, which would rewrite json.RawMessage bytes (tool parameters,
// Responses items) inside the frozen wire form and diverge from the bytes the
// main request sent. Save/Load must compact those fields so the resumed
// process replays the provider-cached unit byte-exact (2026-08-31).
func TestSidecarPrettyPrintRoundTripPreservesRawBytes(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	st := CompactionState{
		SchemaVersion: compactionStateSchemaCurrent,
		LastWireMessages: []provider.Message{
			{Role: provider.RoleSystem, Content: "s"},
			{Role: provider.RoleAssistant, ResponsesItems: []json.RawMessage{json.RawMessage(`{"type":"reasoning","id":"r1"}`)}},
			{Role: provider.RoleUser, ServerSearch: []provider.ServerSearchCall{{ID: "s1", Raw: json.RawMessage(`{"titles":["a"]}`)}}},
		},
		LastWireTools: []provider.ToolSchema{
			{Name: "read", Description: "read a file", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
	}
	if err := SaveCompactionState(sessionPath, st); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, ok, err := LoadCompactionState(sessionPath)
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if len(loaded.LastWireTools) != 1 || string(loaded.LastWireTools[0].Parameters) != `{"type":"object"}` {
		t.Fatalf("tool parameters not byte-preserved: %q", loaded.LastWireTools[0].Parameters)
	}
	if string(loaded.LastWireMessages[1].ResponsesItems[0]) != `{"type":"reasoning","id":"r1"}` {
		t.Fatalf("responses item not byte-preserved: %q", loaded.LastWireMessages[1].ResponsesItems[0])
	}
	if string(loaded.LastWireMessages[2].ServerSearch[0].Raw) != `{"titles":["a"]}` {
		t.Fatalf("server-search raw not byte-preserved: %q", loaded.LastWireMessages[2].ServerSearch[0].Raw)
	}
	os.Remove(sessionPath)
}
