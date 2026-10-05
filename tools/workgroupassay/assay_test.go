package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
	"reasonix/internal/state/workgroup"
)

func toolFrame(id string, issuer event.ToolIssuer, kind string) eventwire.Event {
	return eventwire.Event{Kind: kind, Source: "executor",
		Tool: &eventwire.Tool{ID: id, Name: "bash", Issuer: string(issuer)}}
}

// sampleTurn builds one authored turn holding n assistant calls in a single
// run, which is the shape the group size is read off.
func sampleTurn(authored, calls int) turn {
	t := turn{session: "s.wire.jsonl", authored: authored, index: authored,
		frames: []eventwire.Event{{Kind: "turn_started"}}}
	for i := range calls {
		id := string(rune('a'+authored%26)) + string(rune('0'+i%10))
		t.frames = append(t.frames,
			toolFrame(id, event.IssuedByModel, "tool_dispatch"),
			toolFrame(id, event.IssuedByModel, "tool_result"))
	}
	t.frames = append(t.frames, eventwire.Event{Kind: "turn_done"})
	return t
}

func sample(sizes ...int) []turn {
	out := make([]turn, 0, len(sizes))
	for i, n := range sizes {
		out = append(out, sampleTurn(i+1, n))
	}
	return out
}

// The verdict has never run on real data, so it runs on a sample built to sit
// on each side of the gate. An instrument nobody has seen produce a number is
// one nobody has checked.
func TestTheFrozenGateFiresOnlyWhenBothHalvesHold(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sizes []int
		noGo  bool
	}{
		// Every group holds one call and no turn has three: both halves.
		{"thin and rare", []int{1, 1, 1, 1, 1, 1, 1, 1, 1, 1}, true},
		// Groups are still mostly single, but plenty of turns have work to fold.
		{"thin but common", []int{1, 1, 1, 1, 1, 4, 4, 4, 4, 4}, false},
		// Few turns are rich, but the groups that exist are not single calls.
		{"coarse but rare", []int{2, 2, 2, 2, 2, 2, 2, 2, 2, 2}, false},
		{"rich", []int{3, 4, 5, 3, 4, 5, 3, 4, 5, 6}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := assess(sample(tc.sizes...), workgroup.V1())
			if a.Illegal != "" {
				t.Fatalf("the constructed sample is not legally partitionable: %s", a.Illegal)
			}
			if a.Turns != len(tc.sizes) {
				t.Fatalf("turns = %d, want %d", a.Turns, len(tc.sizes))
			}
			if a.noGo() != tc.noGo {
				t.Fatalf("NO-GO = %v (singleton %.0f%%, rich %.0f%%), want %v",
					a.noGo(), 100*a.singletonShare(), 100*a.richShare(), tc.noGo)
			}
		})
	}
}

// A turn with no calls is in the sample and contributes no group. Dropping the
// quiet turns would answer a different question and flatter the feature.
func TestAQuietTurnCountsAgainstCoverage(t *testing.T) {
	a := assess(sample(0, 0, 0, 0, 0, 4, 4, 4, 4, 4), workgroup.V1())
	if a.Turns != 10 || a.ZeroCallTurns != 5 {
		t.Fatalf("turns=%d zero-call=%d, want 10 and 5", a.Turns, a.ZeroCallTurns)
	}
	if a.richShare() != 0.5 {
		t.Fatalf("rich share = %.2f, want the quiet turns in the denominator", a.richShare())
	}
}

// A sample this fold cannot legally partition is not a sample to draw a verdict
// from, so the assay refuses rather than reporting numbers over it.
func TestAnIllegalPartitionRefusesRatherThanReports(t *testing.T) {
	bad := sample(2)
	// A barrier with the boundary in the wrong place seals the group over a
	// call still running — the defect the semantics pass rejected.
	frames := []eventwire.Event{
		{Kind: "turn_started"},
		toolFrame("x", event.IssuedByModel, "tool_dispatch"),
		{Kind: "approval_request", Approval: &eventwire.Approval{ID: "b1"}},
		toolFrame("x", event.IssuedByModel, "tool_result"),
		{Kind: "turn_done"},
	}
	bad[0].frames = frames
	rules := workgroup.V1()
	rules.BarrierClosesAfterCall, rules.BarrierSplitsInPlace = false, true
	if a := assess(bad, rules); a.Illegal == "" {
		t.Fatal("the assay reported over a partition that seals a group on a running call")
	}
}

// Eligibility is the protocol's, not the reader's: a turn the host never named
// is a synthetic continuation, and a truncated log is a prefix.
func TestOnlyNamedTurnsInACompleteLogAreEligible(t *testing.T) {
	dir := t.TempDir()
	name := time.Now().Format("20060102-150405") + ".000000000-m.wire.jsonl"
	path := filepath.Join(dir, name)
	authored := 1
	named, _ := json.Marshal(eventwire.Event{Kind: "turn_started", AuthoredTurn: &authored})
	unnamed, _ := json.Marshal(eventwire.Event{Kind: "turn_started"})
	body := string(named) + "\n" + string(unnamed) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	turns, unnamedCount := turnsIn(path, time.Now())
	if len(turns) != 1 || unnamedCount != 1 {
		t.Fatalf("turns=%d unnamed=%d, want one of each", len(turns), unnamedCount)
	}
	meta := filepath.Join(dir, strings.TrimSuffix(name, ".wire.jsonl")+".wire.meta.json")
	if err := os.WriteFile(meta, []byte(`{"truncated":true}`), 0o600); err != nil {
		t.Fatalf("write witness: %v", err)
	}
	if !truncated(path) {
		t.Fatal("a log whose witness says truncated was read as complete")
	}
}

// The protocol says the natural sample excludes experiments. Saying it is not
// enough: before this, the only thing keeping a scripted run out was that it
// happened before the freeze, which says nothing about the next one. A
// workspace under a temp root is where a driven run lives, and the check reads
// the host's own slug encoding rather than guessing at directory names.
func TestWorkspacesUnderATempRootAreNotOrdinaryUse(t *testing.T) {
	slugs := scratchSlugs()
	if len(slugs) == 0 {
		t.Fatal("no temp-root slugs were derived, so nothing would ever be excluded")
	}
	for _, tc := range []struct {
		name    string
		dir     string
		scratch bool
	}{
		{"a driven run's workspace", config.WorkspaceSlug(filepath.Join(os.TempDir(), "assay", "ws")), true},
		{"the system temp root itself", config.WorkspaceSlug(filepath.Clean(os.TempDir())), true},
		{"a person's own project", config.WorkspaceSlug(filepath.Join(home(t), "projects", "thing")), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := underScratch(tc.dir, slugs); got != tc.scratch {
				t.Fatalf("underScratch(%q) = %v, want %v", tc.dir, got, tc.scratch)
			}
		})
	}
	// Where one temp root is reached by two names, the host may have written
	// either. Asserting the second name unconditionally instead asserts that
	// a Unix path is a temp root on hosts where it is an ordinary directory.
	if alias := resolved("/tmp"); filepath.Clean(alias) != filepath.Clean("/tmp") {
		t.Run("the temp root's other name", func(t *testing.T) {
			dir := config.WorkspaceSlug(filepath.Join(alias, "run"))
			if !underScratch(dir, slugs) {
				t.Fatalf("underScratch(%q) = false, want true", dir)
			}
		})
	}
}

func home(t *testing.T) string {
	t.Helper()
	dir, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory to build a non-temp path from")
	}
	return dir
}
