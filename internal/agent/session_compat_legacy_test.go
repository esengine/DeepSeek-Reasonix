package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// Legacy session JSONL compatibility gate for the extension-kernel work.
// Sessions written by older releases (messages without any of the newer
// fields — no reasoning signature, no images, no local_only, no event log)
// must load unchanged, and a save/load round-trip must preserve every
// message. The extension kernel migrates session paths across runtime
// rebuilds, so this contract is load-bearing.

const legacySessionFixture = `{"role":"system","content":"LEGACY SYSTEM PROMPT","createdAt":1690000000000}
{"role":"user","content":"first question","createdAt":1690000001000}
{"role":"assistant","content":"first answer","createdAt":1690000002000}
{"role":"user","content":"run something","createdAt":1690000003000}
{"role":"assistant","content":"running","tool_calls":[{"id":"call_abc","name":"bash","arguments":"{\"cmd\":\"ls\"}"}],"createdAt":1690000004000}
{"role":"tool","content":"file.txt","tool_call_id":"call_abc","name":"bash","createdAt":1690000005000}
{"role":"assistant","content":"done","createdAt":1690000006000}
`

func TestLegacySessionJSONLRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy-session.jsonl")
	if err := os.WriteFile(path, []byte(legacySessionFixture), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession legacy fixture: %v", err)
	}
	msgs := s.Snapshot()
	if len(msgs) != 7 {
		t.Fatalf("loaded %d messages, want 7", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[0].Content != "LEGACY SYSTEM PROMPT" {
		t.Fatalf("system message drifted: %+v", msgs[0])
	}
	if msgs[4].ToolCalls[0].ID != "call_abc" || msgs[4].ToolCalls[0].Name != "bash" {
		t.Fatalf("tool call drifted: %+v", msgs[4].ToolCalls)
	}
	if msgs[5].ToolCallID != "call_abc" || msgs[5].Name != "bash" {
		t.Fatalf("tool result drifted: %+v", msgs[5])
	}

	// Save through the current writer and reload: the legacy conversation
	// must survive a full round-trip with content intact.
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got := reloaded.Snapshot()
	if len(got) != len(msgs) {
		t.Fatalf("round-trip changed message count: %d -> %d", len(msgs), len(got))
	}
	for i := range msgs {
		if got[i].Role != msgs[i].Role || got[i].Content != msgs[i].Content ||
			got[i].ToolCallID != msgs[i].ToolCallID || got[i].Name != msgs[i].Name ||
			len(got[i].ToolCalls) != len(msgs[i].ToolCalls) {
			t.Fatalf("message %d drifted across round-trip:\nbefore: %+v\nafter:  %+v", i, msgs[i], got[i])
		}
	}
}
