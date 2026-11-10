package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

type scopeRun struct {
	outside string
	cards   []event.Approval
	results map[string]string
}

// runOutsideWrites builds a real session in full-access posture, has the model
// write one file per name into a folder outside the workspace, and answers every
// write-scope card the way answer says. interactive=false leaves the session
// the way a headless run is: nothing is there to ask.
func runOutsideWrites(t *testing.T, interactive bool, answer func(event.Approval) (allow, session, persist bool), names ...string) *scopeRun {
	t.Helper()
	return runOutsideCalls(t, interactive, answer, func(outside string) []provider.ToolCall {
		var calls []provider.ToolCall
		for _, name := range names {
			calls = append(calls, provider.ToolCall{ID: "w-" + name, Name: "write_file", Arguments: `{"path":` + quote(filepath.Join(outside, name)) + `,"content":"hello\n"}`})
		}
		return calls
	})
}

// runOutsideCalls is runOutsideWrites for any calls the model makes against a
// folder outside the workspace.
func runOutsideCalls(t *testing.T, interactive bool, answer func(event.Approval) (allow, session, persist bool), makeCalls func(outside string) []provider.ToolCall) *scopeRun {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	outside := robustTempDir(t)
	t.Chdir(dir)
	writeUserConfig(t, "[sandbox]\nbash = \"off\"\n")
	rec := &postureProvider{calls: makeCalls(outside)}
	postureRegister.Do(func() {
		provider.Register("boot-posture", func(provider.Config) (provider.Provider, error) {
			postureMu.Lock()
			defer postureMu.Unlock()
			return postureCurrent, nil
		})
	})
	postureMu.Lock()
	postureCurrent = rec
	postureMu.Unlock()
	writeFile(t, dir, "reasonix.toml", "default_model = \"test-model\"\n[codegraph]\nenabled = false\n[[providers]]\nname = \"test-model\"\nkind = \"boot-posture\"\nmodel = \"x\"\n")
	approveWorkspace(t, dir)

	run := &scopeRun{outside: outside, results: map[string]string{}}
	var ctrl *control.Controller
	var ready sync.WaitGroup
	ready.Add(1)
	var mu sync.Mutex
	sink := event.FuncSink(func(e event.Event) {
		mu.Lock()
		if e.Kind == event.ToolResult {
			run.results[e.Tool.ID] = e.Tool.Output
		}
		if e.Kind == event.ApprovalRequest && (e.Approval.Tool == control.WriteScopeApprovalTool || e.Approval.Tool == control.DeleteScopeApprovalTool) {
			run.cards = append(run.cards, e.Approval)
		}
		mu.Unlock()
		if e.Kind == event.ApprovalRequest {
			card := e.Approval
			go func() {
				ready.Wait()
				allow, session, persist := answer(card)
				ctrl.Approve(card.ID, allow, session, persist)
			}()
		}
	})
	var err error
	ctrl, err = Build(context.Background(), Options{Sink: sink, SessionDir: filepath.Join(dir, "sessions")})
	ready.Done()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	ctrl.SetFreshSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	if interactive {
		ctrl.EnableInteractiveApproval()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ctrl.Run(ctx, "write the files"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	return run
}

func quote(s string) string { return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"` }

func landed(run *scopeRun, name string) bool {
	_, err := os.Stat(filepath.Join(run.outside, name))
	return err == nil
}

// Full access answers whether to ask, not where the agent may write. A write
// outside the workspace is a question only a person answers, whatever the
// posture, and the answer decides whether the file lands.
func TestEffectWriteOutsideScopeWaitsForAPersonUnderFullAccess(t *testing.T) {
	once := func(event.Approval) (bool, bool, bool) { return true, false, false }
	t.Run("allowed once each, so each write asks", func(t *testing.T) {
		run := runOutsideWrites(t, true, once, "a.txt", "b.txt")
		if len(run.cards) != 2 || !landed(run, "a.txt") || !landed(run, "b.txt") {
			t.Fatalf("cards=%d a=%v b=%v results=%v", len(run.cards), landed(run, "a.txt"), landed(run, "b.txt"), run.results)
		}
		if got := run.cards[0]; got.Tool != control.WriteScopeApprovalTool || !got.Fresh || !strings.HasSuffix(got.Subject, "a.txt") && !strings.HasSuffix(got.Subject, "b.txt") {
			t.Fatalf("card = %+v", got)
		}
	})
	t.Run("the folder allowed for the session, so the second write does not ask", func(t *testing.T) {
		run := runOutsideWrites(t, true, func(event.Approval) (bool, bool, bool) { return true, true, false }, "a.txt", "b.txt")
		if len(run.cards) != 1 || !landed(run, "a.txt") || !landed(run, "b.txt") {
			t.Fatalf("cards=%d a=%v b=%v results=%v", len(run.cards), landed(run, "a.txt"), landed(run, "b.txt"), run.results)
		}
		// The temp tree overlaps what every sandboxed command may already write, so it
		// can be allowed for the session but never recorded as a project folder.
		if !run.cards[0].AllowsSession || run.cards[0].AllowsPersist {
			t.Fatalf("session should be offered and recording should not: %+v", run.cards[0])
		}
	})
	t.Run("refused, so nothing lands and the model is told not to retry", func(t *testing.T) {
		run := runOutsideWrites(t, true, func(event.Approval) (bool, bool, bool) { return false, true, true }, "a.txt")
		if len(run.cards) != 1 || landed(run, "a.txt") {
			t.Fatalf("cards=%d landed=%v", len(run.cards), landed(run, "a.txt"))
		}
		if !strings.Contains(run.results["w-a.txt"], "do not retry") {
			t.Fatalf("the model was not told the write was declined: %q", run.results["w-a.txt"])
		}
	})
	t.Run("a run with nobody to ask keeps the plain refusal", func(t *testing.T) {
		run := runOutsideWrites(t, false, once, "a.txt")
		if len(run.cards) != 0 || landed(run, "a.txt") {
			t.Fatalf("cards=%d landed=%v", len(run.cards), landed(run, "a.txt"))
		}
		if !strings.Contains(run.results["w-a.txt"], "outside the workspace write scope") {
			t.Fatalf("result = %q", run.results["w-a.txt"])
		}
	})
}

func deleteCall(outside string) []provider.ToolCall {
	command := "rm -rf " + quote(filepath.Join(outside, "victim"))
	args, _ := json.Marshal(map[string]string{"command": command})
	return []provider.ToolCall{{ID: "rm", Name: "bash", Arguments: string(args)}}
}

func withVictim(t *testing.T) func(string) []provider.ToolCall {
	return func(outside string) []provider.ToolCall {
		if err := os.MkdirAll(filepath.Join(outside, "victim", "inner"), 0o755); err != nil {
			t.Fatal(err)
		}
		return deleteCall(outside)
	}
}

func victimLeft(run *scopeRun) bool {
	_, err := os.Stat(filepath.Join(run.outside, "victim"))
	return err == nil
}

// A recursive delete outside the workspace used to be refused in every posture.
// It is now put to the person, covers that one command, and stays refused where
// nobody can be asked.
func TestEffectRecursiveDeleteOutsideTheWorkspaceWaitsForAPerson(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the delete runs through a POSIX shell")
	}
	t.Run("allowed, so the folder goes", func(t *testing.T) {
		run := runOutsideCalls(t, true, func(event.Approval) (bool, bool, bool) { return true, true, true }, withVictim(t))
		if len(run.cards) != 1 || run.cards[0].Tool != control.DeleteScopeApprovalTool || victimLeft(run) {
			t.Fatalf("cards=%+v victimLeft=%v results=%v", run.cards, victimLeft(run), run.results)
		}
		if run.cards[0].AllowsSession || run.cards[0].AllowsPersist || !run.cards[0].Fresh {
			t.Fatalf("a delete card must be one-command only: %+v", run.cards[0])
		}
	})
	t.Run("declined, so the folder stays", func(t *testing.T) {
		run := runOutsideCalls(t, true, func(event.Approval) (bool, bool, bool) { return false, false, false }, withVictim(t))
		if len(run.cards) != 1 || !victimLeft(run) || !strings.Contains(run.results["rm"], "do not retry") {
			t.Fatalf("cards=%d victimLeft=%v result=%q", len(run.cards), victimLeft(run), run.results["rm"])
		}
	})
	t.Run("nobody to ask, so the refusal stands", func(t *testing.T) {
		run := runOutsideCalls(t, false, func(event.Approval) (bool, bool, bool) { return true, false, false }, withVictim(t))
		if len(run.cards) != 0 || !victimLeft(run) || !strings.Contains(run.results["rm"], "shell.destructive_target") && !strings.Contains(run.results["rm"], "outside the workspace") {
			t.Fatalf("cards=%d victimLeft=%v result=%q", len(run.cards), victimLeft(run), run.results["rm"])
		}
	})
}
