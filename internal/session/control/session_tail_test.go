package control

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

func TestForkSessionTailOpensWhatTheWholeHistoryCannot(t *testing.T) {
	c, _, _ := runTwoTurns(t)
	src := sessionstore.NewSessionPath(c.sessionDir, c.label)
	writeOversizedEventLog(t, src)
	if _, err := sessionstore.LoadSession(src); !errors.Is(err, sessionstore.ErrSessionReplayLimitExceeded) {
		t.Fatalf("LoadSession = %v, want the replay limit", err)
	}
	before := hashSessionFiles(t, src)

	path, dropped, err := c.ForkSessionTail(src)
	if err != nil {
		t.Fatal(err)
	}
	if dropped == 0 || path == src {
		t.Fatalf("path %q dropped %d, want a new session that left the old part out", path, dropped)
	}
	loaded, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatalf("the fork does not open: %v", err)
	}
	last := loaded.Messages[len(loaded.Messages)-1]
	if last.Content != "newest question" {
		t.Fatalf("last message = %.30q, want the newest kept", last.Content)
	}
	first := firstUser(loaded.Messages)
	if !strings.Contains(first, "earlier messages were too large to carry over") {
		t.Fatalf("the model is not told history was left out: %.120q", first)
	}
	meta, ok, err := sessionstore.LoadBranchMeta(path)
	if err != nil || !ok || meta.ParentID != sessionstore.BranchID(src) {
		t.Fatalf("meta = %+v ok=%v err=%v, want the oversized session as parent", meta, ok, err)
	}
	if after := hashSessionFiles(t, src); after != before {
		t.Fatal("reading the tail changed the oversized session's files")
	}
}

func firstUser(msgs []provider.Message) string {
	for _, m := range msgs {
		if m.Role == provider.RoleUser {
			return m.Content
		}
	}
	return ""
}

func hashSessionFiles(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	h := sha256.New()
	for _, p := range append([]string{path}, store.SessionSidecarFiles(path)...) {
		b, err := os.ReadFile(p)
		if err == nil {
			h.Write([]byte(p))
			h.Write(b)
		}
	}
	return [sha256.Size]byte(h.Sum(nil))
}

// writeOversizedEventLog is a native log whose message payload passes the
// replay bound by appends of 1 MiB each, the way a long session grows past it.
func writeOversizedEventLog(t *testing.T, sessionPath string) {
	t.Helper()
	f, err := os.Create(store.SessionEventLog(sessionPath))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	emit := func(rec map[string]any) {
		rec["schema_version"] = 1
		if err := enc.Encode(rec); err != nil {
			t.Fatal(err)
		}
	}
	emit(map[string]any{"type": "replace", "revision": 1, "messages": []any{map[string]any{"role": "system", "content": "system prompt"}}})
	body := strings.Repeat("x", 1<<20)
	index := 1
	for i := range 70 {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		emit(map[string]any{"type": "append", "revision": i + 2, "base_revision": i + 1, "message_index": index, "messages": []any{map[string]any{"role": role, "content": fmt.Sprintf("%02d %s", i, body)}}})
		index++
	}
	emit(map[string]any{"type": "append", "revision": 72, "base_revision": 71, "message_index": index, "messages": []any{map[string]any{"role": "user", "content": "newest question"}}})
}
