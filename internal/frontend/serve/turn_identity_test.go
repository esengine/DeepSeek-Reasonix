package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
)

type turnIdentityProvider struct{}

func (p *turnIdentityProvider) Name() string { return "turn-identity" }

func (p *turnIdentityProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "answered"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// TestCheckpointAndHistoryNameTheSameMessage is the join the desktop makes,
// taken at the boundary it reads: a rewind entry is offered for a row because
// the snapshot and the row name one message, not because they sit at the same
// place or carry the same words.
func TestCheckpointAndHistoryNameTheSameMessage(t *testing.T) {
	dir := testenv.TempDir(t)
	sess := sessionstore.NewSession("sys")
	exec := agent.New(&turnIdentityProvider{}, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	done := make(chan struct{}, 4)
	ctrl := control.New(control.Options{
		Runner:      exec,
		Executor:    exec,
		Sink:        event.FuncSink(func(e event.Event) { signalTurnDone(e, done) }),
		SessionDir:  dir,
		SessionPath: filepath.Join(dir, "s.jsonl"),
	})
	defer ctrl.Close()
	s := &Server{ctrl: ctrl}

	prompts := []string{"同一句话", "同一句话"}
	for _, prompt := range prompts {
		ctrl.Send(prompt)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("turn %q never finished", prompt)
		}
	}

	var record []struct {
		Role     string `json:"role"`
		Content  string `json:"content"`
		MsgIndex int    `json:"msgIndex"`
	}
	decodeGet(t, s.history, "/history", &record)
	var snapshots []struct {
		Turn     int    `json:"turn"`
		Prompt   string `json:"prompt"`
		MsgIndex int    `json:"msgIndex"`
	}
	decodeGet(t, s.checkpoints, "/checkpoints", &snapshots)

	if len(snapshots) != len(prompts) {
		t.Fatalf("checkpoints = %d, want %d", len(snapshots), len(prompts))
	}
	seen := map[int]bool{}
	for _, cp := range snapshots {
		if cp.MsgIndex < 0 || cp.MsgIndex >= len(record) {
			t.Fatalf("checkpoint %d names index %d, outside a record of %d", cp.Turn, cp.MsgIndex, len(record))
		}
		if seen[cp.MsgIndex] {
			t.Fatalf("two checkpoints name message %d; identical prompts must not share one", cp.MsgIndex)
		}
		seen[cp.MsgIndex] = true
		named := record[cp.MsgIndex]
		if named.Role != "user" || named.MsgIndex != cp.MsgIndex {
			t.Fatalf("checkpoint %d names %+v, want the user message at %d", cp.Turn, named, cp.MsgIndex)
		}
		// The text is the check, never the join: it says the index landed on
		// the message the snapshot was taken for.
		if named.Content != cp.Prompt {
			t.Fatalf("checkpoint %d prompt %q, message %q", cp.Turn, cp.Prompt, named.Content)
		}
	}
}

func signalTurnDone(e event.Event, done chan struct{}) {
	if e.Kind == event.TurnDone {
		done <- struct{}{}
	}
}

func decodeGet(t *testing.T, handler http.HandlerFunc, path string, into any) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		t.Fatalf("decode %s: %v (%s)", path, err, rec.Body.String())
	}
}
