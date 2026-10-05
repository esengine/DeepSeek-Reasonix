package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestPerseverationGuardEvidence replays the recorded samples under
// testdata/perseveration. Each .problem is fed to the guard the way it was
// recorded — the prose prefix as one delta, then unit-sized deltas — and the
// trip offset, the trimmed retry, and the recomputed .cost are asserted against
// the recorded files.
func TestPerseverationGuardEvidence(t *testing.T) {
	problems, err := filepath.Glob(filepath.Join("testdata", "perseveration", "*.problem"))
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) == 0 {
		t.Fatal("no .problem fixtures under testdata/perseveration")
	}
	for _, problemPath := range problems {
		base := strings.TrimSuffix(problemPath, ".problem")
		name := filepath.Base(base)
		t.Run(name, func(t *testing.T) {
			problem := readFixture(t, problemPath)
			fixed := readFixture(t, base+".fixed")
			cost := readFixture(t, base+".cost")
			unit := costUnit(t, cost)

			// The prose prefix goes in one delta, then one unit at a time,
			// reproducing how the fixture was recorded; the trip lands at the
			// end of the repeated block the .cost records.
			start := strings.Index(problem, unit)
			if start <= 0 {
				t.Fatalf("repeated unit %q not found after the prose prefix", unit)
			}
			g := newPerseverationGuard()
			g.observe(problem[:start])
			trip := -1
			for i := start; i+len(unit) <= len(problem); i += len(unit) {
				if g.observe(problem[i : i+len(unit)]) {
					trip = i + len(unit)
					break
				}
			}
			if trip < 0 {
				t.Fatal("guard never tripped on the problem fixture")
			}

			block := trip - start
			reps := block / len(unit)
			tokens := (block + 3) / 4
			wantCost := fmt.Sprintf(
				"example: %s\n"+
					"repeated unit: %q (%d bytes)\n"+
					"repetitions generated before the guard tripped: %d\n"+
					"repeated block before tripping/stripping: %d bytes\n"+
					"wasted tokens (ceil(bytes/4)): %d\n",
				name, unit, len(unit), reps, block, tokens)
			if cost != wantCost {
				t.Fatalf("cost mismatch (guard tripped at byte %d):\n got %q\nwant %q", trip, cost, wantCost)
			}

			if want := trimPerseverationTail(problem) + perseverationRetryMessage(1); fixed != want {
				t.Fatalf("trimmed retry mismatch:\n got %q\nwant %q", fixed, want)
			}
		})
	}
}

func readFixture(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// costUnit reads the repeated unit back out of a .cost file's quoted field.
func costUnit(t *testing.T, cost string) string {
	t.Helper()
	lines := strings.SplitN(cost, "\n", 3)
	if len(lines) < 2 || !strings.HasPrefix(lines[1], "repeated unit: ") {
		t.Fatalf("unexpected .cost header: %q", cost)
	}
	rest := strings.TrimPrefix(lines[1], "repeated unit: ")
	first, last := strings.IndexByte(rest, '"'), strings.LastIndexByte(rest, '"')
	if first < 0 || last <= first {
		t.Fatalf("unexpected repeated-unit line: %q", lines[1])
	}
	unit, err := strconv.Unquote(rest[first : last+1])
	if err != nil {
		t.Fatalf("unquote repeated unit: %v", err)
	}
	return unit
}
