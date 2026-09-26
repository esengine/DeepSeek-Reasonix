package control

import (
	"context"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessioninbox"
)

// haltingText holds the first turn open until the test releases it, which is
// the window a person types into: the model is writing its last message, so
// guidance is still accepted and the loop will never take another step to read
// it. Later turns answer immediately and are recorded for inspection.
type haltingText struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once

	mu   sync.Mutex
	seen []provider.Request
}

func (h *haltingText) Name() string { return "halting" }

func (h *haltingText) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	h.mu.Lock()
	h.seen = append(h.seen, req)
	h.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	first := false
	h.once.Do(func() { first = true })
	if !first {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "answered"}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	}
	close(h.started)
	go func() {
		defer close(ch)
		<-h.release
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "the last message"}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}()
	return ch, nil
}

func (h *haltingText) requests() []provider.Request {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]provider.Request(nil), h.seen...)
}

func (h *haltingText) sawUserText(want string) bool {
	for _, req := range h.requests() {
		for _, m := range req.Messages {
			if m.Role == provider.RoleUser && strings.Contains(m.Content, want) {
				return true
			}
		}
	}
	return false
}

// A line typed while the model writes its final message is accepted with no
// tool round left to read it. closeSteerIntakeIfIdle keeps the loop alive for
// exactly that, and the guarantee is only worth anything end to end: through
// the durable inbox the text reaches a provider request, queue not left paused.
func TestSteerUnreadAtTurnEndIsDeliveredAsAFollowup(t *testing.T) {
	dir := testenv.TempDir(t)
	session := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prov := &haltingText{started: make(chan struct{}), release: make(chan struct{})}
	ag := agent.New(prov, tool.NewRegistry(), sessionstore.NewSession(""), agent.Options{}, event.Discard)
	c := New(Options{Runner: ag, Executor: ag, SessionPath: session, SessionDir: dir, Sink: event.Discard})
	defer c.autosaveWG.Wait()
	release := sync.OnceFunc(func() { close(prov.release) })
	defer release()

	c.Submit("write me something")
	select {
	case <-prov.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the first turn never reached the provider")
	}

	rec, err := c.EnqueueInbox(InboxRequest{Intent: sessioninbox.IntentSteer, Submit: "actually use plan B"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.TrySteerInboxItem(rec.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Disposition != sessioninbox.DispositionSteerAccepted {
		t.Fatalf("disposition = %s, want steer_accepted: the turn is still running", got.Disposition)
	}

	release()
	waitIdleAdmission(t, c)

	deadline := time.Now().Add(5 * time.Second)
	for !prov.sawUserText("actually use plan B") {
		if time.Now().After(deadline) {
			meta, _, readErr := c.ReadInboxItem(rec.ItemID)
			t.Fatalf("the line never reached the model: inbox state = %q (%v), paused = %v",
				meta.State, readErr, c.InboxSnapshot().Paused)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if snap := c.InboxSnapshot(); snap.Paused {
		t.Fatal("the queue was paused by a line it went on to deliver")
	}
}
