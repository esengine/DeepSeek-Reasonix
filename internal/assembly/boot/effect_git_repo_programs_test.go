package boot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

// A git read in a repository that names a program in its own config is put to
// the person instead of running it; the same read in a plain repository runs.
func TestEffectGitReadInARepositoryThatRunsProgramsAsksFirst(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the payload is a POSIX script")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	for _, carries := range []bool{true, false} {
		isolateConfigHome(t)
		dir := robustTempDir(t)
		t.Chdir(dir)
		for _, args := range [][]string{{"init", "--quiet", "-b", "main"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"}} {
			if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v %s", args, err, out)
			}
		}
		marker := filepath.Join(dir, "ran.txt")
		script := filepath.Join(dir, "hook.sh")
		writeFile(t, dir, "hook.sh", "#!/bin/sh\necho ran > "+marker+"\n")
		if err := os.Chmod(script, 0o755); err != nil {
			t.Fatal(err)
		}
		if carries {
			if out, err := exec.Command("git", "-C", dir, "config", "core.fsmonitor", script).CombinedOutput(); err != nil {
				t.Fatalf("git config: %v %s", err, out)
			}
		}
		usePostureProvider(&postureProvider{calls: []provider.ToolCall{bashCall("c", "git status")}})
		writeFile(t, dir, "reasonix.toml", "default_model = \"test-model\"\n\n[codegraph]\nenabled = false\n\n[[providers]]\nname = \"test-model\"\nkind = \"boot-posture\"\nmodel = \"x\"\n")
		approveWorkspace(t, dir)
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
		_ = ctrl.Run(context.Background(), "look at the repository")
		ctrl.Close()
		mu.Lock()
		got := append([]event.Approval(nil), asked...)
		mu.Unlock()
		_, ranErr := os.Stat(marker)
		if carries {
			if ranErr == nil {
				t.Fatal("the repository's program ran before anyone approved it")
			}
			if len(got) != 1 || !strings.Contains(got[0].Reason, "core.fsmonitor") {
				t.Fatalf("approvals = %+v, want one that names core.fsmonitor", got)
			}
		} else if len(got) != 0 {
			t.Fatalf("a plain repository's read asked for approval: %+v", got)
		}
	}
}
