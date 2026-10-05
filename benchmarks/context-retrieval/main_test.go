package main

import (
	"bytes"
	"fmt"
	"maps"
	mathrand "math/rand"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// The same checks the preflight mode runs, under go test: a compaction change
// that invalidates the fixtures fails here rather than three hundred API calls
// later.

func TestCorpusIsWellFormed(t *testing.T) {
	if err := validateCorpus(); err != nil {
		t.Fatal(err)
	}
	if got := len(searchTasks()); got != 6 {
		t.Errorf("search experiment has %d tasks, want 6", got)
	}
	if got := len(indexTasks()); got != 12 {
		t.Errorf("index experiment has %d tasks, want 12", got)
	}
	stopping := map[string]bool{}
	tiers := map[string]int{}
	for _, task := range indexTasks() {
		if task.Role == roleStopping {
			stopping[task.ID] = true
			continue
		}
		tiers[task.CueTier]++
	}
	if !stopping["i03-coalescing"] || !stopping["i04-recovery-fence"] {
		t.Errorf("the stopping corpus is %v; it must keep i03-coalescing and i04-recovery-fence", stopping)
	}
	// Two per tier, over the efficiency substrate, is what makes the staircase
	// readable: the same task can be compared across the boundary its cue sits
	// on, instead of two different questions being compared across two arms.
	for _, tier := range []string{tierQuarter, tierHalf, tierDefault} {
		if tiers[tier] != 2 {
			t.Errorf("tier %s has %d tasks, want 2", tier, tiers[tier])
		}
	}
}

// The role decides which batch a task enters; the literal lists are only where
// it is declared, so the two must not drift.
func TestIndexListsMatchTheirRoles(t *testing.T) {
	for _, task := range append(append(indexQuarterTasks(), indexHalfTasks()...), indexDefaultTasks()...) {
		if task.Role != roleEfficiency {
			t.Errorf("%s sits in an efficiency list with role %q", task.ID, task.Role)
		}
	}
	for _, task := range stoppingIndexTasks() {
		if task.Role != roleStopping {
			t.Errorf("%s sits in the stopping list with role %q", task.ID, task.Role)
		}
	}
}

// The invariant the whole instantiation layer exists for: no literal a scorer
// needs may be readable in this repository before a run starts.
func TestNoAnswerLiteralExistsInTheRepository(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Skip(err)
	}
	for _, task := range allTasks() {
		inst, err := instantiateTask(task, seededRand(task.ID, "leak-check"))
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range append(inst.AnswerMarkers, inst.CueMarker) {
			if len(marker) < 6 {
				continue // a bare number is not a literal anyone can grep for
			}
			if found, where := grepRepo(t, root, marker); found {
				t.Errorf("%s: %q is readable at %s before the run", task.ID, marker, where)
			}
		}
	}
}

// A run's values are unpredictable from a previous run only if the live
// generator is seeded from entropy. Comparing answers across two instances
// cannot assert that: a task whose answer is a bounded number collides by
// chance (1/8601 to 1/286 per task), so the check is on the seed source.
func TestLiveRandDrawsFromEntropy(t *testing.T) {
	a, b := liveRand(), liveRand()
	for range 4 {
		if a.Int63() != b.Int63() {
			return
		}
	}
	t.Fatal("two live generators produced the same stream: they are seeded alike")
}

// The source alone decides an instance: the same entropy gives the same
// answers, different entropy gives different values.
func TestLiveRandSourceDecidesTheInstance(t *testing.T) {
	same := func() *mathrand.Rand {
		rng, err := liveRandFrom(bytes.NewReader(bytes.Repeat([]byte{7}, 8)))
		if err != nil {
			t.Fatal(err)
		}
		return rng
	}
	other, err := liveRandFrom(bytes.NewReader(bytes.Repeat([]byte{9}, 8)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := liveRandFrom(bytes.NewReader(nil)); err == nil {
		t.Fatal("an empty entropy source was accepted")
	}
	for _, task := range allTasks() {
		a, _ := instantiateTask(task, same())
		b, _ := instantiateTask(task, same())
		if !slices.Equal(a.AnswerMarkers, b.AnswerMarkers) || !maps.Equal(a.Vars, b.Vars) {
			t.Errorf("%s: the same entropy gave different instances", task.ID)
		}
		c, _ := instantiateTask(task, other)
		if maps.Equal(a.Vars, c.Vars) {
			t.Errorf("%s: different entropy gave the same values", task.ID)
		}
	}
}

// A preflight that could not reproduce would calibrate against one run's
// nonces and report about another's.
func TestSeededInstancesReproduce(t *testing.T) {
	for _, task := range allTasks() {
		a, _ := instantiateTask(task, seededRand(task.ID, "x"))
		b, _ := instantiateTask(task, seededRand(task.ID, "x"))
		if !slices.Equal(a.AnswerMarkers, b.AnswerMarkers) || a.body != b.body {
			t.Errorf("%s did not reproduce under the same seed", task.ID)
		}
	}
}

// An unresolved placeholder would put "{{marker}}" in the transcript and score
// every run wrong; it fails the fixture instead.
func TestUnresolvedPlaceholderIsRefused(t *testing.T) {
	if _, err := instantiate("value is {{missing}}", fixtureVars{"other": "x"}); err == nil {
		t.Fatal("an unresolved placeholder was accepted")
	}
}

func repoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("not in a git checkout: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// grepRepo searches tracked files only: build artifacts and temp fixtures are
// not what a fresh checkout hands anyone.
func grepRepo(t *testing.T, root, needle string) (bool, string) {
	t.Helper()
	cmd := exec.Command("git", "grep", "-l", "-F", needle)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return false, "" // exit 1 means no match
	}
	return true, strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}

func TestEveryFixturePassesPreflight(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and folds 36 sessions")
	}
	root := t.TempDir()
	for _, task := range allTasks() {
		t.Run(task.ID, func(t *testing.T) {
			found, err := checkTask(task, root)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range found {
				t.Error(f)
			}
		})
	}
}
