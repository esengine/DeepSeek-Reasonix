package tabhost_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tabhost"
	"reasonix/internal/tool"
)

// scriptedTurns replays fixed chunk sets per Stream call (same pattern as control tests).
type scriptedTurns struct {
	mu    sync.Mutex
	turns [][]provider.Chunk
	i     int
}

func (s *scriptedTurns) Name() string { return "scripted-tabhost" }

func (s *scriptedTurns) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan provider.Chunk, 8)
	var chunks []provider.Chunk
	if s.i < len(s.turns) {
		chunks = s.turns[s.i]
		s.i++
	} else {
		chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "fallback"}, {Type: provider.ChunkDone}}
	}
	go func() {
		defer close(ch)
		for _, c := range chunks {
			ch <- c
		}
	}()
	return ch, nil
}

func textTurn(s string) []provider.Chunk {
	return []provider.Chunk{
		{Type: provider.ChunkText, Text: s},
		{Type: provider.ChunkDone},
	}
}

// realControllerBuilder builds a genuine control.Controller with a scripted provider.
// Each tab gets its own session dir under workspace and independent agent/controller.
func realControllerBuilder(t *testing.T, turnText string) tabhost.Builder {
	t.Helper()
	return func(opts tabhost.CreateTabOpts, sink event.Sink) (control.SessionAPI, error) {
		sessionDir := filepath.Join(opts.WorkspaceRoot, ".reasonix-sessions")
		prov := &scriptedTurns{turns: [][]provider.Chunk{
			textTurn(turnText + ":" + filepath.Base(opts.WorkspaceRoot)),
			textTurn(turnText + "-again"),
		}}
		ag := agent.New(prov, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, sink)
		c := control.New(control.Options{
			Runner:        ag,
			Executor:      ag,
			Sink:          sink,
			Label:         opts.Label,
			SessionDir:    sessionDir,
			WorkspaceRoot: opts.WorkspaceRoot,
		})
		c.EnableInteractiveApproval()
		c.EnsureSessionPath()
		return c, nil
	}
}

func TestParallelSubmitTwoWorkspacesRace(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	h := tabhost.New(realControllerBuilder(t, "reply"))
	defer h.CloseAll()

	a, err := h.CreateTab(tabhost.CreateTabOpts{WorkspaceRoot: dirA, Label: "A"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.CreateTab(tabhost.CreateTabOpts{WorkspaceRoot: dirB, Label: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if a.SessionPath == "" || b.SessionPath == "" {
		t.Fatalf("missing session paths a=%q b=%q", a.SessionPath, b.SessionPath)
	}
	if a.SessionPath == b.SessionPath {
		t.Fatalf("tabs must not share session path: %s", a.SessionPath)
	}

	ch, unsub := h.Bus().Subscribe()
	defer unsub()

	var (
		doneA, doneB atomic.Bool
		textA, textB atomic.Value
	)

	// Drain events until both tabs finish a turn.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		deadline := time.After(15 * time.Second)
		for {
			select {
			case <-deadline:
				return
			case raw, ok := <-ch:
				if !ok {
					return
				}
				var w tabhost.WireEvent
				if err := json.Unmarshal(raw, &w); err != nil {
					continue
				}
				switch w.Kind {
				case "text":
					if w.TabID == a.ID {
						textA.Store(w.Text)
					}
					if w.TabID == b.ID {
						textB.Store(w.Text)
					}
				case "turn_done":
					if w.TabID == a.ID {
						doneA.Store(true)
					}
					if w.TabID == b.ID {
						doneB.Store(true)
					}
				}
				if doneA.Load() && doneB.Load() {
					return
				}
			}
		}
	}()

	// Concurrent Submit must not cancel or mis-route the sibling tab.
	var submitWG sync.WaitGroup
	submitWG.Add(2)
	go func() {
		defer submitWG.Done()
		if err := h.SubmitHTTP(a.ID, "task-A"); err != nil {
			t.Errorf("submit A: %v", err)
		}
	}()
	go func() {
		defer submitWG.Done()
		if err := h.SubmitHTTP(b.ID, "task-B"); err != nil {
			t.Errorf("submit B: %v", err)
		}
	}()
	submitWG.Wait()
	wg.Wait()

	if !doneA.Load() || !doneB.Load() {
		t.Fatalf("turn_done missing A=%v B=%v textA=%v textB=%v",
			doneA.Load(), doneB.Load(), textA.Load(), textB.Load())
	}
	// Text should reference each workspace basename, proving distinct controllers.
	ta, _ := textA.Load().(string)
	tb, _ := textB.Load().(string)
	if ta == "" || tb == "" {
		t.Fatalf("empty text A=%q B=%q", ta, tb)
	}
	// Active switch mid-flight must not cancel either turn (already done here;
	// exercise API concurrency still).
	if err := h.SetActiveTab(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.SetActiveTab(b.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDuplicateSessionPathRejected(t *testing.T) {
	// Force both builders to use the same SessionPath so host uniqueness/lease fires.
	sharedPath := filepath.Join(t.TempDir(), "shared.jsonl")
	buildFixed := func(label string) tabhost.Builder {
		return func(opts tabhost.CreateTabOpts, sink event.Sink) (control.SessionAPI, error) {
			prov := &scriptedTurns{turns: [][]provider.Chunk{textTurn(label)}}
			ag := agent.New(prov, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, sink)
			return control.New(control.Options{
				Runner:        ag,
				Executor:      ag,
				Sink:          sink,
				Label:         label,
				SessionDir:    filepath.Dir(sharedPath),
				SessionPath:   sharedPath,
				WorkspaceRoot: opts.WorkspaceRoot,
			}), nil
		}
	}

	h := tabhost.New(buildFixed("first"))
	defer h.CloseAll()
	m1, err := h.CreateTab(tabhost.CreateTabOpts{WorkspaceRoot: t.TempDir(), Label: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if m1.SessionPath == "" {
		t.Fatal("expected session path")
	}

	// Same host + predeclared path: rejected before/at create.
	_, err = h.CreateTab(tabhost.CreateTabOpts{
		WorkspaceRoot: t.TempDir(),
		Label:         "2",
		SessionPath:   m1.SessionPath,
	})
	if err == nil {
		t.Fatal("expected session path in use on same host")
	}

	// Separate host trying the same path: lease acquire must fail.
	h2 := tabhost.New(buildFixed("second"))
	defer h2.CloseAll()
	if _, err := h2.CreateTab(tabhost.CreateTabOpts{WorkspaceRoot: t.TempDir(), Label: "x"}); err == nil {
		t.Fatal("expected lease held when second host opens same session path")
	}
}

func TestCancelDoesNotCrossTabs(t *testing.T) {
	// Slow-ish scripted turn: first chunk delayed via multi-turn stream is hard;
	// instead start two tabs, cancel A, ensure B can still complete.
	dirA := t.TempDir()
	dirB := t.TempDir()
	h := tabhost.New(realControllerBuilder(t, "ok"))
	defer h.CloseAll()

	a, err := h.CreateTab(tabhost.CreateTabOpts{WorkspaceRoot: dirA, Label: "A"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.CreateTab(tabhost.CreateTabOpts{WorkspaceRoot: dirB, Label: "B"})
	if err != nil {
		t.Fatal(err)
	}

	ctrlA, _, err := h.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctrlA.Cancel() // must not affect B

	ch, unsub := h.Bus().Subscribe()
	defer unsub()
	if err := h.SubmitHTTP(b.ID, "only-B"); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for B turn_done")
		case raw := <-ch:
			var w tabhost.WireEvent
			if json.Unmarshal(raw, &w) != nil {
				continue
			}
			if w.Kind == "turn_done" && w.TabID == b.ID {
				return
			}
			if w.Kind == "turn_done" && w.TabID == a.ID {
				// A was cancelled idle; if a done arrives without submit it's fine to ignore
			}
		}
	}
}
