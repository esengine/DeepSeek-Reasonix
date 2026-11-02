package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/contract/eventwire"
)

// A turn whose text is empty because it was only an image is still a turn:
// /history carries the count exactly because the text has nothing left once
// the host's blocks come off. Restore has to keep it and say what it was.
func TestRestoreKeepsATurnThatWasOnlyAnImage(t *testing.T) {
	var m HistoryMessage
	if err := json.Unmarshal([]byte(`{"role":"user","content":"","images":1}`), &m); err != nil {
		t.Fatal(err)
	}
	tr := &Transcript{}
	tr.Restore([]HistoryMessage{m})
	if len(tr.Items) != 1 || tr.Items[0].Kind != ItemUser {
		t.Fatalf("an image-only turn restored as %d rows (%+v)", len(tr.Items), tr.Items)
	}
	if tr.Items[0].Images != 1 {
		t.Fatalf("the restored row lost the image count: %+v", tr.Items[0])
	}
	if got := ansi.Strip(renderItem(&tr.Items[0], 80, 0, false)); !strings.Contains(got, "1 image") {
		t.Fatalf("the restored row says nothing about the image: %q", got)
	}
}

// A turn that carried words and an image keeps both facts.
func TestRestoreKeepsTheWordsAndTheImageOfATurn(t *testing.T) {
	tr := &Transcript{}
	tr.Restore([]HistoryMessage{{Role: "user", Content: "what is this?", Images: 2}})
	if len(tr.Items) != 1 {
		t.Fatalf("restored %d rows, want one: %+v", len(tr.Items), tr.Items)
	}
	row := tr.Items[0]
	if row.Text != "what is this?" || row.Images != 2 {
		t.Fatalf("restored row = %+v, want its words and two images", row)
	}
	got := ansi.Strip(renderItem(&row, 80, 0, false))
	if !strings.Contains(got, "what is this?") || !strings.Contains(got, "2 image") {
		t.Fatalf("the row does not show both facts: %q", got)
	}
}

// A normal text turn restores exactly as it did before: the person's line and
// a settled answer, with nothing added to either.
func TestRestoreOfANormalTextTurnIsUnchanged(t *testing.T) {
	tr := &Transcript{}
	tr.Restore([]HistoryMessage{
		{Role: "user", Content: "fix the build"},
		{Role: "assistant", Content: "fixed"},
	})
	if len(tr.Items) != 2 {
		t.Fatalf("restored %d rows, want two: %+v", len(tr.Items), tr.Items)
	}
	if tr.Items[0].Kind != ItemUser || tr.Items[0].Text != "fix the build" || tr.Items[0].Images != 0 || tr.Items[0].Via != nil {
		t.Fatalf("user row = %+v", tr.Items[0])
	}
	if tr.Items[1].Kind != ItemSay || tr.Items[1].Text != "fixed" || !tr.Items[1].Done {
		t.Fatalf("answer row = %+v", tr.Items[1])
	}
	if got := ansi.Strip(renderItem(&tr.Items[0], 80, 0, false)); strings.Contains(got, "image") || strings.Contains(got, "device") {
		t.Fatalf("a normal text turn gained a line: %q", got)
	}
}

// The contract keeps the provenance of a turn: the device a person wrote from
// and the model that answered. Restore keeps both on the row that carries them,
// so a reload can still say them.
func TestRestoreKeepsWhereATurnCameFrom(t *testing.T) {
	tr := &Transcript{}
	tr.Restore([]HistoryMessage{
		{Role: "user", Content: "go", Via: &eventwire.Via{Device: "dev-a", Ordinal: 2}},
		{Role: "assistant", Content: "ok", ModelRef: "deepseek/deepseek-v4"},
	})
	if len(tr.Items) != 2 {
		t.Fatalf("restored %d rows, want two: %+v", len(tr.Items), tr.Items)
	}
	if via := tr.Items[0].Via; via == nil || via.Device != "dev-a" || via.Ordinal != 2 {
		t.Fatalf("the user row lost where it came from: %+v", tr.Items[0])
	}
	if got := ansi.Strip(renderItem(&tr.Items[0], 80, 0, false)); !strings.Contains(got, "device 2") {
		t.Fatalf("the restored row does not say where it came from: %q", got)
	}
	if tr.Items[1].ModelRef != "deepseek/deepseek-v4" {
		t.Fatalf("the answer row lost the model that wrote it: %+v", tr.Items[1])
	}
}
