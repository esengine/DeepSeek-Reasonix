package main

import (
	"fmt"
	"strings"
)

// Anchor arms decide what hypothesis the agent holds before it has looked at
// anything: none, the task's real cause, or a plausible cause that is wrong.
// Comparing the three prices how much a handed-down conclusion is worth, and
// how much of it survives contact with the evidence.
const (
	anchorBlind   = "blind"
	anchorCorrect = "correct"
	anchorWrong   = "wrong"
)

func normalizeAnchorArm(arm string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(arm)) {
	case "", anchorBlind:
		return anchorBlind, nil
	case anchorCorrect:
		return anchorCorrect, nil
	case anchorWrong:
		return anchorWrong, nil
	default:
		return "", fmt.Errorf("unknown anchor arm %q (want blind, correct or wrong)", arm)
	}
}

// seedFor returns the hypothesis this arm hands the agent, and whether the
// task can be scored in that arm at all. A task with no authored seed is never
// silently run blind: its control run would land in the seeded denominator and
// flatter whichever arm collected it.
func (t task) seedFor(anchor string) (string, bool) {
	switch anchor {
	case "", anchorBlind:
		return "", true
	case anchorCorrect:
		seed := strings.TrimSpace(t.SeedCorrect)
		return seed, seed != ""
	case anchorWrong:
		seed := strings.TrimSpace(t.SeedWrong)
		return seed, seed != ""
	}
	return "", false
}

// anchorPrompt puts this arm's hypothesis in front of the task, where the
// agent meets it before it has read anything. An anchor offered after
// exploration would be a different experiment.
func anchorPrompt(anchor string, t task) string {
	seed, ok := t.seedFor(anchor)
	if !ok || seed == "" {
		return t.Prompt
	}
	return seed + "\n\n" + t.Prompt
}

// anchorSkip returns the recorded skip for a task this arm cannot score, so a
// missing seed leaves a visible row instead of quietly shrinking the corpus.
func anchorSkip(cfg suiteConfig, t task) (result, bool) {
	if _, ok := t.seedFor(cfg.anchor); ok {
		return result{}, false
	}
	return result{
		task: t, Profile: benchmarkProfileStandard, Anchor: cfg.anchor, Skipped: true,
		Note: "skipped: no seed_" + cfg.anchor + " authored for this task",
	}, true
}

// renderAnchor reports the arm and what it scored. Anchor resistance is only
// meaningful against a blind baseline from the same corpus, so this section
// publishes one arm's numbers and names the comparison rather than inventing a
// resistance figure from a single run.
func renderAnchor(results []result) string {
	arm := ""
	solved, total, skipped := 0, 0, 0
	for _, r := range results {
		if strings.TrimSpace(r.Anchor) != "" {
			arm = r.Anchor
		}
		if r.Skipped {
			if strings.Contains(r.Note, "no seed_") {
				skipped++
			}
			continue
		}
		total++
		if r.Passed {
			solved++
		}
	}
	if arm == "" || arm == anchorBlind {
		// The control arm needs no section of its own: it is the ordinary run.
		if skipped == 0 {
			return ""
		}
	}
	b := fmt.Sprintf("**Anchor**: arm **%s** · solved %d/%d (%s)\n", arm, solved, total, pct(solved, total))
	if skipped > 0 {
		// Naming the drop matters: a seeded arm silently scoring fewer tasks
		// than the blind baseline is not the same experiment.
		b += fmt.Sprintf("- **%d task(s) skipped**: no seed authored, so they are outside this arm's denominator\n", skipped)
	}
	if arm == anchorWrong {
		b += "- resistance = this solve rate over the blind arm's on the same tasks\n"
	}
	return b + "\n"
}
