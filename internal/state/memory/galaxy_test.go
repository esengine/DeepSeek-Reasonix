package memory

import (
	"strings"
	"testing"
	"time"
)

func TestNewFieldsRoundTripThroughTheFile(t *testing.T) {
	m := Memory{
		ID: "mem_1", Revision: 3, Name: "ship-rule", Description: "d", Type: TypeConstraint,
		Scope: FactScopeProject, Body: "never ship on a friday",
		Galaxy: "  release   process ", Importance: 3, Strength: 54.5, LastUsedAt: time.Now(), Uses: 6,
	}
	out := render(m, "ship-rule")
	got := parseMemory("ship-rule.md", []byte(out))
	if got.Type != TypeConstraint || got.Galaxy != "release process" || got.Importance != 3 {
		t.Fatalf("round trip lost a field: %+v", got)
	}
	if strings.Contains(out, "strength") || strings.Contains(out, "uses") || strings.Contains(out, "last_used") {
		t.Fatalf("usage state belongs in the usage file, not the fact file:\n%s", out)
	}
}

func TestAFileWithoutTheNewFieldsGetsDefaultsNotZeroes(t *testing.T) {
	old := "---\nname: old\ndescription: d\nmetadata:\n  type: feedback\n  scope: project\n---\n\nbody\n"
	m := parseMemory("old.md", []byte(old))
	if m.Galaxy != "" || m.Importance != 0 || m.Strength != 0 || m.Uses != 0 || !m.LastUsedAt.IsZero() {
		t.Fatalf("an old file invented values: %+v", m)
	}
	if m.ResolvedImportance() != ImportanceNormal || m.StrengthDays() != 30 {
		t.Fatalf("defaults = importance %d strength %v, want 2 and 30", m.ResolvedImportance(), m.StrengthDays())
	}
}

func TestInitialStrengthFollowsImportanceAndVolatility(t *testing.T) {
	for _, tc := range []struct {
		m    Memory
		want float64
	}{
		{Memory{Type: TypeProject, Importance: 1}, 7},
		{Memory{Type: TypeProject, Importance: 2}, 30},
		{Memory{Type: TypeProject, Importance: 3}, 120},
		{Memory{Type: TypeReference}, 7},
		{Memory{Type: TypeProject, Volatility: VolatilityStable}, 120},
		{Memory{Type: TypeProject, Volatility: VolatilityVolatile}, 14},
		{Memory{Type: TypeProject, Strength: 54}, 54},
	} {
		if got := tc.m.StrengthDays(); got != tc.want {
			t.Errorf("%+v strength = %v, want %v", tc.m, got, tc.want)
		}
	}
}

func TestConstraintsPinnedAndEvergreenNeverDecay(t *testing.T) {
	for name, m := range map[string]Memory{
		"constraint": {Type: TypeConstraint},
		"pinned":     {Type: TypeProject, Activation: ActivationPinned},
		"evergreen":  {Type: TypeProject, Volatility: VolatilityEvergreen},
	} {
		if !m.NoDecay() {
			t.Errorf("%s should not decay", name)
		}
	}
	if (Memory{Type: TypeProject}).NoDecay() {
		t.Error("an ordinary project fact must decay")
	}
}

func TestAnUpdateThatOmitsTheNewFieldsKeepsThem(t *testing.T) {
	existing := Memory{Galaxy: "release", Importance: 3}
	got := inheritOnUpdate(Memory{}, existing, false)
	if got.Galaxy != "release" || got.Importance != 3 {
		t.Fatalf("an update dropped state: %+v", got)
	}
	got = inheritOnUpdate(Memory{Galaxy: "other", Importance: 1}, existing, false)
	if got.Galaxy != "other" || got.Importance != 1 {
		t.Fatalf("an explicit value was overridden: %+v", got)
	}
}

func TestConstraintStaysReadableByAnOlderBinary(t *testing.T) {
	out := render(Memory{Name: "c", Description: "d", Type: TypeConstraint, Scope: FactScopeProject, Body: "b"}, "c")
	if !strings.Contains(out, "type: project") || !strings.Contains(out, "fact_type: constraint") {
		t.Fatalf("an older binary would misfile a constraint:\n%s", out)
	}
}

func TestGalaxyNamesAreTrimmedAndBounded(t *testing.T) {
	if got := NormalizeGalaxy(strings.Repeat("星", 100)); len([]rune(got)) != maxGalaxyRunes {
		t.Fatalf("galaxy length = %d", len([]rune(got)))
	}
	if NormalizeImportance(9) != 0 || NormalizeImportance(0) != 0 || NormalizeImportance(2) != 2 {
		t.Fatal("importance outside 1..3 must read as unset")
	}
}
