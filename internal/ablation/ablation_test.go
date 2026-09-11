package ablation

import "testing"

func TestParseAcceptsSpecFormsAndRejectsUnknownModules(t *testing.T) {
	for _, tc := range []struct {
		spec string
		arm  string
	}{
		{"", "full"},
		{"none", "full"},
		{"NONE", "full"},
		{"evidence", "no-evidence"},
		{"planner,evidence", "no-evidence+no-planner"},
		{"planner evidence", "no-evidence+no-planner"},
		{" Evidence , Retrieval ", "no-evidence+no-retrieval"},
		{"all", "no-evidence+no-planner+no-subagent+no-retrieval+no-compaction+no-full-fold"},
	} {
		set, err := Parse(tc.spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.spec, err)
		}
		if got := set.Arm(); got != tc.arm {
			t.Errorf("Parse(%q).Arm() = %q, want %q", tc.spec, got, tc.arm)
		}
	}

	if _, err := Parse("evidence,planer"); err == nil {
		t.Fatal("a misspelled module must fail loudly, not silently run the control arm")
	}
}

func TestArmNameIsIndependentOfSpecOrder(t *testing.T) {
	a, _ := Parse("retrieval,evidence,planner")
	b, _ := Parse("planner,retrieval,evidence")
	if a.Arm() != b.Arm() {
		t.Fatalf("arm names diverge by input order: %q vs %q", a.Arm(), b.Arm())
	}
}

func TestStringRoundTripsThroughParse(t *testing.T) {
	for _, spec := range []string{"none", "evidence", "evidence,compaction", "all"} {
		set, err := Parse(spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", spec, err)
		}
		again, err := Parse(set.String())
		if err != nil {
			t.Fatalf("Parse(%q): %v", set.String(), err)
		}
		if again.Arm() != set.Arm() {
			t.Errorf("%q round-tripped to %q", set.Arm(), again.Arm())
		}
	}
}

func TestZeroValueIsTheControlArm(t *testing.T) {
	var s Set
	if !s.Empty() || s.Arm() != "full" {
		t.Fatalf("zero Set = %q, want the full control arm", s.Arm())
	}
	for _, m := range Modules() {
		if s.Off(m) {
			t.Errorf("zero Set disabled %s", m)
		}
	}
}
