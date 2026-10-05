package config

import (
	"path/filepath"
	"testing"
)

func trustStore(t *testing.T) (*ActivationStore, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	return DefaultActivationStore(), t.TempDir()
}

// A .mcp.json is checked in, so whoever wrote the repo chose the command. It
// used to start on the next session in that folder with nothing asked.
func TestARepositoryDeclaredServerDoesNotStartOnItsOwn(t *testing.T) {
	store, root := trustStore(t)
	for _, source := range []MCPConfigSource{MCPSourceProjectMCPJSON, MCPSourceProjectConfig} {
		entry := PluginEntry{Name: "codegraph", Command: "codegraph", Source: source}
		enabled, err := store.IsEnabled(entry, root)
		if err != nil {
			t.Fatal(err)
		}
		if enabled {
			t.Fatalf("%s started with nobody having answered for it", source)
		}
		if !store.AwaitingDecision(entry, root) {
			t.Fatalf("%s is off but not reported as awaiting a decision", source)
		}
	}
}

// Off because nobody answered and off because the user said no are different
// facts, and the answer sticks either way.
func TestAnAnsweredServerStopsWaiting(t *testing.T) {
	store, root := trustStore(t)
	entry := PluginEntry{Name: "codegraph", Command: "codegraph", Source: MCPSourceProjectMCPJSON}

	if err := store.SetServerEnabled(entry, root, ActivationProject, true); err != nil {
		t.Fatal(err)
	}
	if enabled, _ := store.IsEnabled(entry, root); !enabled {
		t.Fatal("an approved server did not start")
	}
	if store.AwaitingDecision(entry, root) {
		t.Fatal("an approved server is still reported as waiting")
	}

	if err := store.SetServerEnabled(entry, root, ActivationProject, false); err != nil {
		t.Fatal(err)
	}
	if enabled, _ := store.IsEnabled(entry, root); enabled {
		t.Fatal("a refused server started")
	}
	if store.AwaitingDecision(entry, root) {
		t.Fatal("a refused server is reported as waiting, which would ask again")
	}
}

// The gate is about who wrote the file. A server from the user's own config —
// including the Claude scopes, which only they write — still follows auto_start.
func TestAServerFromTheUsersOwnFilesStillFollowsAutoStart(t *testing.T) {
	store, root := trustStore(t)
	for _, source := range []MCPConfigSource{
		MCPSourceUserConfig, MCPSourceClaudeLocal, MCPSourceClaudeUser, MCPSourcePluginPackage,
	} {
		entry := PluginEntry{Name: "mine", Command: "mine", Source: source}
		if enabled, err := store.IsEnabled(entry, root); err != nil || !enabled {
			t.Fatalf("%s = %v (%v), want it to start", source, enabled, err)
		}
		if store.AwaitingDecision(entry, root) {
			t.Fatalf("%s asks for an answer the user already gave by writing the file", source)
		}
		off := false
		entry.AutoStart = &off
		if enabled, _ := store.IsEnabled(entry, root); enabled {
			t.Fatalf("%s ignored auto_start=false", source)
		}
	}
}

// The decision is per project: approving a repo server in one folder must not
// answer for a same-named one somewhere else.
func TestApprovalDoesNotTravelToAnotherProject(t *testing.T) {
	store, root := trustStore(t)
	entry := PluginEntry{Name: "codegraph", Command: "codegraph", Source: MCPSourceProjectMCPJSON}
	if err := store.SetServerEnabled(entry, root, ActivationProject, true); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(t.TempDir(), "other")
	if enabled, _ := store.IsEnabled(entry, elsewhere); enabled {
		t.Fatal("an approval in one project started a same-named server in another")
	}
}

// Source is assigned by the merge and carries toml:"-", so a merge that failed
// leaves a project file's own entries with none. Reading that as "not from a
// repo" would open the gate exactly when the config is in a state nobody
// vouched for.
func TestAnEntryOfUnknownProvenanceIsTreatedAsRepositoryDeclared(t *testing.T) {
	store, root := trustStore(t)
	entry := PluginEntry{Name: "orphan", Command: "orphan", Source: MCPSourceUnknown}
	if enabled, _ := store.IsEnabled(entry, root); enabled {
		t.Fatal("an entry with no recorded source started on its own")
	}
	if !store.AwaitingDecision(entry, root) {
		t.Fatal("it is off but not reported as awaiting a decision")
	}
	if DeclaredDefaultOn(entry) {
		t.Fatal("the store-unavailable default would re-enable it")
	}
}

// The pre-change default is what every caller used when the store could not be
// read. Keeping it there would re-enable exactly the servers the gate is for.
func TestTheStoreUnavailableDefaultDoesNotReopenTheGate(t *testing.T) {
	for _, source := range []MCPConfigSource{MCPSourceProjectMCPJSON, MCPSourceProjectConfig, MCPSourceUnknown} {
		if DeclaredDefaultOn(PluginEntry{Name: "x", Source: source}) {
			t.Fatalf("%q would start with the store unreadable", source)
		}
	}
	for _, source := range []MCPConfigSource{MCPSourceUserConfig, MCPSourceClaudeLocal} {
		if !DeclaredDefaultOn(PluginEntry{Name: "x", Source: source}) {
			t.Fatalf("%q would stop working whenever the store cannot be read", source)
		}
	}
}

// ServerOverrideFor pins a repository-declared server to the project layer on
// write. Reading a global row anyway would let one row approve it everywhere.
func TestNoGlobalRowCanApproveARepositoryDeclaredServerEverywhere(t *testing.T) {
	store, root := trustStore(t)
	entry := PluginEntry{Name: "codegraph", Command: "codegraph", Source: MCPSourceProjectMCPJSON}
	global := ServerOverrideFor(entry, root, ActivationGlobal)
	global.Scope, global.Key, global.Enabled = ActivationGlobal, "", true
	if err := store.SetOverride(global); err != nil {
		t.Fatal(err)
	}
	if enabled, _ := store.IsEnabled(entry, root); enabled {
		t.Fatal("a global row approved a repository-declared server")
	}
}
