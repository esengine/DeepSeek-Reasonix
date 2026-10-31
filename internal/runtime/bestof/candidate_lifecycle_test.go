package bestof

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/isolation"
)

func TestBestOfPanicKeepsOriginalStoreResultsAcrossRestart(t *testing.T) {
	repo, managed := repoWithFile(t), t.TempDir()
	store := isolation.NewStore(managed, repo, opened(t, repo))
	var arrived sync.WaitGroup
	arrived.Add(2)
	judge := &judgeStub{reply: `{"winner":2}`}
	panicCause := errors.New("candidate host panic")
	runner := func(_ context.Context, run Run) (Outcome, error) {
		if err := os.WriteFile(filepath.Join(run.WorkspaceRoot, "a.txt"), []byte("kept candidate\n"), 0o644); err != nil {
			return Outcome{}, err
		}
		arrived.Done()
		arrived.Wait()
		if run.Index == 1 {
			panic(panicCause)
		}
		return Outcome{Answer: "healthy sibling"}, nil
	}
	task := New(Spec{Store: store, Runner: runner, Judge: JudgeSpec{Provider: judge}})
	out, err := task.Execute(t.Context(), json.RawMessage(`{"prompt":"write then panic","n":2}`))
	if !errors.Is(err, isolation.ErrRunnerPanic) || !errors.Is(err, panicCause) {
		t.Fatalf("panic identity lost: %v", err)
	}
	if len(judge.seen) != 0 || readA(t, repo) != "start\n" {
		t.Fatal("unknown group reached judge or publisher")
	}
	ids := store.Pending()
	if len(ids) != 2 || worktreeCount(t, repo) != 3 {
		t.Fatalf("lost candidate identity or files: %v", ids)
	}
	for _, id := range ids {
		if !strings.Contains(out, id) {
			t.Fatalf("result omitted original ID %s: %s", id, out)
		}
	}
	restarted := isolation.NewStore(managed, repo, opened(t, repo))
	if !slices.Equal(ids, restarted.Pending()) {
		t.Fatal("restart minted or lost IDs")
	}
	unknown := 0
	for _, id := range ids {
		_, err := restarted.Paths(id)
		if errors.Is(err, isolation.ErrCandidateUnsettled) {
			unknown++
			if _, err := restarted.Discard(t.Context(), id); !errors.Is(err, isolation.ErrCandidateUnsettled) {
				t.Fatal(err)
			}
		}
	}
	if unknown != 1 {
		t.Fatalf("unknown candidates: %d", unknown)
	}
	restarted.SweepExpired(t.Context(), time.Hour)
	if !slices.Equal(ids, restarted.Pending()) {
		t.Fatal("sweep lost retained group")
	}
}

func TestBestOfConflictRecoversTheOriginalWinner(t *testing.T) {
	repo, managed := repoWithFile(t), t.TempDir()
	store := isolation.NewStore(managed, repo, opened(t, repo))
	runner := func(ctx context.Context, run Run) (Outcome, error) {
		if run.Index == 1 {
			if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("user edit\n"), 0o644); err != nil {
				return Outcome{}, err
			}
		}
		return writer()(ctx, run)
	}
	task := New(Spec{Store: store, Runner: runner, Judge: JudgeSpec{Provider: &judgeStub{reply: `{"winner":2}`}}})
	out, err := task.Execute(t.Context(), json.RawMessage(`{"prompt":"write","n":2}`))
	if err != nil {
		t.Fatal(err)
	}
	ids := store.Pending()
	if len(ids) != 1 || !strings.Contains(out, ids[0]) {
		t.Fatalf("winner ID missing: %v %s", ids, out)
	}
	restarted := isolation.NewStore(managed, repo, opened(t, repo))
	if _, err := restarted.Apply(t.Context(), ids[0]); err == nil {
		t.Fatal("conflict was ignored")
	}
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("start\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := restarted.Apply(t.Context(), ids[0])
	if err != nil || e.ID != ids[0] || readA(t, repo) != "attempt 2\n" {
		t.Fatalf("same-ID recovery: %v %v", e, err)
	}
}

func TestBestOfNilPanicIsNotACompletedCandidate(t *testing.T) {
	t.Setenv("GODEBUG", "panicnil=1")
	_, err := invokeCandidate(t.Context(), func(context.Context, Run) (Outcome, error) { panic(nil) }, Run{})
	if !errors.Is(err, isolation.ErrRunnerPanic) {
		t.Fatalf("nil panic became normal completion: %v", err)
	}
}

type panickingJudge struct{}

func (panickingJudge) Name() string { return "panic-judge" }
func (panickingJudge) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	panic("judge failure")
}

func TestBestOfJudgePanicRetainsRecoverableOriginalIDs(t *testing.T) {
	repo, managed := repoWithFile(t), t.TempDir()
	store := isolation.NewStore(managed, repo, opened(t, repo))
	task := New(Spec{Store: store, Runner: writer(), Judge: JudgeSpec{Provider: panickingJudge{}}})
	out, err := task.Execute(t.Context(), json.RawMessage(`{"prompt":"write","n":2}`))
	if !errors.Is(err, ErrJudgePanic) {
		t.Fatal(err)
	}
	peer := isolation.NewStore(managed, repo, opened(t, repo))
	ids := peer.Pending()
	if len(ids) != 2 || readA(t, repo) != "start\n" {
		t.Fatal("judge panic applied or removed candidates", ids)
	}
	for _, id := range ids {
		if !strings.Contains(out, id) {
			t.Fatal("judge panic hid original ID", out)
		}
		if _, err := peer.Discard(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
}
