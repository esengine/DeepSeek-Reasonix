package boot

import (
	"sync"
	"testing"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/event"
	"reasonix/internal/state/agentpreset"
)

// A release that rewrites a built-in tool's description changes the schema
// digest a bound conversation recorded. The conversation must go on with the
// tools it recorded and say so once, not turn into history.
func TestCompositionContinuesWhenToolSchemasChangedSinceItWasBound(t *testing.T) {
	home, root := compositionTestWorkspace(t, "openai")
	store := agentpreset.New(home, root)
	d := agentcomposition.Builtins()[0]
	d.ID = "user:drift"
	if _, err := store.Save(d, ""); err != nil {
		t.Fatal(err)
	}
	opts := Options{Home: home, WorkspaceRoot: root, Model: "test-model", AgentComposition: d.ID, resolvedShell: pinnedEffectShell()}
	first, err := buildSnapshotRuntime(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Controller.Close()
	older := *first.Controller.CompositionBinding()
	older.ToolSchemaDigest = "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	older.Seal()
	if err := store.RetainBinding(&older); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var codes []string
	opts.AgentComposition, opts.Composition = "", &older
	opts.Sink = event.FuncSink(func(e event.Event) {
		mu.Lock()
		defer mu.Unlock()
		codes = append(codes, e.Code)
	})
	restored, err := buildSnapshotRuntime(t.Context(), opts)
	if err != nil {
		t.Fatalf("a bound conversation was refused after a tool schema changed: %v", err)
	}
	defer restored.Controller.Close()
	if restored.Controller.CompositionBinding().Digest != older.Digest {
		t.Fatal("the recorded binding must be kept as it was")
	}
	if err := restored.Controller.CompositionExecutionError(); err != nil {
		t.Fatalf("the conversation cannot run: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	n := 0
	for _, code := range codes {
		if code == event.NoticeCodeCompositionToolsChanged {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the change was said %d times, want once (codes %v)", n, codes)
	}
}
