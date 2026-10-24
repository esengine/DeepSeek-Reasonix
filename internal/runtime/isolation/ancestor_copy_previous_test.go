package isolation

import (
	"bytes"
	"os"
	"testing"
)

func TestPreviousV5ReaderCannotDiscardAncestorCopy(t *testing.T) {
	binary := os.Getenv("REASONIX_PREVIOUS_COPY_READER")
	if binary == "" {
		t.Skip("independently built previous v5 owner binary not supplied")
	}
	store, original := integrationFixture(t)
	copy, err := store.CopyOwned(t.Context(), copySource(original), "descendant")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(t.Context(), copy, Outcome{}, nil); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(entryRecordPath(copy))
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"read", "discard-parent"} {
		cmd := previousNestedOwner(t.Context(), binary, store, copy.ID, action)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("previous owner: %s %v", out, err)
		}
		assertPreviousNestedRefusal(t, bytes.NewReader(out))
		after, err := os.ReadFile(entryRecordPath(copy))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("previous reader rewrote ancestor-copy protocol", err)
		}
		if got := read(t, copy.Cand.WorkspaceRoot, "a.txt"); got != "candidate\n" {
			t.Fatal("previous owner removed retained copy")
		}
	}
}
