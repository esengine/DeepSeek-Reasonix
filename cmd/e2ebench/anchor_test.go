package main

import (
	"strings"
	"testing"
)

func TestNormalizeAnchorArmRejectsUnknownArms(t *testing.T) {
	for in, want := range map[string]string{"": anchorBlind, "BLIND": anchorBlind, "correct": anchorCorrect, " wrong ": anchorWrong} {
		got, err := normalizeAnchorArm(in)
		if err != nil || got != want {
			t.Errorf("normalizeAnchorArm(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := normalizeAnchorArm("seeded"); err == nil {
		t.Fatal("unknown anchor arm accepted")
	}
}

// A task with no authored seed must be unscorable in a seeded arm. Running it
// blind instead would put a control run in the seeded denominator, which is
// the one way an anchor-resistance figure can be quietly wrong.
func TestSeedForRefusesToRunAnUnseededTaskInASeededArm(t *testing.T) {
	seeded := task{SeedCorrect: "It is the CRLF path.", SeedWrong: "It is the byte indexing."}
	bare := task{}

	if seed, ok := seeded.seedFor(anchorWrong); !ok || seed != "It is the byte indexing." {
		t.Errorf("seeded wrong arm = %q, %v", seed, ok)
	}
	if _, ok := bare.seedFor(anchorCorrect); ok {
		t.Error("unseeded task accepted into the correct arm")
	}
	if _, ok := bare.seedFor(anchorWrong); ok {
		t.Error("unseeded task accepted into the wrong arm")
	}
	// Blind is the control: every task belongs to it, seeded or not.
	if seed, ok := bare.seedFor(anchorBlind); !ok || seed != "" {
		t.Errorf("blind arm = %q, %v; want every task, unseeded", seed, ok)
	}
	if seed, ok := seeded.seedFor(anchorBlind); !ok || seed != "" {
		t.Errorf("blind arm handed a seed: %q", seed)
	}
}

// The seed has to actually reach the prompt, and reach it in front: an
// experiment whose treatment never arrives would report the control's numbers
// under the treatment's name.
func TestAnchorPromptPutsTheSeedInFrontOfTheTask(t *testing.T) {
	diagnose := task{
		Prompt:      "Diagnose the failing test.",
		SeedCorrect: "It is the CRLF path.",
		SeedWrong:   "It is the byte indexing.",
	}
	if got := anchorPrompt(anchorBlind, diagnose); got != diagnose.Prompt {
		t.Errorf("blind arm altered the prompt: %q", got)
	}
	for arm, seed := range map[string]string{anchorCorrect: diagnose.SeedCorrect, anchorWrong: diagnose.SeedWrong} {
		got := anchorPrompt(arm, diagnose)
		if !strings.HasPrefix(got, seed) {
			t.Errorf("%s arm did not lead with its seed: %q", arm, got)
		}
		if !strings.HasSuffix(got, diagnose.Prompt) {
			t.Errorf("%s arm lost the task: %q", arm, got)
		}
	}
	// An unseeded task is skipped, never quietly run as the control.
	if got := anchorPrompt(anchorWrong, task{Prompt: "p"}); got != "p" {
		t.Errorf("unseeded task was altered: %q", got)
	}
}

// The blind arm is the ordinary run and needs no section; a seeded arm must
// name itself, because its numbers are not comparable with the baseline's.
func TestRenderAnchorStaysSilentForTheControlArm(t *testing.T) {
	got := renderAnchor([]result{{Passed: true, Anchor: anchorBlind}, {Anchor: anchorBlind}})
	if got != "" {
		t.Fatalf("blind arm rendered a section: %q", got)
	}
}

// A seeded arm scoring fewer tasks than the baseline is a different corpus.
// The drop has to be stated, not left to be inferred from a smaller total.
func TestRenderAnchorNamesTheArmAndItsSkippedTasks(t *testing.T) {
	got := renderAnchor([]result{
		{Passed: true, Anchor: anchorWrong},
		{Passed: false, Anchor: anchorWrong},
		{Anchor: anchorWrong, Skipped: true, Note: "skipped: no seed_wrong authored for this task"},
	})
	for _, want := range []string{"arm **wrong**", "solved 1/2", "**1 task(s) skipped**", "resistance ="} {
		if !strings.Contains(got, want) {
			t.Errorf("anchor section missing %q:\n%s", want, got)
		}
	}
}
