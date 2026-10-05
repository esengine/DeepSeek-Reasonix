package builtin

import (
	"testing"

	"reasonix/internal/contract/tool"
)

// pathWriters are the built-ins whose contract is writing the paths their own
// arguments name. Receipt.Write is read by the delivery gates, the baseline and
// the cross-turn diff evidence, so one that stops declaring PathWriter keeps
// working while its writes stop being attributable to a file.
var pathWriters = map[string]bool{
	"write_file": true, "edit_file": true, "multi_edit": true, "move_file": true,
	"notebook_edit": true, "delete_range": true, "delete_symbol": true,
}

func TestWritersDeclareThePathsTheyWrite(t *testing.T) {
	found := map[string]bool{}
	for _, x := range tool.Builtins() {
		if !pathWriters[x.Name()] {
			continue
		}
		found[x.Name()] = true
		if !tool.WritesNamedPaths(x) {
			t.Errorf("%s stopped declaring PathWriter: its writes are no longer attributable to the paths it names", x.Name())
		}
	}
	for name := range pathWriters {
		if !found[name] {
			t.Errorf("%s is not a registered built-in; this list describes a tool that no longer exists", name)
		}
	}
}

// A tool that mutates without naming what it touched must not claim otherwise:
// bash is the standing example, and crediting it with named paths would attach
// a write to whatever path happened to appear in its arguments.
func TestOpaqueMutatorsClaimNoNamedPaths(t *testing.T) {
	seen := 0
	for _, x := range tool.Builtins() {
		switch x.Name() {
		case "bash", "kill_shell":
			seen++
			if tool.WritesNamedPaths(x) {
				t.Errorf("%s declares PathWriter, but nothing in its arguments says which files it changed", x.Name())
			}
		}
	}
	if seen != 2 {
		t.Fatalf("found %d of 2 opaque mutators; the registry is not populated here", seen)
	}
}

// PathWriter is about attribution, not permission: a read-only tool declaring
// it would record writes for a call that changes nothing.
func TestOnlyWritersDeclarePathWriter(t *testing.T) {
	for _, x := range tool.Builtins() {
		if tool.WritesNamedPaths(x) && x.ReadOnly() {
			t.Errorf("%s is ReadOnly and still declares PathWriter", x.Name())
		}
	}
}
