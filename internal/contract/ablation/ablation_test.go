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
		{"all", "no-evidence+no-planner+no-subagent+no-retrieval+no-compaction+no-upstream+no-recall-search"},
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

// The index axis is not a module, but a run that moved it is not the control
// arm either: results keyed by arm name would otherwise merge with it.
func TestFoldIndexScaleNamesItsOwnArm(t *testing.T) {
	var s Set
	if got := s.WithFoldIndex(FoldIndexQuarter).Arm(); got != "index-quarter" {
		t.Errorf("Arm() = %q, want index-quarter", got)
	}
	if got := New(Evidence).WithFoldIndex(FoldIndexOff).Arm(); got != "no-evidence+index-off" {
		t.Errorf("Arm() = %q, want no-evidence+index-off", got)
	}
	if s.WithFoldIndex(FoldIndexQuarter).Empty() {
		t.Error("a quartered index reported itself as the control arm")
	}
	if !s.WithFoldIndex(FoldIndexDefault).Empty() {
		t.Error("the default scale is the control arm")
	}
}

func TestFoldIndexScaleRatios(t *testing.T) {
	for _, tc := range []struct {
		spec  string
		scale FoldIndexScale
		ratio float64
	}{
		{"", FoldIndexDefault, 1}, {"default", FoldIndexDefault, 1},
		{"half", FoldIndexHalf, 0.5}, {"QUARTER", FoldIndexQuarter, 0.25},
		{"off", FoldIndexOff, 0}, {"0", FoldIndexOff, 0},
	} {
		got, err := ParseFoldIndexScale(tc.spec)
		if err != nil {
			t.Fatalf("ParseFoldIndexScale(%q): %v", tc.spec, err)
		}
		if got != tc.scale || got.Ratio() != tc.ratio {
			t.Errorf("ParseFoldIndexScale(%q) = %q (ratio %v), want %q (%v)", tc.spec, got, got.Ratio(), tc.scale, tc.ratio)
		}
	}
	if _, err := ParseFoldIndexScale("0.37"); err == nil {
		t.Fatal("a free-form fraction was accepted; the axis is four steps on purpose")
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
