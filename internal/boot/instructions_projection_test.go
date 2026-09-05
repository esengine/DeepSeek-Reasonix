package boot

// Standing instructions as turn-dynamic canonical state: they are discovered
// from disk each turn, so an edit made past every writer still reaches the model
// — and an edit that changes nothing the model reads reaches it not at all.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
)

func writeInstructions(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestInstructionsOutOfBandEditReachesTheNextTurn(t *testing.T) {
	h := newProjectionHarness(t, "instructions-outofband", "", "")
	writeInstructions(t, h.dir, "REASONIX.md", "Rule one: never push on red.\n")
	h.restart()

	first := h.turn("turn-alpha")
	firstBlock := blockOf(projectionOf(t, first, "turn-alpha"), "project-instructions")
	if !strings.Contains(firstBlock, "Rule one") {
		t.Fatalf("the first turn did not carry the instructions on disk:\n%s", firstBlock)
	}
	if again := blockOf(projectionOf(t, h.turn("turn-beta"), "turn-beta"), "project-instructions"); again != "" {
		t.Errorf("the instructions were re-sent although nothing changed:\n%s", again)
	}

	// The edit no writer announces: an editor, a script, a branch switch.
	writeInstructions(t, h.dir, "REASONIX.md", "Rule one: never push on red.\nRule two: run make lint first.\n")
	changed := blockOf(projectionOf(t, h.turn("turn-gamma"), "turn-gamma"), "project-instructions")
	if !strings.Contains(changed, "Rule two") {
		t.Errorf("freshness: an out-of-band instruction edit never reached the model:\n%s", changed)
	}
	if again := blockOf(projectionOf(t, h.turn("turn-delta"), "turn-delta"), "project-instructions"); again != "" {
		t.Errorf("the changed instructions were sent twice:\n%s", again)
	}

	// A rewrite of the same bytes moves the file and changes nothing the model
	// reads, which is the case a metadata fingerprint would get wrong.
	writeInstructions(t, h.dir, "REASONIX.md", "Rule one: never push on red.\nRule two: run make lint first.\n")
	if again := blockOf(projectionOf(t, h.turn("turn-epsilon"), "turn-epsilon"), "project-instructions"); again != "" {
		t.Errorf("a rewrite that changed no rule re-sent the block:\n%s", again)
	}

	if a, b := systemOf(first), systemOf(h.turn("turn-zeta")); a != b {
		t.Errorf("cache-boundary: editing instructions moved the prefix:\nfirst diff site: %q", firstDivergence(a, b))
	}

	live := blockOf(projectionOf(t, h.turn("turn-eta"), "turn-eta"), "project-instructions")
	if live != "" {
		t.Fatalf("nothing changed and a block was still owed, so restart parity measures nothing:\n%s", live)
	}
	h.restart()
	rebuilt := blockOf(projectionOf(t, h.turn("turn-theta"), "turn-theta"), "project-instructions")
	if !strings.Contains(rebuilt, "Rule two") {
		t.Errorf("a rebuilt session did not carry the instructions it read from disk:\n%s", rebuilt)
	}
	if rebuilt != changed {
		t.Errorf("the rebuilt session disagrees with the live one it replaced:\nfirst diff site: %q", firstDivergence(changed, rebuilt))
	}
}

// AGENTS.md is a peer file, not a fallback, so it has to answer the same way.
func TestInstructionsAgentsFileReachesTheNextTurn(t *testing.T) {
	h := newProjectionHarness(t, "instructions-agents", "", "")
	writeInstructions(t, h.dir, "REASONIX.md", "Rule one: never push on red.\n")
	h.restart()
	h.turn("turn-alpha")

	writeInstructions(t, h.dir, "AGENTS.md", "Rule from AGENTS: prefer table tests.\n")
	block := blockOf(projectionOf(t, h.turn("turn-beta"), "turn-beta"), "project-instructions")
	if !strings.Contains(block, "prefer table tests") {
		t.Errorf("freshness: a new AGENTS.md never reached the model:\n%s", block)
	}
}

// Removal is the row silence gets wrong. The rules the model already has would
// go on standing as current, which is the failure a deletion is most likely to
// cause and the one nothing would report.
func TestInstructionsRemovalStopsStandingAsCurrent(t *testing.T) {
	h := newProjectionHarness(t, "instructions-removal", "", "")
	writeInstructions(t, h.dir, "REASONIX.md", "Rule one: never push on red.\n")
	h.restart()
	if block := blockOf(projectionOf(t, h.turn("turn-alpha"), "turn-alpha"), "project-instructions"); !strings.Contains(block, "Rule one") {
		t.Fatalf("the arm has no instructions to remove:\n%s", block)
	}

	if err := os.Remove(filepath.Join(h.dir, "REASONIX.md")); err != nil {
		t.Fatalf("remove the instructions: %v", err)
	}
	block := blockOf(projectionOf(t, h.turn("turn-beta"), "turn-beta"), "project-instructions")
	if block == "" {
		t.Fatalf("the instructions were removed and the model was told nothing; the rules it holds still read as current")
	}
	if strings.Contains(block, "Rule one") {
		t.Errorf("the removal re-sent the rule it removed:\n%s", block)
	}
	if again := blockOf(projectionOf(t, h.turn("turn-gamma"), "turn-gamma"), "project-instructions"); again != "" {
		t.Errorf("the removal notice was sent twice:\n%s", again)
	}
}

// A fold owes the block again for a different reason than an edit does: the
// canonical state did not move, the model's copy of it did.
func TestInstructionsAreOwedAgainAfterAFold(t *testing.T) {
	h := newProjectionHarness(t, "instructions-fold", "\ncompact_ratio = 0.4\n", "\ncontext_window = 12000\n")
	writeInstructions(t, h.dir, "REASONIX.md", "Rule one: never push on red.\n")
	h.restart()

	filler := strings.Repeat("filler sentence about the ledger. ", 200)
	for _, prompt := range []string{"turn-1", "turn-2", "turn-3", "turn-4", "turn-5", "turn-6"} {
		h.turn(prompt + " " + filler)
	}
	if err := h.ctrl.Compact(t.Context(), ""); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if !h.events.saw(event.CompactionDone) {
		t.Fatalf("no fold completed, so this arm measures nothing: %v", h.events.kinds)
	}

	block := blockOf(projectionOf(t, h.turn("turn-eta"), "turn-eta"), "project-instructions")
	if !strings.Contains(block, "Rule one") {
		t.Errorf("the fold left the session with no standing instructions, and nothing on disk had changed:\n%s", block)
	}
}
