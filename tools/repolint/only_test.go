package main

import "testing"

// A change touching one file should not have to read the whole tree's recorded
// debt to learn whether it introduced anything.
func TestOnlyNarrowsToTheFilesAChangeTouched(t *testing.T) {
	over := []Finding{{"mine.go", 1, ruleEssay, "", 1}, {"theirs.go", 2, ruleEssay, "", 1}}
	overruns := []Overrun{
		{File: "mine.go", Rule: ruleEssay, Count: 1, Allowed: 0},
		{File: "theirs.go", Rule: ruleEssay, Count: 1, Allowed: 0},
	}
	gotOver, gotRuns := limitToPaths(over, overruns, splitPaths("./mine.go"))
	if len(gotOver) != 1 || gotOver[0].File != "mine.go" {
		t.Fatalf("findings = %v, want only mine.go (and ./ prefixes normalized)", gotOver)
	}
	if len(gotRuns) != 1 || gotRuns[0].File != "mine.go" {
		t.Fatalf("overruns = %v, want only mine.go", gotRuns)
	}
}

// A repo-wide ceiling survives the filter: a file can push the tree past one
// without exceeding its own budget, and hiding that would make -only a way to
// pass a check the full run fails.
func TestOnlyKeepsRepoWideCeilings(t *testing.T) {
	_, gotRuns := limitToPaths(nil, []Overrun{{Rule: ruleBanner, Count: 5, Allowed: 2}}, splitPaths("mine.go"))
	if len(gotRuns) != 1 || gotRuns[0].File != "" {
		t.Fatalf("overruns = %v, want the repo ceiling kept", gotRuns)
	}
}

func TestOverrunReadsAsASentence(t *testing.T) {
	file := Overrun{File: "a.go", Rule: ruleEssay, Count: 4, Allowed: 0}
	if got := file.String(); got != "a.go: essay is 4 over its 0 budget" {
		t.Fatalf("String() = %q", got)
	}
	repo := Overrun{Rule: ruleBanner, Count: 5, Allowed: 2}
	if got := repo.String(); got != "repo total for banner is 5, above the 2 ceiling" {
		t.Fatalf("String() = %q", got)
	}
}

// An empty -only leaves the report untouched, so the default run is unchanged.
func TestEmptyOnlySelectsNothingToFilterBy(t *testing.T) {
	if got := splitPaths(""); len(got) != 0 {
		t.Fatalf("splitPaths(\"\") = %v, want empty so the caller skips filtering", got)
	}
}
