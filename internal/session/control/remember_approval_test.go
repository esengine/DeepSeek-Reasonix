package control

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/memory"
)

// TestAllowRememberByScopeFollowsTheSwitchAndTheScope is the switch's contract:
// each answer covers its own scope only, both off keeps the dialog, and the
// scope comes from the call rather than from a guess.
func TestAllowRememberByScopeFollowsTheSwitchAndTheScope(t *testing.T) {
	root := testenv.TempDir(t)
	c := &Controller{controllerDeps: controllerDeps{
		memory: newMemoryManager(&memory.Set{Store: memory.Store{
			Dir: filepath.Join(root, "project"), GlobalDir: filepath.Join(root, "global"),
		}}),
	}}

	project := json.RawMessage(`{"name":"fact","type":"project","scope":"project","description":"d","body":"b"}`)
	global := json.RawMessage(`{"name":"fact","type":"project","scope":"global","description":"d","body":"b"}`)
	unsaid := json.RawMessage(`{"name":"fact","type":"project","description":"d","body":"b"}`)

	if c.allowRememberByScope(project).AutoAllow || c.allowRememberByScope(global).AutoAllow {
		t.Fatal("both switches off: nothing may pass without asking")
	}

	c.autoConfirmProjectRemember = true
	if !c.allowRememberByScope(project).AutoAllow || !c.allowRememberByScope(unsaid).AutoAllow {
		t.Fatal("project switch on: a project write must pass")
	}
	if c.allowRememberByScope(global).AutoAllow {
		t.Fatal("project switch must not cover a global write")
	}
}
