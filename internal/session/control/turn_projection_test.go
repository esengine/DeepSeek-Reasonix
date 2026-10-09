package control

import (
	"reasonix/internal/state/sessionstore"
	"slices"
	"strings"
	"testing"
)

// The order blocks are declared in is the order the model reads them, so it is
// pinned here rather than left to whichever prepend site was written last.
func TestTurnBlockOrderIsDeclared(t *testing.T) {
	c := New(Options{})
	defer c.Close()

	got := []string{}
	for _, blk := range c.turnBlocksFor("hello", true, nil, false, "", "", "", "") {
		got = append(got, blk.tag)
	}
	want := []string{
		"hook-context",
		"available-skills",
		"project-instructions",
		"mcp-prompt-failure",
		"scheduled-run",
		"background-jobs",
		"memory-update",
		"reasoning-language",
		"response-language",
		"", // the plan-mode marker, which is a line and not a block
		"active-goal",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("turn block order changed:\n got %v\nwant %v", got, want)
	}
}

// Every block a turn can carry has to be registered, or the strippers, the
// search indexer and the supersede pass all silently treat it as user text.
// Registration is one list; this is what makes adding a block reach it.
func TestEveryTurnBlockIsRegisteredAsTransient(t *testing.T) {
	c := New(Options{})
	defer c.Close()

	for _, blk := range c.turnBlocksFor("hello", true, nil, false, "", "", "", "") {
		if blk.tag == "" {
			continue
		}
		if !slices.Contains(sessionstore.TransientUserBlockTags, blk.tag) {
			t.Errorf("block %q is projected onto turns but is not in sessionstore.TransientUserBlockTags", blk.tag)
		}
	}
}

// The tail follows the user's text; every other block precedes it.
func TestProjectTurnPlacesBlocksAroundTheText(t *testing.T) {
	got := projectTurn([]turnBlock{
		{"first", "<first>a</first>"},
		{"skipped", ""},
		{"second", "<second>b</second>"},
	}, "the request", "<tail>c</tail>")

	want := "<first>a</first>\n\n<second>b</second>\n\nthe request\n\n<tail>c</tail>"
	if got != want {
		t.Fatalf("projection = %q\nwant %q", got, want)
	}
	if strings.Contains(got, "skipped") {
		t.Fatalf("an unowed block was rendered: %q", got)
	}
}
