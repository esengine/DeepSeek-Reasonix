package boot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
)

const (
	proseFixtureCode  = `{"path":"main.go","content":"package main\n\nfunc main() {}\n\nfunc Add(a, b int) int { return a + b }\n"}`
	proseFixtureCode2 = `{"path":"main.go","content":"package main\n\nfunc main() {}\n\nfunc Sub(a, b int) int { return a - b }\n"}`
	proseFixtureDoc   = `{"path":"CHANGELOG.md","content":"- add Add\n"}`
	proseFixtureDoc2  = `{"path":"README.md","content":"Adds two numbers.\n"}`
	proseFixtureCheck = `{"command":"git diff --check"}`
)

// trackFixture puts the fixture under version control so `git diff --check`,
// a check that needs no toolchain, has something to compare against.
func trackFixture(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v unavailable: %v: %s", args, err, out)
		}
	}
}

func proseAfterCheckWorkspace(t *testing.T, declared, hook bool) (home, dir string) {
	t.Helper()
	isolateConfigHome(t)
	home, err := filepath.EvalSymlinks(robustTempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("REASONIX_HOME", home)
	dir, err = filepath.EvalSymlinks(robustTempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	writeUserConfig(t, userModel+"\n[sandbox]\nbash = \"off\"\n\n[codegraph]\nenabled = false\n")
	registerBootTokenProfileTestProvider()
	if hook {
		command, err := json.Marshal(markerCommand(t, filepath.Join(dir, "main.go")))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, config.RootsForHome("").Home(), "settings.json", `{"hooks":{"PostToolUse":[{"match":"write_file","command":`+string(command)+`}]}}`)
	}
	writeFile(t, dir, "go.mod", "module fixture\n\ngo 1.21\n")
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, dir, "README.md", "A neutral readme.\n")
	if declared {
		writeFile(t, dir, "AGENTS.md", "## Reasonix host checks\n\n- verify: git diff --check\n")
	}
	trackFixture(t, dir)
	approveWorkspace(t, dir)
	return home, dir
}

func TestEffectProseAfterPassingCheckOwesNoRerun(t *testing.T) {
	for _, tc := range []struct {
		name     string
		turns    []testutil.Turn
		declared bool
		hook     bool
		sidecar  string
		symlink  bool
		debt     bool
		wantPath string
	}{
		{name: "code, passing check, changelog", turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			call("check", "bash", proseFixtureCheck),
			call("doc", "write_file", proseFixtureDoc),
		}},
		{name: "code, passing check, two prose files", turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			call("check", "bash", proseFixtureCheck),
			call("doc", "write_file", proseFixtureDoc),
			call("doc2", "write_file", proseFixtureDoc2),
		}},
		{name: "code, check, code again, prose", turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			call("check", "bash", proseFixtureCheck),
			call("src2", "write_file", proseFixtureCode2),
			call("doc", "write_file", proseFixtureDoc),
		}, debt: true, wantPath: "main.go"},
		{name: "code between prose after the check", turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			call("check", "bash", proseFixtureCheck),
			call("doc", "write_file", proseFixtureDoc),
			call("src2", "write_file", proseFixtureCode2),
			call("doc2", "write_file", proseFixtureDoc2),
		}, debt: true, wantPath: "main.go"},
		{name: "code between prose, checked again, prose", turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			call("check", "bash", proseFixtureCheck),
			call("doc", "write_file", proseFixtureDoc),
			call("src2", "write_file", proseFixtureCode2),
			call("check2", "bash", proseFixtureCheck),
			call("doc2", "write_file", proseFixtureDoc2),
		}},
		{name: "failed bash writes code after the check", turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			call("check", "bash", proseFixtureCheck),
			call("b", "bash", `{"command":"echo '// x' >> main.go && exit 3"}`),
			call("doc", "write_file", proseFixtureDoc),
		}, debt: true, wantPath: "main.go"},
		{name: "bash appends code after the check", turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			call("check", "bash", proseFixtureCheck),
			call("b", "bash", `{"command":"echo '// x' >> main.go"}`),
			call("doc", "write_file", proseFixtureDoc),
		}, debt: true, wantPath: "main.go"},
		{name: "sed edits code after the check", turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			call("check", "bash", proseFixtureCheck),
			call("b", "bash", `{"command":"sed -i.bak 's/Add/Plus/' main.go"}`),
			call("doc", "write_file", proseFixtureDoc),
		}, debt: true, wantPath: "main.go"},
		{name: "code renamed to prose after the check", turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			call("check", "bash", proseFixtureCheck),
			call("mv", "move_file", `{"source_path":"main.go","destination_path":"main.md"}`),
			call("doc", "write_file", proseFixtureDoc),
		}, debt: true, wantPath: "main.go"},
		{name: "prose alias of code written after the check", symlink: true, turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			call("check", "bash", proseFixtureCheck),
			call("alias", "write_file", `{"path":"notes.md","content":"package main\n\nfunc main() {}\n"}`),
			call("doc", "write_file", proseFixtureDoc),
		}, debt: true, wantPath: "notes.md"},
		{name: "prose alias of code written then removed", symlink: true, turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			call("check", "bash", proseFixtureCheck),
			call("alias", "write_file", `{"path":"notes.md","content":"package main\n\nfunc main() {}\n"}`),
			call("rm", "bash", `{"command":"rm notes.md"}`),
			call("doc", "write_file", proseFixtureDoc),
		}, debt: true, wantPath: "main.go"},
		{name: "prose alias of code written then replaced by prose", symlink: true, turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			call("check", "bash", proseFixtureCheck),
			call("alias", "write_file", `{"path":"notes.md","content":"package main\n\nfunc main() {}\n"}`),
			call("rm", "bash", `{"command":"rm notes.md"}`),
			call("note", "write_file", `{"path":"notes.md","content":"A neutral note.\n"}`),
			call("doc", "write_file", proseFixtureDoc),
		}, debt: true, wantPath: "main.go"},
		{name: "hook rewrites code on the prose write", hook: true, turns: []testutil.Turn{
			call("b", "bash", `{"command":"echo '// b' >> main.go"}`),
			call("check", "bash", proseFixtureCheck),
			call("doc", "write_file", proseFixtureDoc),
		}, debt: true},
		{name: "hook, prose-only turn", hook: true, turns: []testutil.Turn{
			call("doc", "write_file", proseFixtureDoc),
		}, debt: true, wantPath: "tool hook"},
		{name: "hook, code then check", hook: true, turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			call("check", "bash", proseFixtureCheck),
		}},
		{name: "sidecar rewrites code on the prose write", sidecar: "tool.after", turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			call("check", "bash", proseFixtureCheck),
			call("doc", "write_file", proseFixtureDoc),
		}, debt: true},
		{name: "sidecar, prose-only turn", sidecar: "tool.after", turns: []testutil.Turn{
			call("doc", "write_file", proseFixtureDoc),
		}, debt: true, wantPath: "extension"},
		{name: "permission sidecar rewrites code on the prose write", sidecar: "permission.decision", turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			call("check", "bash", proseFixtureCheck),
			call("doc", "write_file", proseFixtureDoc),
		}, debt: true},
		{name: "permission sidecar, prose-only turn", sidecar: "permission.decision", turns: []testutil.Turn{
			call("doc", "write_file", proseFixtureDoc),
		}, debt: true, wantPath: "extension"},
		{name: "declared check", declared: true, turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			call("check", "bash", proseFixtureCheck),
			call("doc", "write_file", proseFixtureDoc),
		}, debt: true},
		{name: "failing check then prose", turns: []testutil.Turn{
			call("src", "write_file", `{"path":"main.go","content":"package main\n\nfunc main() {} \n"}`),
			call("check", "bash", proseFixtureCheck),
			call("doc", "write_file", proseFixtureDoc),
		}, debt: true, wantPath: "main.go"},
		{name: "prose write that failed, no check", turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			call("doc", "write_file", `{"path":"main.go/CHANGELOG.md","content":"- add Add\n"}`),
		}, debt: true, wantPath: "main.go"},
		{name: "check, then a prose write that failed", turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			call("check", "bash", proseFixtureCheck),
			call("doc", "write_file", `{"path":"main.go/CHANGELOG.md","content":"- add Add\n"}`),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, dir := proseAfterCheckWorkspace(t, tc.declared, tc.hook)
			if tc.sidecar != "" {
				installBootFakePlugin(t, home, "prose-writer", map[string]any{
					"intercepts": []string{tc.sidecar},
					"env":        map[string]string{bootFakeEnvWriteOnProse: filepath.Join(dir, "main.go")},
				})
			}
			if tc.symlink {
				if err := os.Symlink(filepath.Join(dir, "main.go"), filepath.Join(dir, "notes.md")); err != nil {
					t.Skipf("symlink creation unavailable (Windows requires privilege or Developer Mode): %v", err)
				}
			}
			turns := slices.Clone(tc.turns)
			prov := testutil.NewMock("prose-after-check", append(turns, testutil.Turn{Text: "done"})...)
			setBootTokenProfileTestProvider(t, prov)
			ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, AgentPreset: AgentPresetBalanced, Sink: &bundleAuditSink{}, HeadlessApprovalMode: control.ToolApprovalAuto})
			if err != nil {
				t.Fatal(err)
			}
			defer ctrl.Close()
			err = ctrl.Run(context.Background(), "add Add and note it in the changelog")
			results := toolResults(prov.Requests())
			if check, ran := results["check"]; ran && !tc.declared && tc.name != "failing check then prose" && !strings.Contains(check, "settled") {
				t.Fatalf("check did not pass: %s", check)
			}
			if tc.sidecar != "" {
				if src, _ := os.ReadFile(filepath.Join(dir, "main.go")); !strings.Contains(string(src), "// sidecar") {
					t.Fatalf("sidecar did not write main.go: %q", src)
				}
			}
			var unready *agent.FinalReadinessError
			if !tc.debt {
				if err != nil {
					t.Fatalf("Run = %v, want completion", err)
				}
				for id, result := range results {
					if strings.Contains(result, "stale_verification") && strings.HasPrefix(id, "doc") {
						t.Errorf("%s told the model a check is owed again: %s", id, result)
					}
				}
				return
			}
			if !errors.As(err, &unready) {
				t.Fatalf("Run = %v, want a readiness error", err)
			}
			if !slices.Contains(unready.Missing, "verification") && !slices.Contains(unready.Missing, "project_check") {
				t.Fatalf("missing = %v, want the check still owed", unready.Missing)
			}
			if tc.wantPath != "" && !strings.Contains(unready.Reason, tc.wantPath) {
				t.Errorf("reason %q does not name %s", unready.Reason, tc.wantPath)
			}
		})
	}
}

type proseTurnSink struct {
	mu        sync.Mutex
	done      chan event.Event
	continued int
}

func (s *proseTurnSink) Emit(e event.Event) {
	switch {
	case e.Kind == event.TurnDone:
		s.done <- e
	case e.Kind == event.Notice && e.Text == i18n.M.ReadinessContinuing:
		s.mu.Lock()
		s.continued++
		s.mu.Unlock()
	}
}

// TestEffectProseAfterCheckSpendsNoContinuation drives the interactive path a
// user sees: the model changes code, runs the check, updates the docs and
// stops, re-running the check only when the host sends it back.
func TestEffectProseAfterCheckSpendsNoContinuation(t *testing.T) {
	for _, tc := range []struct {
		name             string
		turns            []testutil.Turn
		requests, checks int
		continuations    int
	}{
		{name: "code, check, docs, stop", turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			call("check", "bash", proseFixtureCheck),
			call("doc", "write_file", proseFixtureDoc),
			{Text: "Added Add and noted it in the changelog."},
			call("recheck", "bash", proseFixtureCheck),
			{Text: "Re-ran the check."},
		}, requests: 4, checks: 1},
		{name: "code, stop, sent back: check, docs, stop", turns: []testutil.Turn{
			call("src", "write_file", proseFixtureCode),
			{Text: "Added Add."},
			call("check", "bash", proseFixtureCheck),
			call("doc", "write_file", proseFixtureDoc),
			{Text: "Checked and noted it in the changelog."},
			call("recheck", "bash", proseFixtureCheck),
			{Text: "Re-ran the check."},
		}, requests: 5, checks: 1, continuations: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, dir := proseAfterCheckWorkspace(t, false, false)
			script := slices.Clone(tc.turns)
			for range 8 {
				script = append(script, testutil.Turn{Text: "Nothing further to run."})
			}
			prov := testutil.NewMock("prose-after-check-submit", script...)
			setBootTokenProfileTestProvider(t, prov)
			sink := &proseTurnSink{done: make(chan event.Event, 4)}
			ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, AgentPreset: AgentPresetBalanced, Sink: sink, HeadlessApprovalMode: control.ToolApprovalAuto})
			if err != nil {
				t.Fatal(err)
			}
			defer ctrl.Close()
			ctrl.EnableInteractiveApproval()
			ctrl.SetToolApprovalMode(control.ToolApprovalAuto)
			ctrl.Submit("add Add and note it in the changelog")
			var done event.Event
			select {
			case done = <-sink.done:
			case <-time.After(60 * time.Second):
				t.Fatal("turn did not finish")
			}
			checks := 0
			for id := range toolResults(prov.Requests()) {
				if strings.Contains(id, "check") {
					checks++
				}
			}
			sink.mu.Lock()
			continued := sink.continued
			sink.mu.Unlock()
			t.Logf("requests=%d checks=%d continuations=%d outcome=%q err=%v", len(prov.Requests()), checks, continued, done.Outcome, done.Err)
			if done.Err != nil {
				t.Fatalf("turn ended with %v, want it finished", done.Err)
			}
			if got := len(prov.Requests()); got != tc.requests || checks != tc.checks || continued != tc.continuations {
				t.Errorf("requests = %d, checks = %d, continuations = %d; want %d, %d, %d", got, checks, continued, tc.requests, tc.checks, tc.continuations)
			}
		})
	}
}
