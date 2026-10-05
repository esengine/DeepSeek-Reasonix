package evidence

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

const (
	realInvariant = "func TestFoo(t *testing.T) { assertRealInvariant(t) }\n"
	assertsTrue   = "func TestFoo(t *testing.T) { _ = true }\n"
)

func storeIn(t *testing.T) *BaselineStore {
	t.Helper()
	return NewBaselineStore(filepath.Join(testenv.TempDir(t), "criteria"))
}

func capture(t *testing.T, s *BaselineStore, id, content string) TestCriterion {
	t.Helper()
	c, err := s.Capture(id, []byte(content))
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	return c
}

// The store owns what it holds: the same bytes address the same way, and what
// was captured cannot be edited under its own name.
func TestTheBaselineStoreIsContentAddressedAndWriteOnce(t *testing.T) {
	store := storeIn(t)
	first := capture(t, store, "cache_test.go", realInvariant)
	again := capture(t, store, "cache_test.go", realInvariant)
	if first != again {
		t.Fatalf("capture is not stable: %+v vs %+v", first, again)
	}
	rewritten := capture(t, store, "cache_test.go", assertsTrue)
	if rewritten.Digest == first.Digest || rewritten.Identity() == first.Identity() {
		t.Fatal("a rewritten criterion took the identity of the one it replaced")
	}
	content, err := store.Open(first)
	if err != nil || string(content) != realInvariant {
		t.Fatalf("Open(first) = %q, %v; want the original bytes intact", content, err)
	}
}

// A digest the store does not hold is unavailable. It never falls back to the
// workspace, because the workspace is what execution was allowed to rewrite.
func TestTheStoreNeverFallsBackToTheWorkspace(t *testing.T) {
	root := testenv.TempDir(t)
	store := NewBaselineStore(filepath.Join(root, "criteria"))
	absent := TestCriterion{LogicalID: "cache_test.go", Digest: DigestOf([]byte(realInvariant))}

	// The bytes exist in the workspace under the same name; the store still
	// cannot produce them.
	if err := os.WriteFile(filepath.Join(root, "cache_test.go"), []byte(realInvariant), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(absent); err == nil {
		t.Fatal("the store answered from something it does not hold")
	}
}

// The streams below are what `go test -overlay=… -json` actually emitted for
// each shape; the attribution is read off them rather than off error text.
const (
	baselineFailed = `{"Action":"run","Package":"spike","Test":"TestEvict"}
{"Action":"fail","Package":"spike","Test":"TestEvict"}
{"Action":"fail","Package":"spike"}
`
	unrelatedFailed = `{"Action":"run","Package":"spike","Test":"TestEvict"}
{"Action":"pass","Package":"spike","Test":"TestEvict"}
{"Action":"run","Package":"spike","Test":"TestUnrelated"}
{"Action":"fail","Package":"spike","Test":"TestUnrelated"}
{"Action":"fail","Package":"spike"}
`
	// A package that would not build with the captured criterion in it reports
	// no test event at all.
	buildFailed = `{"Action":"fail","Package":"spike"}
`
	baselinePassed = `{"Action":"run","Package":"spike","Test":"TestEvict"}
{"Action":"pass","Package":"spike","Test":"TestEvict"}
{"Action":"pass","Package":"spike"}
`
)

func TestBaselineOutcomeIsAttributedToTheCriterionAndNothingElse(t *testing.T) {
	criteria := []string{"TestEvict"}
	cases := []struct {
		name   string
		stream string
		want   CriterionResult
	}{
		{"the captured criterion failed", baselineFailed, CriterionFail},
		{"the captured criterion passed", baselinePassed, CriterionPass},
		// The package is red and the criterion is not. Reading the package result
		// would report the captured criterion as failing something it passed.
		{"an unrelated test failed", unrelatedFailed, CriterionPass},
		// No verdict was reached. That is not a failure the criterion proved.
		{"the package would not build", buildFailed, CriterionUnavailable},
		{"nothing ran it", "", CriterionUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BaselineOutcomeFromTestJSON([]byte(tc.stream), criteria); got != tc.want {
				t.Fatalf("BaselineOutcomeFromTestJSON = %q, want %q", got, tc.want)
			}
		})
	}
}

// Only kinds whose running a plain `go test` establishes are evaluated. The
// capture stays wider on purpose; the evidence does not.
func TestOnlyCriteriaAPlainRunExecutesAreEvaluable(t *testing.T) {
	src := []byte(`package p

import "testing"

func TestReal(t *testing.T)          {}
func BenchmarkThing(b *testing.B)    {}
func ExampleThing()                  {}
func FuzzThing(f *testing.F)         {}
`)
	got := EvaluableCriterionNames(src)
	if len(got) != 1 || got[0] != "TestReal" {
		t.Fatalf("EvaluableCriterionNames = %v, want only the test", got)
	}
}

func factFor(c TestCriterion, p CriterionProvenance, e *BaselineEvidence) BaselineTestFact {
	return BaselineTestFact{Criterion: c, Provenance: p, Evaluation: e}
}

func evidenceFor(c TestCriterion, epoch uint64, r CriterionResult) *BaselineEvidence {
	return &BaselineEvidence{Criterion: c, StateEpoch: epoch, Result: r, Backend: "go_overlay"}
}

// The four shapes this layer exists to tell apart.
func TestBaselineObligationsAnswerOnlyToTheCapturedDigest(t *testing.T) {
	store := storeIn(t)
	baseline := capture(t, store, "cache_test.go", realInvariant)
	rewritten := TestCriterion{LogicalID: "cache_test.go", Digest: DigestOf([]byte(assertsTrue))}

	cases := []struct {
		name    string
		fact    BaselineTestFact
		wantOwe bool
	}{
		{
			// The attack: the workspace test was rewritten to assert nothing and
			// is green, while the captured criterion fails against the same code.
			name:    "a green rewritten test beside a failing captured one",
			fact:    factFor(baseline, CriterionRewritten, evidenceFor(baseline, 7, CriterionFail)),
			wantOwe: true,
		},
		{
			name:    "the captured criterion passed",
			fact:    factFor(baseline, CriterionRewritten, evidenceFor(baseline, 7, CriterionPass)),
			wantOwe: false,
		},
		{
			// A rename can leave the captured criterion unable to build. That is
			// not a failure it proved, and not permission to drop it.
			name:    "the captured criterion could not run",
			fact:    factFor(baseline, CriterionRewritten, evidenceFor(baseline, 7, CriterionUnavailable)),
			wantOwe: true,
		},
		{
			// Evidence for the bytes that replaced it is evidence about a
			// different criterion, whatever the file is called.
			name:    "a pass recorded against the rewritten bytes",
			fact:    factFor(baseline, CriterionRewritten, evidenceFor(rewritten, 7, CriterionPass)),
			wantOwe: true,
		},
		{
			name:    "a pass against a state the criterion never ran on",
			fact:    factFor(baseline, CriterionUnchanged, evidenceFor(baseline, 6, CriterionPass)),
			wantOwe: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owed := BaselineTestObligations([]BaselineTestFact{tc.fact}, 7)
			if got := len(owed) == 1; got != tc.wantOwe {
				t.Fatalf("obligations = %+v, want owed = %v", owed, tc.wantOwe)
			}
			if tc.wantOwe && owed[0].Kind != ObligationBaselineTest {
				t.Fatalf("kind = %q, want the baseline criterion", owed[0].Kind)
			}
		})
	}
}

// No result here is permission to drop the criterion — the host records what it
// found and asks for nothing it cannot read off the code.
func TestBaselineObligationsNeverGrantSupersession(t *testing.T) {
	store := storeIn(t)
	baseline := capture(t, store, "cache_test.go", realInvariant)
	for _, result := range []CriterionResult{CriterionFail, CriterionUnavailable} {
		fact := factFor(baseline, CriterionRemoved, evidenceFor(baseline, 2, result))
		owed := BaselineTestObligations([]BaselineTestFact{fact}, 2)
		if len(owed) != 1 {
			t.Fatalf("result %q: obligations = %+v, want it still owed", result, owed)
		}
		if strings.Contains(owed[0].Discharge, "supersed") {
			t.Fatalf("discharge = %q, want no supersession offered here", owed[0].Discharge)
		}
	}
}
