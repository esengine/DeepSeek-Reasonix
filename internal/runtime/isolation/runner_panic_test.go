package isolation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// A host runner can unwind after a real write. The result must have the same
// recovery identity as an ordinary error, even when final inspection fails.
func TestRunnerPanicRetainsDiscoverableWork(t *testing.T) {
	for _, fault := range []string{"none", "cancel-and-inspection", "publication"} {
		t.Run(fault, func(t *testing.T) {
			repo, managed := gitRepo(t), testenv.TempDir(t)
			store := NewStore(managed, repo, opened(t, repo))
			cause := errors.New("runner failed after writing")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			originalPath := os.Getenv("PATH")
			var candidateRoot, blocked string
			_, panicValue, runErr := executeCatchingPanic(store, ctx, func(_ context.Context, run Run) (Outcome, error) {
				candidateRoot = run.WorkspaceRoot
				write(t, candidateRoot, "a.txt", "retained after panic\n")
				switch fault {
				case "cancel-and-inspection":
					cancel()
					t.Setenv("PATH", testenv.TempDir(t))
				case "publication":
					blocked = filepath.Join(filepath.Dir(candidateRoot), inspectionFile)
					if err := os.Mkdir(blocked, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				panic(cause)
			}, repo)
			t.Setenv("PATH", originalPath)
			if panicValue != nil {
				t.Fatalf("runner panic escaped before result finalization: %v", panicValue)
			}
			if !errors.Is(runErr, cause) || !errors.Is(runErr, ErrRunnerPanic) {
				t.Fatalf("run error lost panic cause: %v", runErr)
			}
			id := idOf(t, runErr.Error())
			if got := read(t, repo, "a.txt"); got != "one\n" {
				t.Fatalf("runner reached source before apply: %q", got)
			}
			if got := read(t, candidateRoot, "a.txt"); got != "retained after panic\n" {
				t.Fatalf("panic lost candidate bytes: %q", got)
			}
			resumed := NewStore(managed, repo, opened(t, repo))
			if blocked != "" {
				if !strings.Contains(runErr.Error(), "only this live store") || !slices.Contains(store.Pending(), id) {
					t.Fatalf("failed publication did not disclose live-only recovery: %v, %v", runErr, store.Pending())
				}
				if slices.Contains(resumed.Pending(), id) {
					t.Fatal("failed publication falsely established restart durability")
				}
				resumed = store
			} else if !slices.Contains(resumed.Pending(), id) {
				t.Fatalf("runner panic lost durable identity %s: %v", id, resumed.Pending())
			}
			resumed.SweepExpired(t.Context(), -time.Second)
			if got := read(t, candidateRoot, "a.txt"); got != "retained after panic\n" {
				t.Fatalf("sweep removed uninspected panic work: %q", got)
			}
			if blocked != "" {
				if err := os.Remove(blocked); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := resumed.Apply(t.Context(), id); err != nil {
				t.Fatalf("apply retained panic result after Store rebuild: %v", err)
			}
			if got := read(t, repo, "a.txt"); got != "retained after panic\n" {
				t.Fatalf("apply did not use retained bytes: %q", got)
			}
		})
	}
}

func TestRunnerPanicBeforeWritingKeepsInspectionIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, debug string
		value       any
	}{{name: "string", value: "host callback panic"}, {name: "nil"}, {name: "legacy-nil", debug: "panicnil=1"}} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.debug != "" {
				t.Setenv("GODEBUG", tc.debug)
			}
			repo, managed := gitRepo(t), testenv.TempDir(t)
			store := NewStore(managed, repo, opened(t, repo))
			_, escaped, runErr := executeCatchingPanic(store, t.Context(), func(context.Context, Run) (Outcome, error) {
				panic(tc.value)
			}, repo)
			if escaped != nil || !errors.Is(runErr, ErrRunnerPanic) {
				t.Fatalf("runner panic = %v, error = %v; want finalized error", escaped, runErr)
			}
			id := idOf(t, runErr.Error())
			if !slices.Contains(NewStore(managed, repo, opened(t, repo)).Pending(), id) {
				t.Fatalf("panic was treated as a proven empty result: %v, %v", runErr, store.Pending())
			}
		})
	}
}

func TestRunnerInvocationPreservesReturnedErrors(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprintf("changed=%t", changed), func(t *testing.T) {
			repo, managed := gitRepo(t), testenv.TempDir(t)
			store := NewStore(managed, repo, opened(t, repo))
			cause := errors.New("ordinary runner failure")
			_, err := store.Execute(t.Context(), func(_ context.Context, run Run) (Outcome, error) {
				if changed {
					write(t, run.WorkspaceRoot, "a.txt", "ordinary failed work\n")
				}
				return Outcome{}, cause
			}, repo, "run", "")
			if !errors.Is(err, cause) || errors.Is(err, ErrRunnerPanic) {
				t.Fatalf("ordinary runner failure was reclassified: %v", err)
			}
			if !changed {
				if len(store.Pending()) != 0 {
					t.Fatalf("unchanged ordinary error = %v, pending = %v", err, store.Pending())
				}
				return
			}
			id := idOf(t, err.Error())
			if _, err := store.Discard(t.Context(), id); err != nil {
				t.Fatalf("discard ordinary failed work: %v", err)
			}
		})
	}
}

func executeCatchingPanic(store *Store, ctx context.Context, runner Runner, repo string) (report string, panicValue any, err error) {
	defer func() { panicValue = recover() }()
	report, err = store.Execute(ctx, runner, repo, "run", "")
	return
}
