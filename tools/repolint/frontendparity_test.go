package main

import (
	"strings"
	"testing"
)

// The fixture mirrors the tree's own shape: two frontends on the full port, one
// on a narrower composite, so a capability can be out of reach for a reason
// port.go already compiles.
var testPorts = []frontendPort{
	{pkg: "internal/frontend/acp", port: "EditorAPI"},
	{pkg: "internal/frontend/cli", port: "SessionAPI"},
	{pkg: "internal/frontend/serve", port: "SessionAPI"},
}

const (
	acpPkg   = "internal/frontend/acp"
	cliPkg   = "internal/frontend/cli"
	servePkg = "internal/frontend/serve"
)

// parityOf builds what the type check would have produced: capabilities the
// controller exports, the ones each frontend's port carries, and the ones it
// calls.
func parityOf(caps []string, reachable, consumed map[string][]string) parityMatrix {
	m := parityMatrix{reachable: map[string]map[string]bool{}, consumed: map[string]map[string]bool{}}
	for i, name := range caps {
		m.capabilities = append(m.capabilities, capability{name, "internal/session/control/controller.go", i + 1})
	}
	for pkg, names := range reachable {
		m.reachable[pkg] = nameSet(names)
	}
	for pkg, names := range consumed {
		m.consumed[pkg] = nameSet(names)
	}
	return m
}

func nameSet(names []string) map[string]bool {
	out := map[string]bool{}
	for _, n := range names {
		out[n] = true
	}
	return out
}

func everyPort(names ...string) map[string][]string {
	return map[string][]string{acpPkg: names, cliPkg: names, servePkg: names}
}

func parityFor(t *testing.T, m parityMatrix, scopes ...frontendScope) []Finding {
	t.Helper()
	return frontendParityFindings(m, testPorts, scopes, 40)
}

func TestACapabilityEveryFrontendDrivesIsNotDebt(t *testing.T) {
	m := parityOf([]string{"Cancel"}, everyPort("Cancel"), everyPort("Cancel"))
	if got := parityFor(t, m); len(got) != 0 {
		t.Fatalf("a fully wired capability reported as debt: %v", got)
	}
}

// The defect the rule was written for: one frontend drives it, the other two
// are handed it and never call it.
func TestACapabilityOnlyOneFrontendDrivesIsTwoMissingEdges(t *testing.T) {
	m := parityOf([]string{"SwitchBranch"}, everyPort("SwitchBranch"), map[string][]string{cliPkg: {"SwitchBranch"}})
	got := parityFor(t, m)
	if len(got) != 1 {
		t.Fatalf("want one row per capability, got %d: %v", len(got), got)
	}
	if got[0].Weight != 2 {
		t.Fatalf("the ratchet counts edges, not rows: weight %d, want 2", got[0].Weight)
	}
	if want := "Controller.SwitchBranch: acp=no cli=yes serve=no"; got[0].Msg != want {
		t.Fatalf("report is the diagnosis:\n got %q\nwant %q", got[0].Msg, want)
	}
}

func TestTwoOfThreeWiredLeavesOneMissingEdge(t *testing.T) {
	m := parityOf([]string{"Compact"}, everyPort("Compact"),
		map[string][]string{cliPkg: {"Compact"}, servePkg: {"Compact"}})
	got := parityFor(t, m)
	if len(got) != 1 || got[0].Weight != 1 {
		t.Fatalf("want one row weighing 1, got %v", got)
	}
	if !strings.Contains(got[0].Msg, "acp=no") || strings.Contains(got[0].Msg, "serve=no") {
		t.Fatalf("the row must name which frontends are missing: %q", got[0].Msg)
	}
}

// A controller method no frontend drives is not shared behavior yet. Counting
// it would make every internal helper a three-way parity defect and drown the
// rule in noise on its first run.
func TestAnInternalControllerMethodNoFrontendDrivesIsNotACapability(t *testing.T) {
	m := parityOf([]string{"SnapshotWithDurability"}, everyPort("SnapshotWithDurability"), nil)
	if got := parityFor(t, m); len(got) != 0 {
		t.Fatalf("a method no frontend calls reported as debt: %v", got)
	}
}

// A frontend whose port does not carry the capability is out of scope by a
// decision port.go already makes and the compiler already keeps.
func TestAPortThatDoesNotCarryTheCapabilityIsNotDebt(t *testing.T) {
	m := parityOf([]string{"SwitchBranch"},
		map[string][]string{cliPkg: {"SwitchBranch"}, servePkg: {"SwitchBranch"}},
		map[string][]string{cliPkg: {"SwitchBranch"}})
	got := parityFor(t, m)
	if len(got) != 1 || got[0].Weight != 1 {
		t.Fatalf("only serve can be missing here, got %v", got)
	}
	if want := "Controller.SwitchBranch: acp=n/a cli=yes serve=no"; got[0].Msg != want {
		t.Fatalf("out-of-port frontends must read n/a:\n got %q\nwant %q", got[0].Msg, want)
	}
}

// Known boundary: the row is per method, not per user-visible capability. Two
// frontends reaching one feature through different controller methods read as a
// gap, which is the answer this rule can defend from structure alone.
func TestTheSameFeatureReachedByADifferentMethodStillReadsAsAGap(t *testing.T) {
	m := parityOf([]string{"BranchTreeText", "Branches"}, everyPort("BranchTreeText", "Branches"),
		map[string][]string{servePkg: {"BranchTreeText"}, cliPkg: {"Branches"}})
	if got := parityFor(t, m); len(got) != 2 {
		t.Fatalf("want a row for each method, got %v", got)
	}
}

func TestAScopedRowStopsBeingAMissingEdgeAndBecomesTableDebt(t *testing.T) {
	m := parityOf([]string{"Reload"}, everyPort("Reload"), map[string][]string{cliPkg: {"Reload"}})
	scope := frontendScope{"Reload", servePkg, "#1", "the HTTP server reloads on its own generation swap"}
	got := parityFor(t, m, scope)
	if len(got) != 2 {
		t.Fatalf("want the capability row and the table row, got %v", got)
	}
	var edges, table Finding
	for _, f := range got {
		if f.File == frontendScopeFile {
			table = f
		} else {
			edges = f
		}
	}
	if edges.Weight != 1 || !strings.Contains(edges.Msg, "serve=scoped") {
		t.Fatalf("the scoped frontend must leave the missing count and read scoped: %+v", edges)
	}
	if table.Weight != 1 || table.Line != 40 {
		t.Fatalf("the row must cost one against the table's own budget, at the table: %+v", table)
	}
	// Total is unchanged: declaring a scope moves debt into a file a reviewer
	// opens, and never deletes it.
	if edges.Weight+table.Weight != 2 {
		t.Fatalf("scoping changed the total debt: %d", edges.Weight+table.Weight)
	}
}

func TestAScopeRowWithoutAnIssueOrReasonIsMalformed(t *testing.T) {
	for _, bad := range []frontendScope{
		{"Reload", servePkg, "", "because"},
		{"Reload", servePkg, "#1", "   "},
		{"Reload", "internal/nope", "#1", "because"},
		{"", servePkg, "#1", "because"},
	} {
		if err := validateFrontendScopes([]frontendScope{bad}, testPorts); err == nil {
			t.Fatalf("accepted a malformed row: %+v", bad)
		}
	}
	ok := frontendScope{"Reload", servePkg, "#1", "the HTTP server reloads on its own generation swap"}
	if err := validateFrontendScopes([]frontendScope{ok}, testPorts); err != nil {
		t.Fatalf("rejected a complete row: %v", err)
	}
}

func TestTheShippedScopeTableIsWellFormed(t *testing.T) {
	if err := validateFrontendScopes(frontendScopes, frontendPorts); err != nil {
		t.Fatal(err)
	}
}

// A scoped row has to send the reader to the rows, not to the top of the file.
func TestScopeRowsReportAtTheTable(t *testing.T) {
	if line := scopeDeclLine("../.."); line <= 1 {
		t.Fatalf("frontendScopes not located in %s: line %d", frontendScopeFile, line)
	}
}

func TestRecordedParityDebtDoesNotFail(t *testing.T) {
	b := budget(map[string]map[string]int{"internal/session/control/controller.go": {ruleFrontendParity: 3}},
		map[string]int{ruleFrontendParity: 3})
	_, msgs := b.exceeded([]Finding{{"internal/session/control/controller.go", 1, ruleFrontendParity, "", 3}})
	if len(msgs) != 0 {
		t.Fatalf("baselined parity debt reported as new: %v", msgs)
	}
}

// The gate's whole purpose: a capability that gains a frontend it does not
// reach must fail, even though the capability's row already existed.
func TestOneMoreMissingEdgeOnAnAlreadyBaselinedCapabilityFails(t *testing.T) {
	b := budget(map[string]map[string]int{"internal/session/control/controller.go": {ruleFrontendParity: 3}},
		map[string]int{ruleFrontendParity: 3})
	_, msgs := b.exceeded([]Finding{{"internal/session/control/controller.go", 1, ruleFrontendParity, "", 4}})
	if len(msgs) == 0 {
		t.Fatal("a fourth missing edge under a budget of three must fail")
	}
}

func TestANewCapabilityWiredToOneFrontendFails(t *testing.T) {
	b := budget(map[string]map[string]int{}, map[string]int{ruleFrontendParity: 0})
	over, msgs := b.exceeded([]Finding{
		{"internal/session/control/goal.go", 12, ruleFrontendParity, "Controller.SetGoal: acp=no cli=yes serve=no", 2}})
	if len(msgs) == 0 || len(over) != 1 {
		t.Fatalf("a capability landing in an unbaselined file must fail: over=%v msgs=%v", over, msgs)
	}
}

// Wiring a frontend pays debt down, and the surplus has to be reported: budget
// left on a file is room the gap can reopen into without anyone noticing.
func TestWiringAFrontendLeavesReclaimableBudget(t *testing.T) {
	b := budget(map[string]map[string]int{"internal/session/control/controller.go": {ruleFrontendParity: 3}},
		map[string]int{ruleFrontendParity: 3})
	paid := []Finding{{"internal/session/control/controller.go", 1, ruleFrontendParity, "", 2}}
	if slack := reclaimable(b, paid); slack[ruleFrontendParity] != 1 {
		t.Fatalf("paying one edge must free one unit of budget: %v", slack)
	}
	next := baselineFrom(paid)
	if w := widenings(b, next); len(w) != 0 {
		t.Fatalf("tightening a ratchet needs no ceremony: %v", w)
	}
	if next.Limits[ruleFrontendParity] != 2 {
		t.Fatalf("-update must record the lower ceiling, got %d", next.Limits[ruleFrontendParity])
	}
}

func TestRaisingTheParityCeilingIsRefusedWithoutAllowWiden(t *testing.T) {
	b := budget(map[string]map[string]int{"internal/session/control/controller.go": {ruleFrontendParity: 2}},
		map[string]int{ruleFrontendParity: 2})
	next := baselineFrom([]Finding{{"internal/session/control/controller.go", 1, ruleFrontendParity, "", 3}})
	if w := widenings(b, next); len(w) == 0 {
		t.Fatal("carrying a new missing edge into the baseline must be asked for")
	}
}

// Boundary, measured and left open: a capability whose last consumer goes away
// leaves the candidate set, so its row retires and the debt falls rather than
// failing. A port that declares a capability nobody drives is a dead capability
// — 39 of them today, 122 edges — and that is orphan's shape, not parity's;
// orphan cannot see it because it counts functions and never methods.
func TestLosingTheLastConsumerRetiresTheRowInsteadOfFailing(t *testing.T) {
	wired := parityOf([]string{"SwitchBranch"}, everyPort("SwitchBranch"),
		map[string][]string{cliPkg: {"SwitchBranch"}})
	if got := parityFor(t, wired); len(got) != 1 || got[0].Weight != 2 {
		t.Fatalf("one consumer must leave two missing edges: %v", got)
	}
	orphaned := parityOf([]string{"SwitchBranch"}, everyPort("SwitchBranch"), nil)
	if got := parityFor(t, orphaned); len(got) != 0 {
		t.Fatalf("this rule stops measuring a capability nobody drives; it must not\n"+
			"start half-measuring it either: %v", got)
	}
}
