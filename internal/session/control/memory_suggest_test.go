package control

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/memory"
)

func suggestSession(t *testing.T, proactive string) (*Controller, memory.Store) {
	t.Helper()
	root := testenv.TempDir(t)
	cwd := filepath.Join(root, "project")
	userDir := filepath.Join(root, "user")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	c := New(Options{Memory: memory.Load(memory.Options{CWD: cwd, UserDir: userDir, Proactive: proactive})})
	return c, memory.StoreFor(userDir, cwd)
}

const suggestArgs = `{"description":"staging deploys go to eu-west","body":"Staging is eu-west-1.","type":"project"}`

func runRemember(t *testing.T, c *Controller, store memory.Store, args string) string {
	t.Helper()
	ctx := memory.WithQueue(context.Background(), c)
	out, err := memory.NewRememberTool(store).Execute(ctx, json.RawMessage(args))
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	return out
}

func TestAskModeHoldsRememberAsSuggestion(t *testing.T) {
	c, store := suggestSession(t, "ask")
	if !c.allowLowRiskRemember(json.RawMessage(suggestArgs)) {
		t.Fatal("a suggestion writes nothing, so it needs no approval card")
	}
	out := runRemember(t, c, store, suggestArgs)
	if !strings.Contains(out, "suggestion") {
		t.Fatalf("result should say the fact was only held: %q", out)
	}
	if all, _ := store.ListAll(); len(all) != 0 {
		t.Fatalf("ask mode wrote %d facts before the user decided", len(all))
	}
	held := c.MemorySuggestions()
	if len(held) != 1 || held[0].Memory.Body != "Staging is eu-west-1." {
		t.Fatalf("suggestions = %+v", held)
	}
}

func TestAcceptWritesTheEditedFact(t *testing.T) {
	c, store := suggestSession(t, "ask")
	runRemember(t, c, store, suggestArgs)
	held := c.MemorySuggestions()[0]
	edited := held.Memory
	edited.Body = "Staging is eu-west-2."
	if _, err := c.AcceptMemorySuggestion(held.ID, &edited); err != nil {
		t.Fatal(err)
	}
	all, _ := store.ListAll()
	if len(all) != 1 || all[0].Body != "Staging is eu-west-2." {
		t.Fatalf("saved = %+v", all)
	}
	if len(c.MemorySuggestions()) != 0 {
		t.Fatal("an accepted suggestion must leave the queue")
	}
	if _, err := c.AcceptMemorySuggestion(held.ID, nil); !errors.Is(err, ErrNoSuchSuggestion) {
		t.Fatalf("second accept = %v, want ErrNoSuchSuggestion", err)
	}
}

func TestDismissDropsWithoutWriting(t *testing.T) {
	c, store := suggestSession(t, "ask")
	runRemember(t, c, store, suggestArgs)
	id := c.MemorySuggestions()[0].ID
	if err := c.DismissMemorySuggestion(id); err != nil {
		t.Fatal(err)
	}
	if all, _ := store.ListAll(); len(all) != 0 {
		t.Fatalf("dismiss wrote %d facts", len(all))
	}
	if err := c.DismissMemorySuggestion(id); !errors.Is(err, ErrNoSuchSuggestion) {
		t.Fatalf("second dismiss = %v", err)
	}
}

func TestAutoModeWritesDirectly(t *testing.T) {
	c, store := suggestSession(t, "auto")
	runRemember(t, c, store, suggestArgs)
	if all, _ := store.ListAll(); len(all) != 1 {
		t.Fatalf("auto mode saved %d facts, want 1", len(all))
	}
	if len(c.MemorySuggestions()) != 0 {
		t.Fatal("auto mode holds nothing")
	}
}

func TestAskModeStillUpdatesByID(t *testing.T) {
	c, _ := suggestSession(t, "ask")
	if c.allowLowRiskRemember(json.RawMessage(`{"id":"abc","description":"d","body":"b"}`)) {
		t.Fatal("changing an existing fact is not a suggestion and stays gated")
	}
}

func TestYoloSavesInsteadOfAsking(t *testing.T) {
	c, store := suggestSession(t, "ask")
	c.SetToolApprovalMode(ToolApprovalYolo)
	runRemember(t, c, store, suggestArgs)
	if all, _ := store.ListAll(); len(all) != 1 {
		t.Fatalf("under YOLO the fact should be saved, got %d", len(all))
	}
	if len(c.MemorySuggestions()) != 0 {
		t.Fatal("under YOLO nothing is held")
	}
}
