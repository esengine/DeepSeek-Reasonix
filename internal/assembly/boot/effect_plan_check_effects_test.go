package boot

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

// planCheckHold is how the host says a plan check still owes a run.
const planCheckHold = "after the latest change"

// planCheckScript plays the planner (one submit_plan) or the executor (its
// calls in order, then "done" to every later request).
type planCheckScript struct {
	mu       sync.Mutex
	role     string
	planArgs string
	calls    []plannedCall
	reqs     []provider.Request
}

func (s *planCheckScript) Name() string { return "boot-plan-check-" + s.role }

func (s *planCheckScript) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	s.mu.Lock()
	s.reqs = append(s.reqs, req)
	s.mu.Unlock()
	made := 0
	for _, m := range req.Messages {
		made += len(m.ToolCalls)
	}
	ch := make(chan provider.Chunk, 2)
	switch {
	case s.role == "planner" && made == 0:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "plan-1", Name: "submit_plan", Arguments: s.planArgs}}
	case s.role == "planner":
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "Plan submitted."}
	case made < len(s.calls):
		c := s.calls[made]
		raw, _ := json.Marshal(c.args)
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: c.name + "-" + string(rune('a'+made)), Name: c.name, Arguments: string(raw)}}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (s *planCheckScript) saw(want string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.reqs {
		for _, m := range r.Messages {
			if strings.Contains(m.Content, want) {
				return true
			}
		}
	}
	return false
}

// runPlannedChecks drives one planner-routed turn through the real assembly and
// reports whether the host held it for a plan check, with the workspace dir.
func runPlannedChecks(t *testing.T, files map[string]string, planArgs string, calls []plannedCall) (bool, string) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	isolateConfigHome(t)
	// The subject is what the checks write, not the jail: a host without an OS
	// sandbox refuses bash fail-closed, and the checks would never run.
	writeUserConfig(t, "[sandbox]\nbash = \"off\"\n")
	dir := robustTempDir(t)
	t.Chdir(dir)
	// A declared project check makes the turn answer to readiness in the
	// balanced preset; it writes nothing and runs last.
	files["REASONIX.md"] = "## Reasonix host checks\n\n- verify: sh noop.sh\n"
	files["noop.sh"] = "true\n"
	for name, body := range files {
		writeFile(t, dir, name, body)
	}
	kind := uniqueKind("boot-plan-check")
	planner := &planCheckScript{role: "planner", planArgs: planArgs}
	executor := &planCheckScript{role: "executor", calls: append(calls, plannedCall{"bash", map[string]any{"command": "sh noop.sh"}})}
	provider.Register(kind, func(cfg provider.Config) (provider.Provider, error) {
		if cfg.Model == "planner-model" {
			return planner, nil
		}
		return executor, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "executor"

[agent]
planner_model = "planner"

[codegraph]
enabled = false

[[providers]]
name = "executor"
kind = "`+kind+`"
model = "executor-model"

[[providers]]
name = "planner"
kind = "`+kind+`"
model = "planner-model"
`)
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, SessionDir: filepath.Join(dir, "sessions")})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.SetFreshSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runErr := ctrl.Run(ctx, control.PlannerRouteMarker+" change src.txt")
	if len(executor.reqs) == 0 {
		t.Fatalf("the executor never ran (run error %v)", runErr)
	}
	if !executor.saw("verify: sh") {
		t.Fatal("the executor handoff never carried the plan's checks")
	}
	held := executor.saw(planCheckHold) || (runErr != nil && strings.Contains(runErr.Error(), planCheckHold))
	return held, dir
}

// What a check leaves behind outside anything the plan or the turn wrote is
// its own residue and does not void it.
func TestEffectPlanCheckResidueDoesNotVoidItself(t *testing.T) {
	plan := `{"objective":"fix src","steps":[{"id":"s1","title":"fix src","candidate_files":["src.txt"],
	  "acceptance":[{"text":"src is fixed"}],
	  "verification":[{"command":"sh check.sh"}]}]}`
	held, _ := runPlannedChecks(t, map[string]string{
		"src.txt": "bug\n", "pkg/mod.txt": "m\n",
		"check.sh": "mkdir -p .cache pkg/__pycache__ && echo $$ > .cache/stamp && echo $$ > pkg/__pycache__/mod.pyc && grep -qx fixed src.txt\n",
	}, plan, []plannedCall{
		{"write_file", map[string]any{"path": "src.txt", "content": "fixed\n"}},
		{"bash", map[string]any{"command": "sh check.sh"}},
		{"bash", map[string]any{"command": "sh check.sh"}},
	})
	if held {
		t.Fatal("a check's own residue voided the pass it had just produced")
	}
}
