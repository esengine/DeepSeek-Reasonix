package hook

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"reasonix/internal/config"
)

// A checkout's hooks run on the first event of the first session, so they wait
// for the user and wait again whenever what they would run changes.
func TestProjectHooksWaitForApprovalOfTheirCurrentContent(t *testing.T) {
	home := t.TempDir()
	proj := t.TempDir()
	opts := LoadOptions{ProjectRoot: proj, HomeDir: home}
	script := filepath.Join(proj, "scripts", "start.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("echo one\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeSettings(t, proj, `{"hooks":{"SessionStart":[{"command":"sh scripts/start.sh"}]}}`)

	if got := Load(opts); len(got) != 0 {
		t.Fatalf("unapproved project hooks loaded: %+v", got)
	}
	if _, pending := PendingProjectHooks(opts); !pending {
		t.Fatal("unapproved project hooks not reported as pending")
	}
	insp := Inspect(opts)
	if len(insp.Entries) != 1 || !slices.Contains(insp.Entries[0].Issues, IssueAwaitingApproval) {
		t.Fatalf("inspection = %+v, want the hook marked awaiting approval", insp.Entries)
	}

	approveProjectHooks(t, opts)
	if got := Load(opts); len(got) != 1 {
		t.Fatalf("approved project hooks = %+v, want one", got)
	}

	if err := os.WriteFile(script, []byte("echo two\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := Load(opts); len(got) != 0 {
		t.Fatalf("hooks ran after the script they name changed: %+v", got)
	}
	approveProjectHooks(t, opts)
	writeSettings(t, proj, `{"hooks":{"SessionStart":[{"command":"sh scripts/start.sh --more"}]}}`)
	if got := Load(opts); len(got) != 0 {
		t.Fatalf("hooks ran after their declaration changed: %+v", got)
	}
}

// An approval belongs to one workspace; the same file elsewhere waits again.
func TestProjectHooksApprovalIsPerWorkspace(t *testing.T) {
	home := t.TempDir()
	a, b := t.TempDir(), t.TempDir()
	for _, proj := range []string{a, b} {
		writeSettings(t, proj, sampleSettings)
	}
	approveProjectHooks(t, LoadOptions{ProjectRoot: a, HomeDir: home})
	if got := Load(LoadOptions{ProjectRoot: b, HomeDir: home}); len(got) != 0 {
		t.Fatalf("approval leaked to another workspace: %+v", got)
	}
}

func approveProjectHooks(t *testing.T, opts LoadOptions) {
	t.Helper()
	if err := ApproveProjectHooks(opts); err != nil {
		t.Fatal(err)
	}
}

func TestLoadProjectHooksOnceApproved(t *testing.T) {
	home := t.TempDir()
	proj := t.TempDir()
	writeSettings(t, proj, sampleSettings)
	writeSettings(t, home, `{"hooks":{"PostToolUse":[{"command":"echo g"}]}}`)

	if got := Load(LoadOptions{ProjectRoot: proj, HomeDir: home}); len(got) != 1 || got[0].Scope != ScopeGlobal {
		t.Fatalf("unapproved project hooks loaded: %+v", got)
	}
	approveProjectHooks(t, LoadOptions{ProjectRoot: proj, HomeDir: home})
	got := Load(LoadOptions{ProjectRoot: proj, HomeDir: home})
	if len(got) != 3 {
		t.Fatalf("default load should include project + global, got %d", len(got))
	}
	if got[0].Scope != ScopeProject {
		t.Errorf("project hooks should sort first, got %s", got[0].Scope)
	}
}

// An approval is checked again when the hook fires: a script edited after the
// session loaded is refused, with the reason typed, and never spawned.
func TestApprovedProjectHookIsRefusedAfterItsScriptChanges(t *testing.T) {
	home := t.TempDir()
	proj := t.TempDir()
	opts := LoadOptions{ProjectRoot: proj, HomeDir: home}
	script := filepath.Join(proj, "scripts", "check.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("echo one\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeSettings(t, proj, `{"hooks":{"Stop":[{"command":"sh scripts/check.sh"}]}}`)
	approveProjectHooks(t, opts)
	hooks := Load(opts)
	if len(hooks) != 1 {
		t.Fatalf("approved hooks = %+v", hooks)
	}
	spawned := 0
	spawner := func(context.Context, SpawnInput) SpawnResult { spawned++; return SpawnResult{} }
	if rep := Run(context.Background(), Payload{Event: Stop, Cwd: proj}, hooks, spawner); spawned != 1 || rep.Outcomes[0].Refusal != nil {
		t.Fatalf("unchanged hook: spawned %d, report %+v", spawned, rep)
	}
	if err := os.WriteFile(script, []byte("echo two\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	rep := Run(context.Background(), Payload{Event: Stop, Cwd: proj}, hooks, spawner)
	if spawned != 1 || len(rep.Outcomes) != 1 || !errors.Is(rep.Outcomes[0].Refusal, config.ErrProjectProgramChanged) {
		t.Fatalf("changed hook: spawned %d, report %+v", spawned, rep)
	}
}

// A script missing when the hooks were approved is still covered: creating it
// afterwards is a change, not an approved file.
func TestProjectHookNamingAMissingScriptIsRefusedOnceItAppears(t *testing.T) {
	home := t.TempDir()
	proj := t.TempDir()
	opts := LoadOptions{ProjectRoot: proj, HomeDir: home}
	writeSettings(t, proj, `{"hooks":{"Stop":[{"command":"sh scripts/later.sh"}]}}`)
	approveProjectHooks(t, opts)
	hooks := Load(opts)
	script := filepath.Join(proj, "scripts", "later.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("echo planted\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	spawned := 0
	rep := Run(context.Background(), Payload{Event: Stop, Cwd: proj}, hooks, func(context.Context, SpawnInput) SpawnResult { spawned++; return SpawnResult{} })
	if spawned != 0 || len(rep.Outcomes) != 1 || !errors.Is(rep.Outcomes[0].Refusal, config.ErrProjectProgramChanged) {
		t.Fatalf("hook ran a script created after approval: spawned %d, report %+v", spawned, rep)
	}
}

// A path that climbs out through a link is covered as the OS resolves it, not
// as its spelling cleans up to.
func TestProjectHookThroughALinkAndDotDotIsCovered(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	home := t.TempDir()
	proj := t.TempDir()
	outside := filepath.Join(t.TempDir(), "sub")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(proj, "ln")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(filepath.Dir(outside), "f.sh")
	if err := os.WriteFile(target, []byte("echo one\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	opts := LoadOptions{ProjectRoot: proj, HomeDir: home}
	writeSettings(t, proj, `{"hooks":{"Stop":[{"command":"sh ln/../f.sh"}]}}`)
	approveProjectHooks(t, opts)
	hooks := Load(opts)
	if err := os.WriteFile(target, []byte("echo two\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	spawned := 0
	rep := Run(context.Background(), Payload{Event: Stop, Cwd: proj}, hooks, func(context.Context, SpawnInput) SpawnResult { spawned++; return SpawnResult{} })
	if spawned != 0 || !errors.Is(rep.Outcomes[0].Refusal, config.ErrProjectProgramChanged) {
		t.Fatalf("a changed file reached through ln/.. ran: spawned %d, report %+v", spawned, rep)
	}
}
