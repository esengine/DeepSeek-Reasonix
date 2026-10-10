package boot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/permission"
	"reasonix/internal/session/control"
)

// A rule on a program holds when a transparent wrapper or a path spelling
// stands between the call and the program. Each command runs in its own build
// because a blocked call makes the rest of its batch skip. The proof is at the
// boundary the model sees: the refusal identity, the rule it is told matched,
// and the file the command would have removed still on disk.
func bashRuleRun(t *testing.T, rule, mode, command string) (*postureRun, string) {
	t.Helper()
	run := buildPostureRun(t, "[permissions]\n"+rule+"\n", bashCall("c", command))
	victim := filepath.Join(run.dir, "victim.txt")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	run.runIn(t, mode)
	return run, victim
}

func TestEffectBashDenyRuleHoldsThroughWrappers(t *testing.T) {
	for _, cmd := range []string{
		"rm victim.txt",
		"env X=1 rm victim.txt",
		"sudo -n rm victim.txt",
		`env "X=1" rm victim.txt`,
		`env 'X=1' rm victim.txt`,
		`env "X"=1 rm victim.txt`,
		"env a-b=1 rm victim.txt",
		"env 1X=1 rm victim.txt",
		"env foo.bar=1 rm victim.txt",
		`env "A B=1" rm victim.txt`,
		`sudo 'X=1' rm victim.txt`,
		`sudo "X=1" rm victim.txt`,
		"doas rm victim.txt",
		"command rm victim.txt",
		"nohup rm victim.txt",
		"time rm victim.txt",
		"nice -n 5 rm victim.txt",
		"/bin/rm victim.txt",
		"'C:\\tools\\rm.exe' victim.txt",
		"C:/tools/rm.exe victim.txt",
		"echo hi && sudo rm victim.txt",
	} {
		t.Run(cmd, func(t *testing.T) {
			run, victim := bashRuleRun(t, `deny = ["Bash(rm:*)"]`, control.ToolApprovalYolo, cmd)
			if _, err := os.Stat(victim); err != nil {
				t.Fatalf("the denied program ran: %v", err)
			}
			if got := run.results["c"].RefusalCode; got != permission.RefusalDenyRule {
				t.Fatalf("refusal = %q, want %q", got, permission.RefusalDenyRule)
			}
			if saw := run.modelSaw(t, "c"); !strings.Contains(saw, "Matched permission rule: deny Bash(rm:*)") {
				t.Fatalf("the model is not told which rule matched: %q", saw)
			}
		})
	}
}

// An ask rule puts the wrapped form to the person as it does the bare one, and
// the approval they are shown names the rule that stopped it.
func TestEffectBashAskRuleHoldsThroughWrappers(t *testing.T) {
	for _, cmd := range []string{"rm victim.txt", "env X=1 rm victim.txt", "sudo rm victim.txt", "nohup /bin/rm victim.txt"} {
		t.Run(cmd, func(t *testing.T) {
			isolateConfigHome(t)
			dir := robustTempDir(t)
			t.Chdir(dir)
			usePostureProvider(&postureProvider{calls: []provider.ToolCall{bashCall("c", cmd)}})
			writeUserConfig(t, "[permissions]\nask = [\"Bash(rm:*)\"]\n")
			writeFile(t, dir, "reasonix.toml", "default_model = \"test-model\"\n\n[codegraph]\nenabled = false\n\n[[providers]]\nname = \"test-model\"\nkind = \"boot-posture\"\nmodel = \"x\"\n")
			approveWorkspace(t, dir)
			victim := filepath.Join(dir, "victim.txt")
			if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			var ref atomic.Pointer[control.Controller]
			var mu sync.Mutex
			var asked []event.Approval
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind != event.ApprovalRequest {
					return
				}
				mu.Lock()
				asked = append(asked, e.Approval)
				mu.Unlock()
				go ref.Load().Approve(e.Approval.ID, false, false, false)
			})
			ctrl, err := Build(context.Background(), Options{Sink: sink})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			ref.Store(ctrl)
			ctrl.EnableInteractiveApproval()
			t.Cleanup(func() { ctrl.Close() })
			_ = ctrl.Run(context.Background(), "do the task")
			if _, err := os.Stat(victim); err != nil {
				t.Fatalf("the ask-ruled program ran without approval: %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(asked) != 1 || !strings.Contains(asked[0].Reason, "ask Bash(rm:*)") {
				t.Fatalf("approvals = %+v, want one that names the ask rule", asked)
			}
		})
	}
}

// Peeling never widens an allow rule: the command it names runs, the wrapped
// form is not covered by it.
func TestEffectBashAllowRuleDoesNotCoverWrappedForm(t *testing.T) {
	plain, _ := bashRuleRun(t, `allow = ["Bash(echo:*)"]`, control.ToolApprovalAsk, "echo hello")
	if got := plain.results["c"].RefusalCode; got != "" {
		t.Fatalf("the allowed command was refused: %q", got)
	}
	wrapped, _ := bashRuleRun(t, `allow = ["Bash(echo:*)"]`, control.ToolApprovalAsk, "nohup echo hello")
	if got := wrapped.results["c"].RefusalCode; got == "" {
		t.Fatal("the allow rule covered a wrapped form it does not name")
	}
}

// An expansion inside the quotes is not a deny-rule match and not a run: the
// call is stopped for lack of an approver, with the unattended identity.
func TestEffectBashExpansionInQuotedAssignmentIsNotRun(t *testing.T) {
	run, victim := bashRuleRun(t, `deny = ["Bash(rm:*)"]`, control.ToolApprovalAsk, `env "X=$HOME" rm victim.txt`)
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("the call ran: %v", err)
	}
	if got := run.results["c"].RefusalCode; got != permission.RefusalUnattended {
		t.Fatalf("refusal = %q, want %q", got, permission.RefusalUnattended)
	}
}
