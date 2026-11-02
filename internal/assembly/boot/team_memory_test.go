package boot

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/team"
	"reasonix/internal/state/memory"
)

// teamMemorySession builds one session in its own folder, seated as agent when
// agent is set.
func teamMemorySession(t *testing.T, kind, agent string) (*Options, func() []memory.Memory, func(memory.Memory)) {
	t.Helper()
	dir := robustTempDir(t)
	writeFile(t, dir, "reasonix.toml", "default_model = \"test-model\"\n[agent]\nsystem_prompt = \"BASE\"\n[[providers]]\nname = \"test-model\"\nkind = \""+kind+"\"\nmodel = \"x\"\n")
	approveWorkspace(t, dir)
	opts := Options{Home: os.Getenv("REASONIX_HOME"), WorkspaceRoot: dir, resolvedShell: pinnedEffectShell()}
	if agent != "" {
		opts.TeamSeat = seatAt{team.SeatRef{Agent: agent}}
	}
	t.Chdir(dir)
	ctrl, err := Build(context.Background(), opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(ctrl.Close)
	list := func() []memory.Memory {
		got, err := ctrl.Memory().Store.List()
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	save := func(m memory.Memory) {
		if _, err := ctrl.SaveMemory(m); err != nil {
			t.Fatalf("SaveMemory: %v", err)
		}
	}
	return &opts, list, save
}

func named(ms []memory.Memory, name string) bool {
	return slices.ContainsFunc(ms, func(m memory.Memory) bool { return m.Name == name })
}

// An agent keeps one memory whatever folder it works in, and an ordinary
// session in the same folder keeps its project's.
func TestAnAgentRemembersAcrossFolders(t *testing.T) {
	home := isolateConfigHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	rec := &effectRecordingProvider{}
	provider.Register("team-memory", func(provider.Config) (provider.Provider, error) { return rec, nil })
	fact := memory.Memory{Name: "launch-style", Description: "how launch posts read", Type: memory.TypeProject, Body: "Short sentences, no exclamation marks."}

	_, _, saveInFirst := teamMemorySession(t, "team-memory", "a-0123456789abcdef")
	saveInFirst(fact)

	_, listInSecond, _ := teamMemorySession(t, "team-memory", "a-0123456789abcdef")
	if !named(listInSecond(), "launch-style") {
		t.Fatal("the agent lost what it remembered when it moved to another folder")
	}
	_, listOther, _ := teamMemorySession(t, "team-memory", "a-fedcba9876543210")
	if named(listOther(), "launch-style") {
		t.Fatal("one agent's memory reached another agent")
	}
	_, listPlain, _ := teamMemorySession(t, "team-memory", "")
	if named(listPlain(), "launch-style") {
		t.Fatal("an agent's memory reached an ordinary session")
	}
	want := filepath.Join(config.RootsForHome("").TeamDir(), "agents", "a-0123456789abcdef", "memory")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("the agent's memory is not under the team: %v", err)
	}
}
