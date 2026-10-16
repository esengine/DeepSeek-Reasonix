package boot

import (
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/tools/builtin"
)

// A host with no computer-use helper must not offer the computer tools at all.
//
// This is the browser check's twin, and it exists for the same reason:
// `addBuiltins` seeds every registered built-in first, so the two computer tools
// are already in the registry by the time `bindMachineTools` declines to bind
// them. Left there, the model sees them offered and calls one, and the call
// answers `computer.unavailable` — "computer use runs only in Reasonix Studio on
// macOS and Windows". Reported from a phone, where that is every call.
//
// The driver is an argument rather than a package variable, so "this host has no
// helper" is a nil at the call site, which is also what the platform that
// reported this passes.
func TestBindMachineToolsKeepsComputerOutOfTheSchemaWithoutAHelper(t *testing.T) {
	reg := tool.NewRegistry()
	for _, t := range builtin.ComputerTools(nil) {
		reg.Add(t)
	}
	if got := computerToolNames(reg); len(got) != 2 {
		t.Fatalf("computer tools seeded = %v, want both in the registry first", got)
	}

	bindMachineTools(reg, &config.Config{}, "/tmp/workspace", nil, nil)

	if got := computerToolNames(reg); len(got) != 0 {
		t.Fatalf("computer tools still offered on a host with no helper: %v", got)
	}
}

// And they stay out: a later Add must not put the unbound copies back.
func TestSuspendedComputerToolsStayOut(t *testing.T) {
	reg := tool.NewRegistry()
	suspendComputerTools(reg)

	for _, t := range builtin.ComputerTools(nil) {
		reg.Add(t)
	}
	if got := computerToolNames(reg); len(got) != 0 {
		t.Fatalf("a later Add put the computer tools back: %v", got)
	}
}

func computerToolNames(reg *tool.Registry) []string {
	want := map[string]bool{}
	for _, t := range builtin.ComputerTools(nil) {
		want[t.Name()] = true
	}
	var out []string
	for _, name := range reg.Names() {
		if want[name] {
			out = append(out, name)
		}
	}
	return out
}
