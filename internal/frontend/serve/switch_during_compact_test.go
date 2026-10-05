package serve

import (
	"context"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

// blockingSummarizer holds the compaction's summary call open until released,
// so the test can act while a manual fold is in flight.
type blockingSummarizer struct {
	started chan struct{}
	release chan struct{}
}

func (*blockingSummarizer) Name() string { return "blocking-summarizer" }

func (p *blockingSummarizer) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	select {
	case p.started <- struct{}{}:
	default:
	}
	ch := make(chan provider.Chunk, 2)
	go func() {
		defer close(ch)
		select {
		case <-p.release:
		case <-ctx.Done():
			return
		}
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "## Standing facts\n- never change the public API"}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}()
	return ch, nil
}

func foldableCompactSession() *sessionstore.Session {
	big := strings.Repeat("word ", 400)
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "standing constraint: never change the public API"},
	}
	for range 60 {
		msgs = append(msgs,
			provider.Message{Role: provider.RoleAssistant, Content: big},
			provider.Message{Role: provider.RoleUser, Content: "continue"})
	}
	return &sessionstore.Session{Messages: msgs}
}

// A model switch rebuilds the controller from a snapshot of its history. While
// a manual /compact is still folding, that snapshot is the pre-fold history and
// the fold lands on a controller nobody holds any more, so the switch refuses.
func TestSwitchModelRefusesWhileManualCompactIsFolding(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	dir := testenv.TempDir(t)

	prov := &blockingSummarizer{started: make(chan struct{}, 1), release: make(chan struct{})}
	exec := agent.New(prov, tool.NewRegistry(), foldableCompactSession(), agent.Options{
		ContextWindow: 200_000, CompactRatio: 0.8, RecentKeep: 2,
		ArchiveDir: testenv.TempDir(t),
	}, event.Discard)
	old := control.New(control.Options{Executor: exec, SessionDir: dir, Label: "old", Sink: event.Discard})

	built := false
	s := &Server{ctrl: old, bc: NewBroadcaster()}
	s.buildController = func(_ context.Context, _ string) (*control.Controller, error) {
		built = true
		return control.New(control.Options{
			Executor:   agent.New(nil, nil, sessionstore.NewSession("sys"), agent.Options{}, event.Discard),
			SessionDir: dir,
			Label:      "new",
		}), nil
	}

	compactDone := make(chan error, 1)
	go func() {
		_, err := old.Compact(context.Background(), agent.CompactRequest{})
		compactDone <- err
	}()
	select {
	case <-prov.started:
	case err := <-compactDone:
		t.Fatalf("compaction finished without calling the summarizer: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("compaction never reached the summarizer")
	}

	err := s.switchModel(context.Background(), "next-model")
	close(prov.release)
	if cerr := <-compactDone; cerr != nil {
		t.Fatalf("compact: %v", cerr)
	}

	if codedRefusal(err) != codeSwitchModel {
		t.Fatalf("switchModel during compaction = %v, want %s", err, codeSwitchModel)
	}
	if built || s.ctl() != control.SessionAPI(old) {
		t.Fatal("switchModel replaced the controller while its compaction was still folding")
	}
}
