package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

func TestFirstFinishFailureRemainsDiscoverable(t *testing.T) {
	for _, fault := range []string{"inspection", "legacy-publication"} {
		t.Run(fault, func(t *testing.T) {
			repo := gitRepo(t)
			managed := testenv.TempDir(t)
			store := NewStore(managed, repo, opened(t, repo))
			e, err := store.Begin(t.Context(), repo)
			if err != nil {
				t.Fatal(err)
			}
			write(t, e.Cand.WorkspaceRoot, "a.txt", "unfinished inspection\n")
			var blocked string
			originalPath := os.Getenv("PATH")
			if fault == "inspection" {
				t.Setenv("PATH", testenv.TempDir(t))
			} else {
				blocked = entryPath(e.Cand)
				if err := os.Mkdir(blocked, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			kept, finishErr := store.Finish(t.Context(), e)
			t.Setenv("PATH", originalPath)
			if got := read(t, e.Cand.WorkspaceRoot, "a.txt"); got != "unfinished inspection\n" {
				t.Fatalf("candidate changed: %q", got)
			}
			if finishErr == nil || !kept {
				t.Fatalf("Finish = %v, %v; want retained, failed finalization", kept, finishErr)
			}
			restarted := NewStore(managed, repo, opened(t, repo))
			if !slices.Contains(restarted.Pending(), e.ID) {
				t.Fatalf("first Finish failure lost %s after rebuild: %v", e.ID, restarted.Pending())
			}
			restarted.SweepExpired(t.Context(), -time.Second)
			if !slices.Contains(restarted.Pending(), e.ID) {
				t.Fatal("age-based cleanup removed unfinished inspection")
			}
			if blocked != "" {
				if err := os.Remove(blocked); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := NewApplyTool(restarted).Execute(t.Context(), args(e.ID)); err != nil {
				t.Fatalf("same-ID retry after fault cleared: %v", err)
			}
			if got := read(t, repo, "a.txt"); got != "unfinished inspection\n" {
				t.Fatalf("retained result was not applied: %q", got)
			}
		})
	}
}

func failedInspection(t *testing.T) (string, string, *Entry) {
	t.Helper()
	repo := gitRepo(t)
	managed := testenv.TempDir(t)
	store := NewStore(managed, repo, opened(t, repo))
	e, err := store.Begin(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	write(t, e.Cand.WorkspaceRoot, "a.txt", "retained work\n")
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", testenv.TempDir(t))
	_, err = store.Finish(t.Context(), e)
	t.Setenv("PATH", originalPath)
	if err == nil {
		t.Fatal("injected unavailable Git did not fail Finish")
	}
	return repo, managed, e
}

func TestInspectionRecordsPreserveUnknownFieldsAndVersions(t *testing.T) {
	for _, version := range []int{inspectionVersion, inspectionVersion + 1} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			repo, managed, e := failedInspection(t)
			marker := inspectionPath(e.Cand)
			raw, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			fields["version"], _ = json.Marshal(version)
			fields["future"] = json.RawMessage(`{"keep":[1,2,3]}`)
			raw, err = json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(marker, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			restarted := NewStore(managed, repo, opened(t, repo))
			_, pathErr := restarted.Paths(e.ID)
			if version == inspectionVersion && pathErr != nil {
				t.Fatal(pathErr)
			}
			if version != inspectionVersion {
				var refusal tool.Refusal
				if !errors.As(pathErr, &refusal) || refusal.Code != CodeUnavailable || !strings.Contains(refusal.Message, "version") {
					t.Fatalf("unsupported record must refuse explicitly: %v", pathErr)
				}
				if _, err := restarted.Discard(t.Context(), e.ID); err == nil {
					t.Fatal("discard accepted an unsupported version")
				}
			}
			restarted.SweepExpired(t.Context(), -time.Second)
			after, err := os.ReadFile(marker)
			if err != nil || string(after) != string(raw) {
				t.Fatalf("discovery/inspection/sweep rewrote the record: %v", err)
			}
			if read(t, e.Cand.WorkspaceRoot, "a.txt") != "retained work\n" || read(t, repo, "a.txt") != "one\n" {
				t.Fatal("record inspection modified work")
			}
		})
	}
}

func TestUnavailableInspectionRecordKeepsLiveRetry(t *testing.T) {
	repo := gitRepo(t)
	managed := testenv.TempDir(t)
	store := NewStore(managed, repo, opened(t, repo))
	e, err := store.Begin(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	write(t, e.Cand.WorkspaceRoot, "a.txt", "recoverable in live store\n")
	if err := os.Mkdir(inspectionPath(e.Cand), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(entryPath(e.Cand), 0o700); err != nil {
		t.Fatal(err)
	}
	kept, err := store.Finish(t.Context(), e)
	if !kept || err == nil || !strings.Contains(err.Error(), "only this live store") {
		t.Fatalf("missing durable record was hidden: kept=%v err=%v", kept, err)
	}
	if !slices.Contains(store.Pending(), e.ID) {
		t.Fatal("live store lost failed result")
	}
	if _, err := store.Paths(e.ID); err == nil {
		t.Fatal("retry ignored unavailable durable recovery record")
	}
	for _, marker := range []string{inspectionPath(e.Cand), entryPath(e.Cand)} {
		if err := os.Remove(marker); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Paths(e.ID); err != nil {
		t.Fatal(err)
	}
	restarted := NewStore(managed, repo, opened(t, repo))
	if _, err := NewApplyTool(restarted).Execute(t.Context(), args(e.ID)); err != nil {
		t.Fatal(err)
	}
	if read(t, repo, "a.txt") != "recoverable in live store\n" {
		t.Fatal("same-ID retry lost content")
	}
}

func TestInspectionRecordCannotAuthorizeAnotherSlotOrWorkspace(t *testing.T) {
	repo, managed, e := failedInspection(t)
	raw, err := os.ReadFile(inspectionPath(e.Cand))
	if err != nil {
		t.Fatal(err)
	}
	write(t, managed, "copied/"+inspectionFile, string(raw))
	if err := os.Remove(inspectionPath(e.Cand)); err != nil {
		t.Fatal(err)
	}
	if got := NewStore(managed, repo, opened(t, repo)).Pending(); len(got) != 0 {
		t.Fatalf("copied marker authorized an outside candidate: %v", got)
	}
	if err := os.WriteFile(inspectionPath(e.Cand), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	other := gitRepo(t)
	foreign := NewStore(managed, other, opened(t, other))
	if got := foreign.Pending(); len(got) != 0 {
		t.Fatalf("foreign workspace admitted marker: %v", got)
	}
	foreign.SweepExpired(t.Context(), -time.Second)
	if got := read(t, e.Cand.WorkspaceRoot, "a.txt"); got != "retained work\n" {
		t.Fatalf("foreign store removed work: %q", got)
	}
	if got := read(t, filepath.Dir(inspectionPath(e.Cand)), inspectionFile); got != string(raw) {
		t.Fatal("foreign store changed marker")
	}
}

func TestRepeatedFinishKeepsInspectionNamespace(t *testing.T) {
	repo, managed, e := failedInspection(t)
	store := NewStore(managed, repo, opened(t, repo))
	if kept, err := store.Finish(t.Context(), e); !kept || err != nil {
		t.Fatalf("repeat Finish after Git returns: kept=%v err=%v", kept, err)
	}
	if _, err := os.Stat(entryPath(e.Cand)); !os.IsNotExist(err) {
		t.Fatalf("repeat Finish exposed unresolved record to legacy sweepers: %v", err)
	}
	store.SweepExpired(t.Context(), -time.Second)
	if _, err := NewDiscardTool(store).Execute(t.Context(), args(e.ID)); err != nil {
		t.Fatal(err)
	}
}

func TestCachedInspectionCannotDiscardUnsupportedReplacement(t *testing.T) {
	repo, managed, e := failedInspection(t)
	store := NewStore(managed, repo, opened(t, repo))
	if _, err := store.Paths(e.ID); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(inspectionPath(e.Cand))
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]json.RawMessage
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	record["version"] = json.RawMessage(`99`)
	raw, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inspectionPath(e.Cand), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Discard(t.Context(), e.ID); err == nil {
		t.Fatal("cached result bypassed an unsupported record replacement")
	}
	if got := read(t, e.Cand.WorkspaceRoot, "a.txt"); got != "retained work\n" {
		t.Fatalf("unsupported replacement lost work: %q", got)
	}
}

func TestExecuteReportsRunnerAndFirstFinishErrors(t *testing.T) {
	repo := gitRepo(t)
	managed := testenv.TempDir(t)
	store := NewStore(managed, repo, opened(t, repo))
	runnerErr := errors.New("injected runner failure")
	originalPath := os.Getenv("PATH")
	_, err := store.Execute(t.Context(), func(_ context.Context, run Run) (Outcome, error) {
		write(t, run.WorkspaceRoot, "a.txt", "work before failure\n")
		t.Setenv("PATH", testenv.TempDir(t))
		return Outcome{}, runnerErr
	}, repo, "write and fail", "")
	t.Setenv("PATH", originalPath)
	if !errors.Is(err, runnerErr) || err == nil || !strings.Contains(err.Error(), "inspect") {
		t.Fatalf("both failures must remain visible: %v", err)
	}
	id := idOf(t, err.Error())
	if got := NewStore(managed, repo, opened(t, repo)).Pending(); !slices.Contains(got, id) {
		t.Fatalf("reported ID %s not discoverable: %v", id, got)
	}
}
