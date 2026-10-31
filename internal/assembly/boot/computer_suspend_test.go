package boot

import (
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/tools/builtin"
)

// computerToolNames lists the computer-use tool names present in a registry.
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

// A host with no computer-use helper must not offer the computer tools at all:
// `addBuiltins` seeds every built-in first, so leaving them in means the model
// sees them offered, calls one, and is told computer use runs elsewhere. The
// driver is an argument rather than a package variable, so "no helper" is a nil.
func TestBindMachineToolsKeepsComputerOutOfTheSchemaWithoutAHelper(t *testing.T) {
	reg := tool.NewRegistry()
	for _, t := range builtin.ComputerTools(nil) {
		reg.Add(t)
	}
	if got := computerToolNames(reg); len(got) != 2 {
		t.Fatalf("computer tools seeded = %v, want both in the registry first", got)
	}

	bindMachineTools(reg, &config.Config{}, "/tmp/workspace", "workspace", nil, nil)

	if got := computerToolNames(reg); len(got) != 0 {
		t.Fatalf("computer tools still offered on a host with no helper: %v", got)
	}
}

// And they stay out: a later Add — an MCP handshake, a preset binding its own
// set — must not put the unbound copies back.
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
