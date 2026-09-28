package boot

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/hook"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

const roleHookSecret = "role-hook-secret-contents"

// roleHookScript is one model's side of a role-hook run: a reviewing role reads
// secret.txt and then answers; the executor optionally writes a file first.
type roleHookScript struct {
	mu    sync.Mutex
	role  string
	final string
	reqs  []provider.Request
}

func (s *roleHookScript) Name() string { return "boot-role-hooks-" + s.role }

func (s *roleHookScript) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	s.mu.Lock()
	s.reqs = append(s.reqs, req)
	n := len(s.reqs)
	s.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	switch {
	case s.role == "executor" && n == 1 && s.final == "write":
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "exec-write", Name: "write_file", Arguments: `{"path":"out.txt","content":"x\n"}`,
		}}
	case s.role == "executor":
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	case len(effectToolResults(req)) == 0:
		emitReadFile(ch, s.role+"-read", "secret.txt")
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: s.final}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (s *roleHookScript) requests() []provider.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]provider.Request(nil), s.reqs...)
}

var (
	roleHookRegister sync.Once
	roleHookMu       sync.Mutex
	roleHookScripts  map[string]*roleHookScript
)

func useRoleHookScripts(t *testing.T, scripts ...*roleHookScript) {
	t.Helper()
	roleHookRegister.Do(func() {
		provider.Register("boot-role-hooks", func(cfg provider.Config) (provider.Provider, error) {
			roleHookMu.Lock()
			defer roleHookMu.Unlock()
			return roleHookScripts[cfg.Model], nil
		})
	})
	byModel := map[string]*roleHookScript{}
	for _, s := range scripts {
		byModel[s.role+"-model"] = s
	}
	roleHookMu.Lock()
	roleHookScripts = byModel
	roleHookMu.Unlock()
	t.Cleanup(func() {
		roleHookMu.Lock()
		roleHookScripts = nil
		roleHookMu.Unlock()
	})
}

// denyReadSettings is a PreToolUse hook that logs its payload and refuses
// every read_file call.
func denyReadSettings(t *testing.T, dir string) (hook.Settings, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the hook under test is a POSIX shell script")
	}
	logPath := filepath.Join(dir, "hook.log")
	script := filepath.Join(dir, "deny-read.sh")
	writeFile(t, dir, "deny-read.sh", "#!/bin/sh\ncat >> "+shellQuoteForTest(logPath)+"\necho >> "+shellQuoteForTest(logPath)+"\necho 'reading secrets is not allowed' >&2\nexit 2\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	return hook.Settings{Hooks: map[hook.Event][]hook.HookConfig{
		hook.PreToolUse: {{Match: "read_file", Command: script}},
	}}, logPath
}

func writeRoleHookConfig(t *testing.T, dir, roleLine, role string) {
	t.Helper()
	writeFile(t, dir, "reasonix.toml", `
default_model = "executor"

[agent]
`+roleLine+`

[codegraph]
enabled = false

[[providers]]
name = "executor"
kind = "boot-role-hooks"
model = "executor-model"

[[providers]]
name = "`+role+`"
kind = "boot-role-hooks"
model = "`+role+`-model"
`)
	approveWorkspace(t, dir)
}

// assertRoleReadBlocked holds the effect at both boundaries: the hook process
// saw the role's read under the parent session's id suffixed with the role, and
// the role's model got the refusal instead of the file.
func assertRoleReadBlocked(t *testing.T, logPath string, role *roleHookScript, parentSession string) {
	t.Helper()
	reqs := role.requests()
	if len(reqs) < 2 {
		t.Fatalf("%s made %d request(s), want the read and its follow-up", role.role, len(reqs))
	}
	results := effectToolResults(reqs[1])
	if len(results) != 1 || !strings.HasPrefix(results[0], "blocked:") {
		t.Fatalf("%s's read_file result = %q, want a PreToolUse block", role.role, results)
	}
	sessions := hookSessionIDs(t, logPath)
	if len(sessions) != 1 {
		t.Fatalf("PreToolUse calls = %d, want the %s's one read; sessions=%q", len(sessions), role.role, sessions)
	}
	if want := parentSession + ":" + role.role; parentSession == "" || sessions[0] != want {
		t.Fatalf("%s hook session = %q, want %q", role.role, sessions[0], want)
	}
	for _, req := range reqs {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, roleHookSecret) {
				t.Fatalf("the %s's model received the file a PreToolUse hook denied", role.role)
			}
		}
	}
}

func TestEffectPreToolUseCoversPlannerReads(t *testing.T) {
	for _, when := range []string{"configured", "saved_mid_session"} {
		t.Run(when, func(t *testing.T) {
			isolateConfigHome(t)
			dir := robustTempDir(t)
			t.Chdir(dir)
			writeFile(t, dir, "secret.txt", roleHookSecret+"\n")
			planner := &roleHookScript{role: "planner", final: "Plan: nothing to change."}
			executor := &roleHookScript{role: "executor"}
			useRoleHookScripts(t, planner, executor)
			writeRoleHookConfig(t, dir, `planner_model = "planner"`, "planner")
			settings, logPath := denyReadSettings(t, dir)
			if when == "configured" {
				if err := hook.Save(hook.ScopeProject, dir, settings); err != nil {
					t.Fatal(err)
				}
				if err := hook.ApproveSavedProjectHooks(hook.LoadOptions{ProjectRoot: dir}, settings); err != nil {
					t.Fatal(err)
				}
			}

			ctrl, err := Build(context.Background(), Options{Sink: event.Discard, SessionDir: filepath.Join(dir, "sessions")})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			defer ctrl.Close()
			ctrl.SetFreshSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
			if when == "saved_mid_session" {
				if err := ctrl.SaveHooks(hook.ScopeProject, settings); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			_ = ctrl.Run(ctx, control.PlannerRouteMarker+" look at secret.txt")
			assertRoleReadBlocked(t, logPath, planner, sessionstore.BranchID(ctrl.SessionPath()))
		})
	}
}

func TestEffectPreToolUseCoversGuardianReads(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "secret.txt", roleHookSecret+"\n")
	guardian := &roleHookScript{role: "guardian", final: `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"requested write"}`}
	executor := &roleHookScript{role: "executor", final: "write"}
	useRoleHookScripts(t, guardian, executor)
	writeRoleHookConfig(t, dir, `guardian_model = "guardian"`, "guardian")
	settings, logPath := denyReadSettings(t, dir)
	if err := hook.Save(hook.ScopeProject, dir, settings); err != nil {
		t.Fatal(err)
	}
	if err := hook.ApproveSavedProjectHooks(hook.LoadOptions{ProjectRoot: dir}, settings); err != nil {
		t.Fatal(err)
	}

	var ctrl *control.Controller
	var ready sync.WaitGroup
	ready.Add(1)
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest {
			id := e.Approval.ID
			go func() { ready.Wait(); ctrl.Approve(id, false, false, false) }()
		}
	})
	ctrl, err := Build(context.Background(), Options{Sink: sink, SessionDir: filepath.Join(dir, "sessions")})
	ready.Done()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.SetFreshSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
	ctrl.SetToolApprovalMode(control.ToolApprovalAsk)
	ctrl.EnableInteractiveApproval()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_ = ctrl.Run(ctx, "write out.txt")
	assertRoleReadBlocked(t, logPath, guardian, sessionstore.BranchID(ctrl.SessionPath()))
}

// TestEffectPlannerHookSessionFollowsRotation holds that a role's hook session
// is read from the parent when the hook fires, so a rotated session reaches it.
func TestEffectPlannerHookSessionFollowsRotation(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "secret.txt", roleHookSecret+"\n")
	planner := &roleHookScript{role: "planner", final: "Plan: nothing to change."}
	executor := &roleHookScript{role: "executor"}
	useRoleHookScripts(t, planner, executor)
	writeRoleHookConfig(t, dir, `planner_model = "planner"`, "planner")
	settings, logPath := denyReadSettings(t, dir)
	if err := hook.Save(hook.ScopeProject, dir, settings); err != nil {
		t.Fatal(err)
	}
	if err := hook.ApproveSavedProjectHooks(hook.LoadOptions{ProjectRoot: dir}, settings); err != nil {
		t.Fatal(err)
	}
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, SessionDir: filepath.Join(dir, "sessions")})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.SetFreshSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	var parents []string
	for range 2 {
		_ = ctrl.Run(ctx, control.PlannerRouteMarker+" look at secret.txt")
		parents = append(parents, sessionstore.BranchID(ctrl.SessionPath()))
		if err := ctrl.NewSession(); err != nil {
			t.Fatalf("NewSession: %v", err)
		}
	}
	if parents[0] == "" || parents[0] == parents[1] {
		t.Fatalf("parent sessions = %q, want two distinct ids", parents)
	}
	sessions := hookSessionIDs(t, logPath)
	want := []string{parents[0] + ":planner", parents[1] + ":planner"}
	if len(sessions) != 2 || sessions[0] != want[0] || sessions[1] != want[1] {
		t.Fatalf("planner hook sessions = %q, want %q", sessions, want)
	}
}
