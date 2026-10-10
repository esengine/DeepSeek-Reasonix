package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/builtin"
)

func TestEffectPreToolAccessRejectsBeforePreview(t *testing.T) {
	for _, name := range []string{"read_file", "edit_file", "multi_edit", "write_file", "notebook_edit", "delete_range", "delete_symbol"} {
		t.Run(name, func(t *testing.T) {
			args, err := json.Marshal(map[string]any{
				"path": "private/note.go", "old_string": "missing", "new_string": "x", "content": "x",
				"edits":       []map[string]string{{"old_string": "missing", "new_string": "x"}},
				"cell_number": 0, "new_source": "x", "start_anchor": "package main", "end_anchor": "func RemoveMe() {}", "name": "RemoveMe",
				"source_path": "private/note.go", "destination_path": "moved.go",
			})
			if err != nil {
				t.Fatal(err)
			}
			run := buildPostureRun(t, "[sandbox]\nbash = \"off\"\nforbid_read = [\"private\"]\n", provider.ToolCall{ID: "blocked", Name: name, Arguments: string(args)})
			path := filepath.Join(run.dir, "private", "note.go")
			const original = "package main\n// INTERNAL_NOTE=review-canary-do-not-disclose\nfunc RemoveMe() {}\n"
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			run.ctrl.ApplyHeadlessApprovalMode(control.ToolApprovalYolo)
			if err := run.ctrl.Run(context.Background(), "run the file tool"); err != nil {
				t.Fatal(err)
			}
			result := run.results["blocked"]
			message, code := "Cannot read target file: permission denied.", builtin.CodeReadForbidden
			if name == "write_file" {
				message, code = "Cannot overwrite target file: permission to read existing contents is required.", builtin.CodeOverwriteReadForbidden
			}
			if result.Output != message || result.RefusalCode != code || result.Executed {
				t.Fatalf("wrong final-boundary refusal: %+v", result)
			}
			if seen := run.modelSaw(t, "blocked"); strings.Contains(seen, "review-canary") || seen != result.Output {
				t.Fatalf("unexpected model output: %q", seen)
			}
			for _, msg := range run.ctrl.History() {
				for _, call := range msg.ToolCalls {
					if call.ID == "blocked" && call.Diff != "" {
						t.Fatalf("denied call archived original content: %q", call.Diff)
					}
				}
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != original {
				t.Fatalf("protected file changed: %q %v", data, err)
			}
		})
	}
}

func TestEffectPreToolAccessInheritedByDelegatedAgents(t *testing.T) {
	for _, entry := range []string{"task", "read_only_task", "parallel_tasks", "run_skill", "read_only_skill"} {
		t.Run(entry, func(t *testing.T) {
			isolateConfigHome(t)
			dir := robustTempDir(t)
			t.Chdir(dir)
			writeUserConfig(t, "[sandbox]\nforbid_read = [\"secret.txt\"]\n")
			probe := &subagentHookProvider{delegationTool: entry}
			useSubagentHookProvider(t, probe)
			writeFile(t, dir, "reasonix.toml", `default_model = "test-model"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "`+subagentHookProviderKind+`"
model = "x"
`)
			approveWorkspace(t, dir)
			writeFile(t, dir, "secret.txt", roleHookSecret+"\n")
			if strings.HasSuffix(entry, "skill") {
				writeFile(t, dir, ".reasonix/skills/hook-probe.md", "---\ndescription: inspect a file\nrunAs: subagent\nallowed-tools: read_file\n---\nRead the requested file.")
			}
			ctrl, err := Build(context.Background(), Options{Sink: event.Discard, SessionDir: filepath.Join(dir, "sessions")})
			if err != nil {
				t.Fatal(err)
			}
			defer ctrl.Close()
			ctrl.SetFreshSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := ctrl.Run(ctx, "read secret.txt, then delegate reading it"); err != nil {
				t.Fatal(err)
			}
			wantReads := 1
			if entry == "parallel_tasks" {
				wantReads = 2
			}
			results := probe.childReadResults()
			if len(results) != wantReads {
				t.Fatalf("%s completed %d child reads, want %d", entry, len(results), wantReads)
			}
			for _, result := range results {
				if result != "Cannot read target file: permission denied." {
					t.Fatalf("%s child lost the system target policy: %q", entry, result)
				}
			}
		})
	}
}

func TestEffectPreToolAccessInheritedByReviewRoles(t *testing.T) {
	for _, role := range []string{"planner", "guardian"} {
		t.Run(role, func(t *testing.T) {
			isolateConfigHome(t)
			dir := robustTempDir(t)
			t.Chdir(dir)
			writeUserConfig(t, "[sandbox]\nforbid_read = [\"secret.txt\"]\n")
			writeFile(t, dir, "secret.txt", roleHookSecret+"\n")
			reviewer := &roleHookScript{role: role, final: "Plan: nothing to change."}
			executor := &roleHookScript{role: "executor"}
			if role == "guardian" {
				reviewer.final = `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"requested write"}`
				executor.final = "write"
			}
			useRoleHookScripts(t, reviewer, executor)
			writeRoleHookConfig(t, dir, role+`_model = "`+role+`"`, role)
			var ctrl *control.Controller
			var ready sync.WaitGroup
			ready.Add(1)
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.ApprovalRequest {
					id := e.Approval.ID
					go func() { ready.Wait(); ctrl.Approve(id, false, false, false) }()
				}
			})
			var err error
			ctrl, err = Build(context.Background(), Options{Sink: sink, SessionDir: filepath.Join(dir, "sessions")})
			ready.Done()
			if err != nil {
				t.Fatal(err)
			}
			defer ctrl.Close()
			ctrl.SetFreshSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
			prompt := control.PlannerRouteMarker + " look at secret.txt"
			if role == "guardian" {
				ctrl.SetToolApprovalMode(control.ToolApprovalAsk)
				ctrl.EnableInteractiveApproval()
				prompt = "write out.txt"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_ = ctrl.Run(ctx, prompt)
			reqs := reviewer.requests()
			if len(reqs) < 2 {
				t.Fatalf("%s did not attempt its read", role)
			}
			if results := effectToolResults(reqs[1]); len(results) != 1 || results[0] != "Cannot read target file: permission denied." {
				t.Fatalf("%s lost the system target policy: %q", role, results)
			}
		})
	}
}
