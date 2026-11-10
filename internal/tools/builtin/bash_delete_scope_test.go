package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/sandbox"
)

// stubDeleteApprover scripts the delete-scope question and records what it was
// asked; it also stands in as the managed-config approver, the way the
// controller's one approver does.
type stubDeleteApprover struct {
	stubConfigWriteApprover
	allow  bool
	reason string
	asked  [][]string
}

func (s *stubDeleteApprover) ApproveDeleteScope(_ context.Context, req tool.DeleteScopeRequest) (bool, string, error) {
	s.asked = append(s.asked, req.Paths)
	return s.allow, s.reason, nil
}

type deleteScopeRig struct {
	root, outside, victim string
	terminal              *deleteGateTerminal
	b                     bash
}

func newDeleteScopeRig(t *testing.T) *deleteScopeRig {
	t.Helper()
	t.Setenv("REASONIX_FILTER_SUBPROCESS_ENV", "")
	r := &deleteScopeRig{root: testenv.TempDir(t), outside: testenv.TempDir(t), terminal: &deleteGateTerminal{}}
	r.victim = filepath.Join(r.outside, "victim")
	if err := os.MkdirAll(r.victim, 0o755); err != nil {
		t.Fatal(err)
	}
	r.b = bash{shell: sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"}, workDir: r.root, sb: sandbox.Spec{WriteRoots: []string{r.root}}, terminal: r.terminal}
	return r
}

func (r *deleteScopeRig) run(ctx context.Context, command string) error {
	args, _ := json.Marshal(map[string]string{"command": command})
	_, err := r.b.ExecuteDetailed(ctx, args)
	return err
}

func TestRecursiveDeleteOutsideTheScopeIsAskedAboutWhereSomeoneCanBeAsked(t *testing.T) {
	r := newDeleteScopeRig(t)
	approver := &stubDeleteApprover{allow: true}
	ctx := tool.WithConfigWriteApprover(context.Background(), approver)
	if err := r.run(ctx, "rm -rf '"+filepath.ToSlash(r.victim)+"'"); err != nil {
		t.Fatalf("an allowed delete should run: %v", err)
	}
	if r.terminal.calls != 1 {
		t.Fatalf("the command did not run after approval: calls=%d", r.terminal.calls)
	}
	if len(approver.asked) != 1 || len(approver.asked[0]) != 1 || !strings.HasSuffix(filepath.ToSlash(approver.asked[0][0]), "/victim") {
		t.Fatalf("asked = %v, want one question naming the target", approver.asked)
	}
}

func TestRecursiveDeleteOfSeveralOutsideTargetsIsOneQuestion(t *testing.T) {
	r := newDeleteScopeRig(t)
	other := filepath.Join(r.outside, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	approver := &stubDeleteApprover{allow: true}
	ctx := tool.WithConfigWriteApprover(context.Background(), approver)
	command := "rm -rf '" + filepath.ToSlash(r.victim) + "' '" + filepath.ToSlash(other) + "' && ls -d '" + filepath.ToSlash(r.outside) + "'"
	if err := r.run(ctx, command); err != nil {
		t.Fatalf("an allowed delete should run: %v", err)
	}
	if len(approver.asked) != 1 || len(approver.asked[0]) != 2 {
		t.Fatalf("asked = %v, want one question naming both targets", approver.asked)
	}
}

func TestRecursiveDeleteStaysRefusedWhenTheUserDeclines(t *testing.T) {
	r := newDeleteScopeRig(t)
	ctx := tool.WithConfigWriteApprover(context.Background(), &stubDeleteApprover{reason: "the user declined this delete; do not retry it"})
	err := r.run(ctx, "rm -rf '"+filepath.ToSlash(r.victim)+"'")
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != CodeDestructiveTarget || !strings.Contains(refusal.Message, "declined") {
		t.Fatalf("want the destructive-target refusal carrying the reason, got %v", err)
	}
	if r.terminal.calls != 0 {
		t.Fatalf("a declined delete reached the shell: calls=%d", r.terminal.calls)
	}
}

func TestRecursiveDeleteOutsideTheScopeIsRefusedWithNobodyToAsk(t *testing.T) {
	r := newDeleteScopeRig(t)
	for name, ctx := range map[string]context.Context{
		"no approver":                        context.Background(),
		"an approver that only knows config": tool.WithConfigWriteApprover(context.Background(), &stubConfigWriteApprover{allow: true}),
	} {
		err := r.run(ctx, "rm -rf '"+filepath.ToSlash(r.victim)+"'")
		var refusal tool.Refusal
		if !errors.As(err, &refusal) || refusal.Code != CodeDestructiveTarget {
			t.Fatalf("%s: want the destructive-target refusal, got %v", name, err)
		}
	}
	if r.terminal.calls != 0 {
		t.Fatalf("a refused delete reached the shell: calls=%d", r.terminal.calls)
	}
}

// No answer makes these deletable, so the question is never put: a filesystem
// root, the home directory, the workspace itself and its ancestors, and any
// target a variable or glob leaves unbounded.
func TestRecursiveDeleteOfWhatNobodyShouldDeleteIsNeverAskedAbout(t *testing.T) {
	r := newDeleteScopeRig(t)
	home := testenv.TempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	approver := &stubDeleteApprover{allow: true}
	ctx := tool.WithConfigWriteApprover(context.Background(), approver)
	for _, command := range []string{
		"rm -rf /",
		"rm -rf '" + filepath.ToSlash(home) + "'",
		"rm -rf .",
		"rm -rf ..",
		`rm -rf "$target"`,
		"rm -rf '" + filepath.ToSlash(r.outside) + "'/*",
	} {
		if err := r.run(ctx, command); err == nil {
			t.Errorf("%q must stay refused even with an approver that says yes", command)
		}
	}
	if len(approver.asked) != 0 || r.terminal.calls != 0 {
		t.Fatalf("the question was put (%v) or something ran (%d)", approver.asked, r.terminal.calls)
	}
}
