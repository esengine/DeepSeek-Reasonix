package sessionstore

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

// dagLine is one schema 2 entry as 1.x writes it.
func dagLine(t *testing.T, fields map[string]any) string {
	t.Helper()
	fields["schema_version"] = 2
	if _, ok := fields["at"]; !ok {
		fields["at"] = "2026-09-25T13:00:00Z"
	}
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func dagMsg(t *testing.T, id, parent, head string, m map[string]any) string {
	return dagLine(t, map[string]any{"type": "message", "id": id, "parent": parent, "head": head, "msgs": []any{m}})
}

// writeDAGSession lays down a 1.x session: the schema 2 event log and the
// .jsonl checkpoint beside it.
func writeDAGSession(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "20260925-130000.000000000-deepseek-flash.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.SessionEventLog(path), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func contents(msgs []provider.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = string(m.Role) + ":" + m.Content
	}
	return out
}

func baseDAG(t *testing.T) []string {
	return []string{
		dagLine(t, map[string]any{"type": "log", "generation": 1}),
		dagLine(t, map[string]any{"type": "writer", "writer": "w1", "pid": 42}),
		dagMsg(t, "m1", "", "main", map[string]any{"role": "system", "content": "sys"}),
		dagMsg(t, "m2", "m1", "main", map[string]any{"role": "user", "content": "<session-context>", "origin": "host"}),
		dagMsg(t, "m3", "m2", "main", map[string]any{"role": "user", "content": "hello", "origin": "user"}),
		dagMsg(t, "m4", "m3", "main", map[string]any{"role": "assistant", "content": "hi"}),
		dagLine(t, map[string]any{"type": "turn_begin", "head": "main"}),
		dagLine(t, map[string]any{"type": "turn_end", "head": "main"}),
	}
}

// The selected head's chain is the transcript, and a message 1.x wrote as the
// host reads as host-authored here.
func TestDAGSessionOpensItsSelectedHead(t *testing.T) {
	path := writeDAGSession(t, baseDAG(t)...)
	if !IsForeignSessionLog(path) {
		t.Fatal("a schema 2 log is not recognised as 1.x's")
	}
	s, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	got := contents(s.Snapshot())
	want := []string{"system:sys", "user:<session-context>", "user:hello", "assistant:hi"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("transcript = %v, want %v", got, want)
	}
	if msgs := s.Snapshot(); !msgs[1].HostAuthored || msgs[2].HostAuthored {
		t.Fatalf("host origin not carried: %+v", msgs[1:3])
	}
	if preview, turns := SessionPreviewFromMessages(s.Snapshot()); preview != "hello" || turns != 1 {
		t.Fatalf("preview = %q, turns = %d", preview, turns)
	}
}

// Forks, selects, rewinds, patches, redactions and system overrides all change
// what the selected head reads.
func TestDAGMarkersShapeTheTranscript(t *testing.T) {
	lines := append(baseDAG(t),
		dagLine(t, map[string]any{"type": "fork", "head": "main", "new_head": "h2", "from": "m3"}),
		dagMsg(t, "m5", "m3", "h2", map[string]any{"role": "assistant", "content": "forked"}),
		dagLine(t, map[string]any{"type": "select", "head": "h2"}),
		dagLine(t, map[string]any{"type": "patch", "target": "m3", "msgs": []any{map[string]any{"role": "user", "content": "hello (patched)"}}}),
		dagLine(t, map[string]any{"type": "redact", "targets": map[string]any{"m5": []any{map[string]any{"role": "assistant", "content": "[redacted]"}}}}),
		dagLine(t, map[string]any{"type": "system", "head": "h2", "msgs": []any{map[string]any{"role": "system", "content": "sys2"}}}),
	)
	msgs, _, err := loadSessionDAGMessages(writeDAGSession(t, lines...), defaultSessionReplayLimits)
	if err != nil {
		t.Fatal(err)
	}
	want := "system:sys2|user:<session-context>|user:hello (patched)|assistant:[redacted]"
	if got := strings.Join(contents(msgs), "|"); got != want {
		t.Fatalf("transcript = %s, want %s", got, want)
	}

	rewound := append(baseDAG(t), dagLine(t, map[string]any{"type": "rewind", "head": "main", "to": "m3"}))
	msgs, _, err = loadSessionDAGMessages(writeDAGSession(t, rewound...), defaultSessionReplayLimits)
	if err != nil || contents(msgs)[len(msgs)-1] != "user:hello" {
		t.Fatalf("rewind not applied: %v %v", contents(msgs), err)
	}
}

// A torn last line leaves what came before it; an entry type this reader does
// not know refuses the log rather than misreading it.
func TestDAGReplayStopsAtDamageAndRefusesUnknownEntries(t *testing.T) {
	torn := writeDAGSession(t, append(baseDAG(t), `{"schema_version":2,"type":"message","id":"m9"`)...)
	msgs, damaged, err := loadSessionDAGMessages(torn, defaultSessionReplayLimits)
	if err != nil || !damaged || len(msgs) != 4 {
		t.Fatalf("torn tail: %d msgs, damaged=%v, err=%v", len(msgs), damaged, err)
	}
	unknown := writeDAGSession(t, append(baseDAG(t), dagLine(t, map[string]any{"type": "future_marker"}))...)
	if _, _, err := loadSessionDAGMessages(unknown, defaultSessionReplayLimits); err == nil {
		t.Fatal("an unknown entry type was read past")
	}
}

// fileSums hashes a session directory's files except the save lock, which is
// the coordination file both lines take under the same name.
func fileSums(t *testing.T, dir string) map[string][32]byte {
	t.Helper()
	sums := map[string][32]byte{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".lock") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err == nil {
			sums[e.Name()] = sha256.Sum256(b)
		}
	}
	return sums
}

// Saving never writes a 1.x session: the same transcript is reported
// unchanged, and anything more has to go to a session this build owns.
func TestDAGSessionSaveNeverWritesTheLog(t *testing.T) {
	path := writeDAGSession(t, baseDAG(t)...)
	before := fileSums(t, filepath.Dir(path))
	s, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSnapshot(path); !errors.Is(err, ErrSessionLogUnchanged) {
		t.Fatalf("unchanged save = %v, want ErrSessionLogUnchanged", err)
	}
	s.Add(provider.Message{Role: provider.RoleUser, Content: "more"})
	if err := s.SaveSnapshot(path); !errors.Is(err, ErrSessionLogForeign) {
		t.Fatalf("changed save = %v, want ErrSessionLogForeign", err)
	}
	after := fileSums(t, filepath.Dir(path))
	for name, sum := range before {
		if after[name] != sum {
			t.Errorf("%s changed", name)
		}
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			t.Errorf("save created %s", name)
		}
	}
}
