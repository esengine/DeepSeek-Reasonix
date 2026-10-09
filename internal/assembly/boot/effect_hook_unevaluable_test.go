package boot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/ext/hook"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

// A PreToolUse hook whose matcher cannot be compiled is a gate nobody can
// evaluate: the write it guards must not run, and the model must be told the
// cause as a code rather than left to guess.
func TestEffectUnevaluablePreToolUseHookBlocksTheCall(t *testing.T) {
	for _, tc := range []struct {
		name        string
		event       hook.Event
		wantBlocked bool
	}{
		{"gating_event_blocks", hook.PreToolUse, true},
		{"observing_event_does_not", hook.PostToolUse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfigHome(t)
			dir := robustTempDir(t)
			t.Chdir(dir)
			executor := &roleHookScript{role: "executor", final: "write"}
			useRoleHookScripts(t, executor)
			writeRoleHookConfig(t, dir, `max_steps = 8`, "unused")

			settingsPath := hook.GlobalSettingsPath("")
			if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
				t.Fatal(err)
			}
			body := `{"hooks":{"` + string(tc.event) + `":[{"match":"write_file[","command":"true","description":"guard"}]}}`
			if err := os.WriteFile(settingsPath, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}

			var notices []event.Event
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.Notice {
					notices = append(notices, e)
				}
			})
			ctrl, err := Build(context.Background(), Options{Sink: sink, SessionDir: filepath.Join(dir, "sessions")})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			defer ctrl.Close()
			ctrl.SetFreshSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
			ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			_ = ctrl.Run(ctx, "write out.txt")

			reqs := executor.requests()
			if len(reqs) < 2 {
				t.Fatalf("executor made %d request(s), want the write and its follow-up", len(reqs))
			}
			results := effectToolResults(reqs[1])
			if len(results) != 1 {
				t.Fatalf("tool results = %q", results)
			}
			_, statErr := os.Stat(filepath.Join(dir, "out.txt"))
			if !tc.wantBlocked {
				if statErr != nil || strings.HasPrefix(results[0], "blocked:") {
					t.Fatalf("an observing hook must not stop the write: stat=%v result=%q", statErr, results[0])
				}
				return
			}
			if statErr == nil {
				t.Fatal("the guarded write ran although its PreToolUse hook could not be evaluated")
			}
			for _, want := range []string{"blocked:", "hook_unevaluable", "code=invalid_matcher", "step=match", "event=PreToolUse"} {
				if !strings.Contains(results[0], want) {
					t.Errorf("model-visible result %q is missing %q", results[0], want)
				}
			}
			if strings.Contains(results[0], "missing closing") || strings.Contains(results[0], "regexp") {
				t.Errorf("model-visible result carries regexp error prose: %q", results[0])
			}
		})
	}
}
