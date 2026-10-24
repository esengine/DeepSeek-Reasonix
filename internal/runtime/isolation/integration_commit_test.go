package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/tools/shellrun"
)

func TestIntegrationCheckProcess(t *testing.T) {
	if os.Getenv("REASONIX_INTEGRATION_CHECK_PROCESS") == "1" {
		os.Exit(0)
	}
}

func checkedIntegration(t *testing.T, prepare ...func(*Store)) (*Store, *Entry, *Integration) {
	t.Helper()
	store, entry := integrationFixture(t)
	// Match the directory semantics that Git actually materializes. A separate
	// regression keeps intentionally different source modes and ignored inputs.
	if err := os.Chmod(store.workspace, 0755); err != nil {
		t.Fatal(err)
	}
	for _, setup := range prepare {
		setup(store)
	}
	i, err := store.PrepareIntegration(t.Context(), entry.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(i.Close)
	checkPreparedIntegration(t, store, i)
	return store, entry, i
}

func checkPreparedIntegration(t *testing.T, store *Store, i *Integration) {
	t.Helper()
	command := "integration-check-process"
	digest := evidence.CheckCommandDigest(command)
	contract := CheckContract{Identity: "frozen-host-contract", CommandDigest: digest}
	if err := i.BeginChecks(t.Context(), []CheckContract{contract}); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	observer := evidence.NewCheckInputObserver(i.attempt.Candidate.WorktreeRoot)
	result := shellrun.RunForeground(tool.WithShellLaunchObserver(t.Context(), observer), shellrun.Request{
		Argv: []string{executable, "-test.run=^TestIntegrationCheckProcess$"}, Dir: i.WorkspaceRoot(),
		Env: append(os.Environ(), "REASONIX_INTEGRATION_CHECK_PROCESS=1"), ShellPath: executable, Command: command,
	})
	if result.Err != nil || result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("actual check process failed: %+v", result)
	}
	inputs := observer.Inputs()
	checks := []CheckEvidence{{ExecutionID: "fixture-host-execution", Contract: contract.Identity, CommandDigest: digest, Success: true, Inputs: inputs}}
	if err := i.FinishChecks(t.Context(), checks); err != nil {
		t.Fatal(err)
	}
	if i.Status().Verification != "unknown" {
		t.Fatal("actual local launch incorrectly claimed external-input completeness")
	}
	if err := i.BeginChecks(t.Context(), []CheckContract{contract}); err != nil {
		t.Fatal(err)
	}
	// Replace only the environment coverage owner for this state-machine test.
	// Production has no override: a real environment owner must discharge gaps.
	checks[0].Inputs[0].Gaps = nil
	checks[0].Inputs[0].EnvironmentReceipt = "test-only-environment-owner"
	store.checkVerifier = func(input evidence.CheckInputs, execution string) bool {
		return input.EnvironmentReceipt == "test-only-environment-owner" && execution == "fixture-host-execution"
	}
	if err := i.FinishChecks(t.Context(), checks); err != nil {
		t.Fatal(err)
	}
	if i.Status().Verification != "valid" {
		t.Fatalf("fixture did not reach commit boundary: %+v", i.Status())
	}
}

func TestVerifiedIntegrationPublishesAndRetriesIdempotently(t *testing.T) {
	store, original, i := checkedIntegration(t)
	authorize := func(context.Context, []string) error { return nil }
	if err := i.Commit(t.Context(), authorize); err != nil {
		t.Fatal(err)
	}
	if got := read(t, store.workspace, "a.txt"); got != "candidate\n" {
		t.Fatalf("verified target not published: %q", got)
	}
	if status := i.Status(); status.State != Integrated || !status.CleanupComplete {
		t.Fatalf("commit did not retain terminal receipt: %+v", status)
	}
	write(t, store.workspace, "a.txt", "later user edit\n")
	if err := i.Commit(t.Context(), authorize); err != nil {
		t.Fatal(err)
	}
	if got := read(t, store.workspace, "a.txt"); got != "later user edit\n" {
		t.Fatalf("idempotent retry overwrote a later edit: %q", got)
	}
	retained, err := store.operationSnapshot(t.Context(), original.ID)
	if err != nil || retained.State != Integrated {
		t.Fatalf("original receipt lost: %+v %v", retained, err)
	}
}

func TestIntegrationSourceDriftInvalidatesCheckedAttempt(t *testing.T) {
	store, _, i := checkedIntegration(t)
	write(t, store.workspace, "unrelated-input", "changed after verification\n")
	err := i.Commit(t.Context(), func(context.Context, []string) error { return nil })
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != CodeStaleExecution || i.Status().Verification != "invalid" {
		t.Fatalf("old receipt reused after input drift: %v %+v", err, i.Status())
	}
	if got := read(t, store.workspace, "a.txt"); got != "one\n" {
		t.Fatalf("stale checked attempt published: %q", got)
	}
}

func TestIntegrationRejectsDifferentPostimageBeforeAnySourceWrite(t *testing.T) {
	for _, kind := range []string{"ignored-dependency", "empty-directory", "directory-mode", "file-mode-0600", "file-mode-0640", "checkout-filter"} {
		t.Run(kind, func(t *testing.T) {
			store, original, i := checkedIntegration(t, func(store *Store) {
				switch kind {
				case "checkout-filter":
					write(t, store.workspace, ".gitattributes", "input text eol=crlf\n")
					write(t, store.workspace, "input", "input\n")
				case "ignored-dependency":
					write(t, store.workspace, ".gitignore", "ignored\n")
					write(t, store.workspace, "ignored", "local dependency\n")
				case "empty-directory":
					if err := os.MkdirAll(filepath.Join(store.workspace, "empty"), 0755); err != nil {
						t.Fatal(err)
					}
				case "directory-mode":
					write(t, store.workspace, "localdir/input", "input\n")
					if err := os.Chmod(filepath.Join(store.workspace, "localdir"), 0700); err != nil {
						t.Fatal(err)
					}
				case "file-mode-0600", "file-mode-0640":
					mode := os.FileMode(0600)
					if kind == "file-mode-0640" {
						mode = 0640
					}
					if err := os.Chmod(filepath.Join(store.workspace, "a.txt"), mode); err != nil {
						t.Fatal(err)
					}
				}
			})
			modeEquivalent := false
			if kind == "directory-mode" {
				sourceMode, sourceErr := os.Stat(filepath.Join(store.workspace, "localdir"))
				candidateMode, candidateErr := os.Stat(filepath.Join(i.WorkspaceRoot(), "localdir"))
				// Some filesystems expose no distinct POSIX directory modes.
				// Such a representation is already equivalent, not a mismatch.
				modeEquivalent = sourceErr == nil && candidateErr == nil && sourceMode.Mode().Perm() == candidateMode.Mode().Perm()
			}
			if kind == "file-mode-0600" || kind == "file-mode-0640" {
				sourceMode, sourceErr := os.Stat(filepath.Join(store.workspace, "a.txt"))
				candidateMode, candidateErr := os.Stat(filepath.Join(i.WorkspaceRoot(), "a.txt"))
				modeEquivalent = sourceErr == nil && candidateErr == nil && sourceMode.Mode().Perm() == candidateMode.Mode().Perm()
			}
			err := i.Commit(t.Context(), func(context.Context, []string) error { return nil })
			if modeEquivalent {
				if err != nil {
					t.Fatalf("equivalent directory modes refused: %v", err)
				}
				return
			}
			var refusal tool.Refusal
			if !errors.As(err, &refusal) || refusal.Code != CodeStaleExecution || read(t, store.workspace, "a.txt") != "one\n" {
				t.Fatalf("different expected postimage was discovered after publication: %v %+v", err, i.Status())
			}
			current, err := store.operationSnapshot(t.Context(), original.ID)
			if err != nil || current.State != Pending {
				t.Fatalf("preflight failure changed the original result state: %+v %v", current, err)
			}
		})
	}
}

func TestIntegrationReauthorizesActualJournalRecoveryPaths(t *testing.T) {
	store, _, i := checkedIntegration(t)
	var calls int
	denied := errors.New("original grant excludes other.txt")
	err := i.Commit(t.Context(), func(_ context.Context, paths []string) error {
		calls++
		for _, path := range paths {
			if filepath.Base(path) == "other.txt" {
				return denied
			}
		}
		if calls == 1 {
			// Simulate a different process leaving a journal after pre-reading
			// recovery paths, before this transaction obtains the source scope.
			write(t, store.workspace, "other.txt", "after\n")
			journal := map[string]any{"version": 1, "root": store.repo.WorkTree, "tree": i.attempt.Tree, "started": 1,
				"files": []any{map[string]any{"path": "other.txt", "before": map[string]any{"Kind": "file", "Mode": 0644, "Data": []byte("before\n")},
					"after": map[string]any{"Kind": "file", "Mode": 0644, "Data": []byte("after\n")}}}}
			raw, _ := json.Marshal(journal)
			path := filepath.Join(store.repo.GitDir, "reasonix-apply-v1", "active.json")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
		}
		return nil
	})
	if !errors.Is(err, denied) || read(t, store.workspace, "other.txt") != "after\n" {
		t.Fatalf("coordination lease substituted for recovery permission: %v", err)
	}
}
