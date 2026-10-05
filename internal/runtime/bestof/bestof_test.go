package bestof

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

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/gitcmd"
)

// opened is dir's identity as a session opening it resolves it.
func opened(t *testing.T, dir string) gitcmd.Repo {
	t.Helper()
	repo, err := gitcmd.Open(context.Background(), dir)
	if err != nil {
		return gitcmd.Repo{Dir: dir}
	}
	return repo
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := gitcmd.Command(context.Background(), dir, args...)
	cmd.Env = append(cmd.Env, "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func repoWithFile(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := testenv.TempDir(t)
	git(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("start\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "init")
	return repo
}

type judgeStub struct {
	reply string
	mu    sync.Mutex
	seen  []string
}

func (j *judgeStub) Name() string { return "judge-stub" }

func (j *judgeStub) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	j.mu.Lock()
	j.seen = append(j.seen, req.Messages[len(req.Messages)-1].Content)
	j.mu.Unlock()
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: j.reply}
	close(ch)
	return ch, nil
}

// writer is a Runner whose attempt k writes "attempt k" into a.txt, and whose
// attempts listed in fail return an error instead.
func writer(fail ...int) Runner {
	return func(_ context.Context, run Run) (Outcome, error) {
		if slices.Contains(fail, run.Index) {
			return Outcome{}, errors.New("attempt crashed")
		}
		body := []byte("attempt " + string(rune('0'+run.Index)) + "\n")
		if err := os.WriteFile(filepath.Join(run.WorkspaceRoot, "a.txt"), body, 0o644); err != nil {
			return Outcome{}, err
		}
		return Outcome{Answer: "I rewrote a.txt"}, nil
	}
}

func readA(t *testing.T, repo string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repo, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func worktreeCount(t *testing.T, repo string) int {
	t.Helper()
	return strings.Count(git(t, repo, "worktree", "list", "--porcelain"), "worktree ")
}

func TestBestOfAppliesTheJudgesPickAndRemovesEveryWorktree(t *testing.T) {
	repo := repoWithFile(t)
	judge := &judgeStub{reply: `{"winner": 2, "reason": "cleaner"}`}
	tl := New(Spec{Repo: opened(t, repo), ManagedRoot: filepath.Join(testenv.TempDir(t), "c"), Runner: writer(), Judge: JudgeSpec{Provider: judge}})
	out, err := tl.Execute(t.Context(), json.RawMessage(`{"prompt":"rewrite a.txt","n":3}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := readA(t, repo); got != "attempt 2\n" {
		t.Fatalf("a.txt = %q, want the judge's pick", got)
	}
	if !strings.Contains(out, "Attempt 2 won") || !strings.Contains(out, "Judge: cleaner") {
		t.Fatalf("report = %q", out)
	}
	if n := worktreeCount(t, repo); n != 1 {
		t.Fatalf("%d worktrees left, want only the workspace", n)
	}
	if len(judge.seen) != 1 || !strings.Contains(judge.seen[0], "+attempt 3") {
		t.Fatalf("the judge did not see every attempt's diff")
	}
}

// The judge and the parent both read each attempt's host record next to its
// diff: a failed check and a rewritten test are observations, not claims.
func TestBestOfShowsEachAttemptsHostRecord(t *testing.T) {
	repo := repoWithFile(t)
	judge := &judgeStub{reply: `{"winner": 1, "reason": "its checks held"}`}
	base := writer()
	runner := func(ctx context.Context, run Run) (Outcome, error) {
		out, err := base(ctx, run)
		if run.Index == 2 {
			out.Host = &event.CompletionSummaryInfo{Verdict: "complete", ChecksPassed: 1, ChecksFailed: 1, CriteriaRewritten: []string{"a_test.go:TestA"}}
		}
		return out, err
	}
	tl := New(Spec{Repo: opened(t, repo), ManagedRoot: filepath.Join(testenv.TempDir(t), "c"), Runner: runner, Judge: JudgeSpec{Provider: judge}})
	out, err := tl.Execute(t.Context(), json.RawMessage(`{"prompt":"rewrite a.txt","n":2}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(judge.seen) != 1 {
		t.Fatalf("judge calls = %d", len(judge.seen))
	}
	seen := judge.seen[0]
	for _, want := range []string{
		"no completion summary",
		"checks 1 passed, 1 failed, 0 suppressed",
		"rewrote or removed existing tests: a_test.go:TestA",
	} {
		if !strings.Contains(seen, want) {
			t.Fatalf("judge evidence missing %q:\n%s", want, seen)
		}
	}
	if !strings.Contains(out, "Host: verdict complete; checks 1 passed, 1 failed") {
		t.Fatalf("report does not carry the host record: %q", out)
	}
}

// With one attempt finished there is nothing to compare, so no judge is paid.
func TestBestOfSkipsTheJudgeWhenOneAttemptFinished(t *testing.T) {
	repo := repoWithFile(t)
	judge := &judgeStub{reply: `{"winner": 1}`}
	tl := New(Spec{Repo: opened(t, repo), ManagedRoot: filepath.Join(testenv.TempDir(t), "c"), Runner: writer(1), Judge: JudgeSpec{Provider: judge}})
	out, err := tl.Execute(t.Context(), json.RawMessage(`{"prompt":"rewrite a.txt","n":2}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if readA(t, repo) != "attempt 2\n" || len(judge.seen) != 0 {
		t.Fatalf("a.txt = %q, judge calls = %d", readA(t, repo), len(judge.seen))
	}
	if !strings.Contains(out, "failed — attempt crashed") {
		t.Fatalf("the failed attempt is not reported: %q", out)
	}
}

// A judge that names no finished attempt decides nothing: the workspace is
// untouched and the failure is the tool's, not a silent default.
func TestBestOfAppliesNothingWhenTheJudgeDoesNotDecide(t *testing.T) {
	repo := repoWithFile(t)
	tl := New(Spec{Repo: opened(t, repo), ManagedRoot: filepath.Join(testenv.TempDir(t), "c"), Runner: writer(),
		Judge: JudgeSpec{Provider: &judgeStub{reply: `{"winner": 7}`}}})
	if _, err := tl.Execute(t.Context(), json.RawMessage(`{"prompt":"rewrite a.txt","n":2}`)); !errors.Is(err, errJudgeReply) {
		t.Fatalf("err = %v, want errJudgeReply", err)
	}
	if readA(t, repo) != "start\n" || worktreeCount(t, repo) != 1 {
		t.Fatal("an undecided run touched the workspace or left worktrees")
	}
}

func TestBestOfReportsWhenEveryAttemptFailed(t *testing.T) {
	repo := repoWithFile(t)
	tl := New(Spec{Repo: opened(t, repo), ManagedRoot: filepath.Join(testenv.TempDir(t), "c"), Runner: writer(1, 2)})
	if _, err := tl.Execute(t.Context(), json.RawMessage(`{"prompt":"x","n":2}`)); !errors.Is(err, ErrNoCandidateFinished) {
		t.Fatalf("err = %v, want ErrNoCandidateFinished", err)
	}
}

// The user kept editing while the attempts ran: the winner is not written over
// that work, and its worktree stays so nothing it did is lost.
func TestBestOfKeepsTheWinnerWhenTheWorkspaceMoved(t *testing.T) {
	repo := repoWithFile(t)
	moving := func(ctx context.Context, run Run) (Outcome, error) {
		if run.Index == 1 {
			_ = os.WriteFile(filepath.Join(repo, "a.txt"), []byte("user edit\n"), 0o644)
		}
		return writer()(ctx, run)
	}
	tl := New(Spec{Repo: opened(t, repo), ManagedRoot: filepath.Join(testenv.TempDir(t), "c"), Runner: moving,
		Judge: JudgeSpec{Provider: &judgeStub{reply: `{"winner": 1, "reason": "r"}`}}})
	out, err := tl.Execute(t.Context(), json.RawMessage(`{"prompt":"x","n":2}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if readA(t, repo) != "user edit\n" || !strings.Contains(out, "NOT applied") {
		t.Fatalf("a.txt = %q, report %q", readA(t, repo), out)
	}
	if n := worktreeCount(t, repo); n != 2 {
		t.Fatalf("%d worktrees, want the workspace and the kept winner", n)
	}
}

func TestBestOfRefusesBadArguments(t *testing.T) {
	tl := New(Spec{KnownModel: func(ref string) bool { return ref == "flash" }})
	for _, raw := range []string{`{"prompt":""}`, `{"prompt":"x","n":9}`, `{"prompt":"x","models":["nope"]}`} {
		if _, err := tl.Execute(t.Context(), json.RawMessage(raw)); err == nil {
			t.Fatalf("%s was accepted", raw)
		}
	}
}
