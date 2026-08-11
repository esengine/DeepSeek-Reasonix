package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/sandbox"
)

// stateRootFor builds a fake Reasonix state root with the two guarded session
// trees populated, returning the root and one file path in each tree.
func stateRootFor(t *testing.T) (root, cliSession, projectSession string) {
	t.Helper()
	root = t.TempDir()
	cliSession = filepath.Join(root, "sessions", "20260707-abc.jsonl")
	projectSession = filepath.Join(root, "projects", "-Users-me-proj", "sessions", "20260707-def.jsonl")
	for _, p := range []string{cliSession, projectSession} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, cliSession, projectSession
}

func TestSessionDataGuardDeniesSessionStores(t *testing.T) {
	root, cliSession, projectSession := stateRootFor(t)
	g := NewSessionDataGuard(root, nil)

	for _, target := range []string{
		cliSession,
		projectSession,
		filepath.Join(root, "sessions", "sub", "new.jsonl"),                     // not-yet-existing file under the store
		filepath.Join(root, "projects", "any-slug", "sessions", "x.jsonl.meta"), // CAS ledger sidecar
	} {
		if err := g.Check(target); err == nil {
			t.Errorf("Check(%q) = nil, want session-data denial", target)
		} else if !strings.Contains(err.Error(), "Reasonix's own session/state data") {
			t.Errorf("Check(%q) error %q does not name session/state data", target, err)
		}
	}
}

func TestSessionDataGuardDeniesRuntimeLedgers(t *testing.T) {
	root, _, _ := stateRootFor(t)
	g := NewSessionDataGuard(root, nil)

	for _, target := range []string{
		filepath.Join(root, "desktop-tabs.json"),
		filepath.Join(root, "desktop-tabs.json.tmp"), // the fixed atomic-save sibling
		filepath.Join(root, "desktop-projects.json"),
		filepath.Join(root, "desktop-window.json"),
		filepath.Join(root, "desktop-workspace"),
		filepath.Join(root, "metrics-pending.json"),
		filepath.Join(root, "crash-pending.json"),
	} {
		if err := g.Check(target); err == nil {
			t.Errorf("Check(%q) = nil, want runtime-ledger denial", target)
		}
	}
	// Same names below a NESTED directory are ordinary files (only
	// state-root-direct ledgers are the app's).
	if err := g.Check(filepath.Join(root, "backups", "desktop-tabs.json")); err != nil {
		t.Errorf("nested copy of a ledger name should be writable: %v", err)
	}
	// heartbeat-tasks.json is a documented human/AI-editable contract
	// (desktop/heartbeat.go; the heartbeat panel tip tells users agents can
	// edit it) — the guard must never break that flow.
	if err := g.Check(filepath.Join(root, "heartbeat-tasks.json")); err != nil {
		t.Errorf("heartbeat-tasks.json is AI-editable by product contract, got %v", err)
	}
}

func TestSessionDataGuardCaseVariantOnFoldingSystems(t *testing.T) {
	if !foldPaths {
		t.Skip("case-sensitive default filesystem: a case variant is a genuinely different path")
	}
	root, cliSession, _ := stateRootFor(t)
	g := NewSessionDataGuard(root, nil)

	upper := filepath.Join(root, "SESSIONS", filepath.Base(cliSession))
	if err := g.Check(upper); err == nil {
		t.Fatalf("Check(%q) = nil; case variant reaches the same store on this filesystem and must be denied", upper)
	}
	mixedLedger := filepath.Join(root, "Desktop-Tabs.JSON")
	if err := g.Check(mixedLedger); err == nil {
		t.Fatalf("Check(%q) = nil, want case-folded ledger denial", mixedLedger)
	}
}

func TestConfineReadCaseVariantOnFoldingSystems(t *testing.T) {
	if !foldPaths {
		t.Skip("case-sensitive default filesystem: a case variant is a genuinely different path")
	}
	forbidDir := t.TempDir()
	secret := filepath.Join(forbidDir, "secret.txt")
	if err := os.WriteFile(secret, []byte("classified"), 0o644); err != nil {
		t.Fatal(err)
	}
	forbidRoots := realRoots([]string{forbidDir})
	upper := filepath.Join(filepath.Dir(forbidDir), strings.ToUpper(filepath.Base(forbidDir)), "secret.txt")
	if !confineRead(forbidRoots, upper) {
		t.Fatalf("confineRead missed case variant %q of a forbidden root", upper)
	}
}

func TestSessionDataGuardAllowsOrdinaryStatePaths(t *testing.T) {
	root, _, _ := stateRootFor(t)
	g := NewSessionDataGuard(root, nil)

	for _, target := range []string{
		filepath.Join(root, "config.toml"),                        // config is confine()'s job, not this guard's
		filepath.Join(root, "projects", "slug", "memory", "a.md"), // memory files are not session data
		filepath.Join(root, "skills", "demo", "SKILL.md"),
		filepath.Join(t.TempDir(), "unrelated.txt"),
	} {
		if err := g.Check(target); err != nil {
			t.Errorf("Check(%q) = %v, want nil", target, err)
		}
	}
}

func TestSessionDataGuardZeroValueUnconfined(t *testing.T) {
	var g SessionDataGuard
	if err := g.Check("/anywhere/sessions/x.jsonl"); err != nil {
		t.Errorf("zero-value guard should be unconfined, got %v", err)
	}
	if hint := g.CommandHint("", "rm -rf ~/.reasonix/sessions"); hint != "" {
		t.Errorf("zero-value guard hint = %q, want empty", hint)
	}
}

func TestSessionDataGuardDeniesSecurityBoundaryFiles(t *testing.T) {
	root, _, _ := stateRootFor(t)
	g := NewSessionDataGuard(root, nil)

	// settings.json holds the global hooks (arbitrary shell commands run on
	// every future session), so it remains a security boundary.
	target := filepath.Join(root, "settings.json")
	if err := g.Check(target); err == nil {
		t.Errorf("Check(%q) = nil, want security-boundary denial", target)
	} else if !strings.Contains(err.Error(), "security boundary") {
		t.Errorf("Check(%q) error %q should name the security boundary", target, err)
	}
	if err := g.Check(filepath.Join(root, "trust.json")); err != nil {
		t.Errorf("obsolete trust.json should not remain a security boundary: %v", err)
	}
	// The same names nested below the state root are ordinary files (a project
	// checkout under a home workspace may legitimately contain them).
	if err := g.Check(filepath.Join(root, "backups", "settings.json")); err != nil {
		t.Errorf("nested settings.json should be writable: %v", err)
	}
	// An explicit allow_write entry stays the sanctioned escape hatch.
	allowed := NewSessionDataGuard(root, []string{root})
	if err := allowed.Check(filepath.Join(root, "settings.json")); err != nil {
		t.Errorf("allow_write-covered settings.json should pass, got %v", err)
	}
}

func TestSessionDataGuardSecurityFilesCaseVariantOnFoldingSystems(t *testing.T) {
	if !foldPaths {
		t.Skip("case-sensitive default filesystem: a case variant is a genuinely different path")
	}
	root, _, _ := stateRootFor(t)
	g := NewSessionDataGuard(root, nil)
	if err := g.Check(filepath.Join(root, "Settings.JSON")); err == nil {
		t.Fatal("case variant of settings.json reaches the same bytes on this filesystem and must be denied")
	}
}

func TestSessionDataGuardAllowWriteEscapeHatch(t *testing.T) {
	root, cliSession, projectSession := stateRootFor(t)
	g := NewSessionDataGuard(root, []string{filepath.Join(root, "sessions")})

	if err := g.Check(cliSession); err != nil {
		t.Errorf("allow_write-listed store should pass, got %v", err)
	}
	// The other store stays guarded.
	if err := g.Check(projectSession); err == nil {
		t.Error("project store should stay denied when only the CLI store is allowed")
	}
}

func TestWriteToolsRejectSessionData(t *testing.T) {
	root, cliSession, projectSession := stateRootFor(t)
	// Workspace root covers the state root — the accidental self-write shape
	// (e.g. a home-directory workspace).
	guard := NewSessionDataGuard(root, nil)
	tools := ConfineWriters([]string{root}, guard, ManagedConfigPaths{})

	argsFor := func(name, target string) json.RawMessage {
		var m map[string]any
		switch name {
		case "write_file":
			m = map[string]any{"path": target, "content": "tampered"}
		case "edit_file":
			m = map[string]any{"path": target, "old_string": "{}", "new_string": "[]"}
		case "multi_edit":
			m = map[string]any{"path": target, "edits": []map[string]any{{"old_string": "{}", "new_string": "[]"}}}
		case "move_file":
			m = map[string]any{"source_path": target, "destination_path": target + ".bak"}
		case "notebook_edit":
			m = map[string]any{"path": target, "cell_index": 0, "mode": "delete"}
		case "delete_range":
			m = map[string]any{"path": target, "start_anchor": "{}", "end_anchor": "{}"}
		case "delete_symbol":
			m = map[string]any{"path": target, "name": "x"}
		default:
			t.Fatalf("unhandled tool %s", name)
		}
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	for _, tl := range tools {
		for _, target := range []string{cliSession, projectSession} {
			_, err := tl.Execute(context.Background(), argsFor(tl.Name(), target))
			if err == nil || !strings.Contains(err.Error(), "session/state data") {
				t.Errorf("%s on %q: err = %v, want session-data denial", tl.Name(), target, err)
			}
		}
		// The same tool still writes ordinary workspace files (guard is not a
		// blanket block on the state root).
		if tl.Name() == "write_file" {
			ok := filepath.Join(root, "notes.txt")
			if _, err := tl.Execute(context.Background(), argsFor("write_file", ok)); err != nil {
				t.Errorf("write_file on ordinary path: %v", err)
			}
		}
	}
}

func TestSessionDataGuardCommandHint(t *testing.T) {
	root, cliSession, _ := stateRootFor(t)
	g := NewSessionDataGuard(root, nil)

	hinted := []string{
		"python3 fix.py " + cliSession,
		"rm -rf " + filepath.ToSlash(filepath.Join(root, "projects", "slug", "sessions")),
		"Get-Content " + strings.ToUpper(filepath.ToSlash(filepath.Join(root, "sessions"))) + "/x.jsonl", // case-insensitive
	}
	for _, cmd := range hinted {
		if hint := g.CommandHint("", cmd); hint == "" {
			t.Errorf("CommandHint(%q) = empty, want warning", cmd)
		} else if !strings.Contains(hint, "conflict cop") {
			t.Errorf("CommandHint(%q) = %q, want conflict-copy explanation", cmd, hint)
		}
	}
	for _, cmd := range []string{
		"go test ./...",
		"ls " + filepath.Join(t.TempDir(), "sessions"), // "sessions" under an unrelated root
		"",
	} {
		if hint := g.CommandHint("", cmd); hint != "" {
			t.Errorf("CommandHint(%q) = %q, want empty", cmd, hint)
		}
	}
}

func TestSessionDataGuardCommandHintEnvVarForm(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	state := filepath.Join(home, ".reasonix")
	if err := os.MkdirAll(filepath.Join(state, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	g := NewSessionDataGuard(state, nil)

	for _, cmd := range []string{
		`python3 -c "open('$HOME/.reasonix/sessions/x.jsonl','w')"`,
		"rm ${HOME}/.reasonix/projects/slug/sessions/y.jsonl",
	} {
		if hint := g.CommandHint("", cmd); hint == "" {
			t.Errorf("CommandHint(%q) = empty, want warning for env-var path form", cmd)
		}
	}
}

func TestSessionDataGuardCommandHintRelativeFromStateRoot(t *testing.T) {
	root, _, _ := stateRootFor(t)
	g := NewSessionDataGuard(root, nil)
	// The desktop Global workspace lives at <state root>/global-workspace, so a
	// relative ../sessions reaches the store without an absolute path in the
	// command text.
	workDir := filepath.Join(root, "global-workspace")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if hint := g.CommandHint(workDir, "python3 fix.py ../sessions/x.jsonl"); hint == "" {
		t.Error("relative reference from a state-root workDir should warn")
	}
	if hint := g.CommandHint(workDir, "go build ./..."); hint != "" {
		t.Errorf("ordinary command in the Global workspace should stay clean, got %q", hint)
	}
	// A workDir already inside a guarded store warns on every command: any
	// relative operation there touches the store.
	inStore := filepath.Join(root, "projects", "slug", "sessions")
	if hint := g.CommandHint(inStore, "python3 fix.py x.jsonl"); hint == "" {
		t.Error("workDir inside a session store should warn unconditionally")
	}
	// An unrelated workDir does not fabricate warnings.
	if hint := g.CommandHint(t.TempDir(), "cat ../sessions/x.jsonl"); hint != "" {
		t.Errorf("relative form outside the state root should stay clean, got %q", hint)
	}
}

func TestBashAppendsSessionDataHint(t *testing.T) {
	root, cliSession, _ := stateRootFor(t)
	guard := NewSessionDataGuard(root, nil)
	b := ConfineBash(sandbox.Spec{Mode: "off"}, guard)

	args, _ := json.Marshal(map[string]string{"command": "echo " + cliSession})
	out, err := b.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if !strings.Contains(out, "WARNING: this command referenced Reasonix's own session/state data") {
		t.Fatalf("bash output missing session-data warning:\n%s", out)
	}
	// An ordinary command stays clean.
	args, _ = json.Marshal(map[string]string{"command": "echo hello"})
	out, err = b.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if strings.Contains(out, "WARNING") {
		t.Fatalf("bash output has spurious warning:\n%s", out)
	}
}
