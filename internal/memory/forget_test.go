package memory

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestForgetToolDeletes drives the tool with raw JSON args and verifies the fact
// is removed from the store.
func TestForgetToolDeletes(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	if _, err := store.Save(Memory{Name: "stale-fact", Description: "d", Type: TypeProject, Body: "b"}); err != nil {
		t.Fatal(err)
	}

	tl := NewForgetTool(store)
	if tl.Name() != "forget" || tl.ReadOnly() {
		t.Fatalf("unexpected tool identity: name=%q readonly=%v", tl.Name(), tl.ReadOnly())
	}
	if !json.Valid(tl.Schema()) {
		t.Fatal("forget schema is not valid JSON")
	}

	out, err := tl.Execute(context.Background(), []byte(`{"name":"stale-fact"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Forgot memory") {
		t.Fatalf("unexpected tool output: %q", out)
	}
	if len(store.List()) != 0 {
		t.Fatalf("memory not deleted: %+v", store.List())
	}
}

// TestForgetToolValidates rejects an empty name rather than deleting nothing
// silently.
func TestForgetToolValidates(t *testing.T) {
	tl := NewForgetTool(Store{Dir: t.TempDir()})
	if _, err := tl.Execute(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("expected error when name is missing")
	}
}

// fakeQueue records the turn-tail notes the remember/forget tools queue.
type fakeQueue struct{ notes []string }

func (f *fakeQueue) QueueMemory(note string) { f.notes = append(f.notes, note) }

// TestForgetToolQueuesDisregardNote verifies a forget injects a turn-tail note so
// the model stops trusting the still-cached index line this session.
func TestForgetToolQueuesDisregardNote(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	if _, err := store.Save(Memory{Name: "old-fact", Description: "d", Type: TypeProject, Body: "b"}); err != nil {
		t.Fatal(err)
	}
	q := &fakeQueue{}
	ctx := WithQueue(context.Background(), q)
	if _, err := NewForgetTool(store).Execute(ctx, []byte(`{"name":"old-fact"}`)); err != nil {
		t.Fatal(err)
	}
	if len(q.notes) != 1 || !strings.Contains(q.notes[0], "old-fact") {
		t.Fatalf("expected one queued note naming the deleted memory, got %v", q.notes)
	}
}
