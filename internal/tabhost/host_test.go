package tabhost_test

import (
	"encoding/json"
	"os"
	"sync/atomic"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/tabhost"
)

// Tests inject nil SessionAPI; Host still tracks TabMeta and event stamping.

func TestDefaultBuilderConstructs(t *testing.T) {
	b := tabhost.DefaultBuilder(tabhost.BootBuilderOptions{RequireKey: false, StatsSource: "test"})
	if b == nil {
		t.Fatal("DefaultBuilder returned nil")
	}
}

func TestTabMetaFixtureJSONShape(t *testing.T) {
	raw, err := os.ReadFile("testdata/tabmeta_minimal.json")
	if err != nil {
		t.Fatal(err)
	}
	var m tabhost.TabMeta
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.ID == "" || m.Scope != "project" || !m.Ready || !m.Active {
		t.Fatalf("unexpected meta: %+v", m)
	}
	// Round-trip must keep camelCase keys used by the frontend.
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]any
	if err := json.Unmarshal(out, &probe); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"workspaceRoot", "topicId", "toolApprovalMode", "tokenMode"} {
		if _, ok := probe[key]; !ok {
			t.Fatalf("missing json key %q in %s", key, out)
		}
	}
}

func TestCreateListActivateClose(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	var builds atomic.Int32
	h := tabhost.New(func(opts tabhost.CreateTabOpts, sink event.Sink) (control.SessionAPI, error) {
		builds.Add(1)
		// Return nil controller: host still tracks tabs; Get will error.
		_ = sink
		_ = opts
		return nil, nil
	})
	defer h.CloseAll()

	a, err := h.CreateTab(tabhost.CreateTabOpts{Scope: tabhost.ScopeProject, WorkspaceRoot: dirA, Label: "A"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.CreateTab(tabhost.CreateTabOpts{Scope: tabhost.ScopeProject, WorkspaceRoot: dirB, Label: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if builds.Load() != 2 {
		t.Fatalf("builds=%d", builds.Load())
	}
	tabs := h.ListTabs()
	if len(tabs) != 2 {
		t.Fatalf("tabs=%d", len(tabs))
	}
	// Last created is active
	if !tabs[1].Active || tabs[0].Active {
		t.Fatalf("active flags: %+v %+v", tabs[0], tabs[1])
	}
	if err := h.SetActiveTab(a.ID); err != nil {
		t.Fatal(err)
	}
	tabs = h.ListTabs()
	var activeID string
	for _, tm := range tabs {
		if tm.Active {
			activeID = tm.ID
		}
	}
	if activeID != a.ID {
		t.Fatalf("active=%s want %s", activeID, a.ID)
	}
	if err := h.CloseTab(a.ID); err != nil {
		t.Fatal(err)
	}
	tabs = h.ListTabs()
	if len(tabs) != 1 || tabs[0].ID != b.ID {
		t.Fatalf("after close: %+v", tabs)
	}
}

func TestEventBusStampsTabID(t *testing.T) {
	h := tabhost.New(func(opts tabhost.CreateTabOpts, sink event.Sink) (control.SessionAPI, error) {
		// Immediately emit so subscribers see tab-stamped events.
		go func() {
			sink.Emit(event.Event{Kind: event.Text, Text: "hello-" + opts.WorkspaceRoot})
		}()
		return nil, nil
	})
	defer h.CloseAll()

	ch, unsub := h.Bus().Subscribe()
	defer unsub()

	dir := t.TempDir()
	meta, err := h.CreateTab(tabhost.CreateTabOpts{WorkspaceRoot: dir, Label: "x"})
	if err != nil {
		t.Fatal(err)
	}

	var got tabhost.WireEvent
	select {
	case raw := <-ch:
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
	default:
		// allow async emit
		raw := <-ch
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
	}
	if got.TabID != meta.ID {
		t.Fatalf("tabId=%q want %q", got.TabID, meta.ID)
	}
	if got.Kind != "text" {
		t.Fatalf("kind=%q", got.Kind)
	}
	if got.Text == "" {
		t.Fatal("expected text payload")
	}
}

func TestTwoTabsEmitDistinctTabIDs(t *testing.T) {
	h := tabhost.New(func(opts tabhost.CreateTabOpts, sink event.Sink) (control.SessionAPI, error) {
		sink.Emit(event.Event{Kind: event.Notice, Text: opts.Label})
		return nil, nil
	})
	defer h.CloseAll()
	ch, unsub := h.Bus().Subscribe()
	defer unsub()

	a, _ := h.CreateTab(tabhost.CreateTabOpts{WorkspaceRoot: t.TempDir(), Label: "A"})
	b, _ := h.CreateTab(tabhost.CreateTabOpts{WorkspaceRoot: t.TempDir(), Label: "B"})

	seen := map[string]string{}
	for len(seen) < 2 {
		raw := <-ch
		var w tabhost.WireEvent
		if err := json.Unmarshal(raw, &w); err != nil {
			t.Fatal(err)
		}
		seen[w.TabID] = w.Text
	}
	if seen[a.ID] != "A" || seen[b.ID] != "B" {
		t.Fatalf("seen=%v a=%s b=%s", seen, a.ID, b.ID)
	}
}

func TestCreateTabRequiresWorkspace(t *testing.T) {
	h := tabhost.New(func(tabhost.CreateTabOpts, event.Sink) (control.SessionAPI, error) {
		return nil, nil
	})
	_, err := h.CreateTab(tabhost.CreateTabOpts{Scope: tabhost.ScopeProject})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMaxTabs(t *testing.T) {
	h := tabhost.New(func(tabhost.CreateTabOpts, event.Sink) (control.SessionAPI, error) {
		return nil, nil
	}, tabhost.WithMaxTabs(1))
	defer h.CloseAll()
	if _, err := h.CreateTab(tabhost.CreateTabOpts{WorkspaceRoot: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.CreateTab(tabhost.CreateTabOpts{WorkspaceRoot: t.TempDir()}); err == nil {
		t.Fatal("expected limit error")
	}
}
