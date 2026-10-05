package boot

import (
	"encoding/json"
	"reasonix/internal/contract/ablation"
	"sort"
	"testing"

	"reasonix/internal/base/tokencount"
	"reasonix/internal/contract/tool"
)

// What a session is billed for before it does anything: the schemas the
// provider is actually shown. builtin's own budget guards the whole built-in
// set, and the two move independently — a tool promoted out of use_capability
// onto the core surface costs every session without touching that number.
const visibleSchemaTokenBudget = 3708

func TestProviderVisibleSchemaSurfaceStaysWithinBudget(t *testing.T) {
	// goalTurnsUnreachable=false is the larger surface: it keeps update_goal.
	// Guarding the smaller one would let the worst case grow unwatched.
	reg := tool.NewRegistry()
	for _, x := range tool.Builtins() {
		reg.Add(x)
	}
	applyUnifiedProviderToolSurface(reg, false, ablation.Set{}, nil)

	type row struct {
		name string
		tok  int
	}
	var rows []row
	total := 0
	for _, s := range reg.Schemas() {
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("%s: %v", s.Name, err)
		}
		n := tokencount.Text(string(b))
		rows = append(rows, row{s.Name, n})
		total += n
	}
	// An empty or unrestricted surface would pass any ceiling for the wrong reason.
	if len(rows) < 8 {
		t.Fatalf("only %d schemas are provider-visible; the surface is not being applied here", len(rows))
	}
	if len(rows) >= len(tool.Builtins()) {
		t.Fatalf("every built-in is provider-visible (%d of %d); the allow-list did not restrict anything",
			len(rows), len(tool.Builtins()))
	}
	if total > visibleSchemaTokenBudget {
		sort.Slice(rows, func(i, j int) bool { return rows[i].tok > rows[j].tok })
		t.Errorf("provider-visible schemas are %d tokens, over the %d budget by %d.\n"+
			"Every session pays this in its prefix. Raise it only with a reason.\nLargest:", total, visibleSchemaTokenBudget, total-visibleSchemaTokenBudget)
		for i, x := range rows {
			if i >= 6 {
				break
			}
			t.Errorf("  %5d  %s", x.tok, x.name)
		}
	}
}
