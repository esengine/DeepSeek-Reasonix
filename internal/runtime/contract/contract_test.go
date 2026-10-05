package contract

import (
	"context"
	"errors"
	"slices"
	"testing"

	"reasonix/internal/state/trustedstate"
)

func TestDeriveIsCanonical(t *testing.T) {
	a := Derive(Sources{Checks: []string{"go test ./...", "  ", "go vet ./..."}, Tests: []string{"baseline_test@x@1"}})
	b := Derive(Sources{Checks: []string{"go vet ./...", "go test ./...", "go test ./..."}, Tests: []string{"baseline_test@x@1"}})
	if !slices.EqualFunc(a, b, Criterion.Equal) || len(a) != 3 {
		t.Fatalf("derivations differ or kept blanks/duplicates:\n%v\n%v", a, b)
	}
	if (Contract{Criteria: a}).Digest() != (Contract{Criteria: b}).Digest() {
		t.Fatal("equal criteria encode differently")
	}
}

func TestHostPolicyAcceptsOnlyTightening(t *testing.T) {
	first, dec := Accept(nil, "", Derive(Sources{Checks: []string{"A"}}), "task-1")
	if dec != Accepted || first.Revision != 1 || first.ID != "task-1" || first.AcceptedBy != PolicyTemplate {
		t.Fatalf("first = %+v, %s", first, dec)
	}
	same, dec := Accept(&first, "rec-1", Derive(Sources{Checks: []string{"A"}}), "ignored")
	if dec != Unchanged || same.Revision != 1 {
		t.Fatalf("same derivation = %+v, %s", same, dec)
	}
	tighter, dec := Accept(&first, "rec-1", Derive(Sources{Checks: []string{"A"}, Tests: []string{"T"}}), "ignored")
	if dec != Tightened || tighter.Revision != 2 || tighter.Parent != "rec-1" || tighter.ID != "task-1" {
		t.Fatalf("tightening = %+v, %s", tighter, dec)
	}
	for _, relaxed := range [][]Criterion{
		Derive(Sources{}),
		Derive(Sources{Checks: []string{"B"}}),
		Derive(Sources{Checks: []string{"B"}, Tests: []string{"T"}}),
	} {
		kept, dec := Accept(&tighter, "rec-2", relaxed, "ignored")
		if dec != RelaxationRefused || kept.Digest() != tighter.Digest() {
			t.Fatalf("relaxation %v = %+v, %s; want the current revision kept", relaxed, kept, dec)
		}
	}
}

func TestSealAndLoadRoundTrip(t *testing.T) {
	store := trustedstate.Open(t.TempDir(), nil)
	c, _ := Accept(nil, "", Derive(Sources{Checks: []string{"A"}}), "task-1")
	rec, err := Seal(context.Background(), store, "ws", c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Load(store, rec)
	if err != nil || got.Digest() != c.Digest() {
		t.Fatalf("Load = %+v, %v", got, err)
	}
}

func TestLoadRefusesARecordOfAnotherKind(t *testing.T) {
	store := trustedstate.Open(t.TempDir(), nil)
	_, d, err := store.Append(context.Background(), "ws", "shadow_bundle/1", []byte(`{"criteria":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(store, string(d)); !errors.Is(err, trustedstate.ErrTampered) {
		t.Fatalf("Load = %v, want ErrTampered", err)
	}
	if _, err := Load(store, "sha256:"+string(make([]byte, 0))); err == nil {
		t.Fatal("a malformed record name loaded")
	}
}

// Plan criteria are positional within one plan, so what a revision freezes is
// the commands the plan names for them: renumbering or rewording the criteria
// between two plans changes nothing, dropping a command is a relaxation.
func TestPlanChecksAreFrozenByCommandIdentity(t *testing.T) {
	first, _ := Accept(nil, "", Derive(Sources{PlanChecks: []string{"go test ./...", "go vet ./..."}}), "t")
	same := Derive(Sources{PlanChecks: []string{"go vet ./...", "go test ./...", "go test ./..."}})
	if _, dec := Accept(&first, "r", same, "t"); dec != Unchanged {
		t.Fatalf("the same commands in another order were %s, want unchanged", dec)
	}
	fewer := Derive(Sources{PlanChecks: []string{"go test ./..."}})
	if _, dec := Accept(&first, "r", fewer, "t"); dec != RelaxationRefused {
		t.Fatalf("dropping a command was %s, want refused", dec)
	}
	dropped := Dropped(&first, fewer)
	if len(dropped) != 1 || dropped[0].Verifier.Identity != "go vet ./..." || dropped[0].Source != SourcePlan {
		t.Fatalf("Dropped = %+v, want the vet check", dropped)
	}
	relaxed := AcceptRelaxation(first, "r", fewer, "user:plan_approval")
	if relaxed.Revision != 2 || relaxed.Parent != "r" || relaxed.AcceptedBy != "user:plan_approval" || len(relaxed.Criteria) != 1 {
		t.Fatalf("relaxed = %+v", relaxed)
	}
}

// A plan naming files to touch makes a proven change a deliverable. Losing it
// — a later plan naming no files — is a relaxation like any other.
func TestPlanDeliverableIsACriterion(t *testing.T) {
	with := Derive(Sources{PlanChanges: true})
	if len(with) != 1 || with[0].ID != PlanDeliverable || with[0].Verifier.Kind != VerifierChange || !with[0].Required {
		t.Fatalf("Derive = %+v, want the change deliverable", with)
	}
	first, _ := Accept(nil, "", with, "t")
	if _, dec := Accept(&first, "r", Derive(Sources{}), "t"); dec != RelaxationRefused {
		t.Fatalf("dropping the deliverable was %s, want refused", dec)
	}
}
