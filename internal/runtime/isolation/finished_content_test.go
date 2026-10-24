package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/state/mutation"
)

func TestFinishedContentBindsCandidateWithoutInventingVerification(t *testing.T) {
	root := gitRepo(t)
	coordinator, err := mutation.New(root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(t.TempDir(), root, opened(t, root)).WithMutationCoordinator(coordinator)
	entry, err := store.BeginOwned(t.Context(), root, "owner")
	if err != nil {
		t.Fatal(err)
	}
	write(t, entry.Cand.WorkspaceRoot, "a.txt", "candidate\n")
	_, err = store.Complete(t.Context(), entry, Outcome{Evidence: &CandidateEvidence{EnvironmentID: entry.ID,
		Workspace: entry.Cand.WorkspaceRoot, Verification: "passed",
		Receipts: []CandidateReceipt{{Tool: "bash", Success: true}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.operationSnapshot(t.Context(), entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Evidence == nil || got.Evidence.FinishedContent == nil || !got.Evidence.FinishedContent.Complete ||
		got.Evidence.FinishedContent.Digest == "" || got.Evidence.Verification != "unknown" {
		t.Fatalf("finished bytes imply verification: %+v", got.Evidence)
	}
	if got.Evidence.Workspace == root || got.Evidence.Workspace != filepath.Join(entry.Cand.WorktreeRoot, entry.Snap.Prefix) {
		t.Fatal("candidate content attributed to source workspace")
	}
	if _, err := discardReviewed(t, store, entry.ID); err != nil {
		t.Fatal(err)
	}
}

func TestFailedContinuationClearsPreviousCurrentChecks(t *testing.T) {
	store, entry := integrationFixture(t)
	resumed, err := store.Resume(t.Context(), entry.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(t.Context(), resumed, Outcome{Evidence: &CandidateEvidence{
		EnvironmentID: entry.ID, Workspace: resumed.Cand.WorkspaceRoot,
		Checks: []CheckEvidence{{ExecutionID: "previous-host-check", Success: true}},
	}}, nil); err != nil {
		t.Fatal(err)
	}
	resumed, err = store.Resume(t.Context(), entry.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.Complete(t.Context(), resumed, Outcome{Evidence: &CandidateEvidence{
		EnvironmentID: entry.ID, Workspace: resumed.Cand.WorkspaceRoot,
	}}, context.Canceled)
	current, err := store.operationSnapshot(t.Context(), entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != Cancelled || current.Evidence == nil || len(current.Evidence.Checks) != 0 {
		t.Fatalf("cancelled continuation kept prior checks as current evidence: %+v", current)
	}
}

func TestPreparationFailureAfterResumeHasNoPreviousCurrentEvidence(t *testing.T) {
	store, entry := integrationFixture(t)
	resumed, err := store.Resume(t.Context(), entry.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(t.Context(), resumed, Outcome{Evidence: &CandidateEvidence{
		EnvironmentID: entry.ID, Workspace: resumed.Cand.WorkspaceRoot,
		Checks: []CheckEvidence{{ExecutionID: "previous-host-check", Success: true}},
	}}, nil); err != nil {
		t.Fatal(err)
	}
	resumed, err = store.Resume(t.Context(), entry.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if current, err := store.operationManifest(entry.ID); err == nil || current != nil {
		t.Fatal("resumed execution did not retain run ownership")
	}
	_, _ = store.Complete(t.Context(), resumed, Outcome{}, errors.New("runtime preparation failed"))
	current, err := store.operationSnapshot(t.Context(), entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != Failed || current.Evidence == nil || len(current.Evidence.Checks) != 0 || current.Evidence.Verification != "unknown" {
		t.Fatalf("preparation failure inherited prior successful checks: %+v", current)
	}
}

func TestFinishedContentTransitionsClearStaleDigestAndReason(t *testing.T) {
	root := gitRepo(t)
	coordinator, err := mutation.New(root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(t.TempDir(), root, opened(t, root)).WithMutationCoordinator(coordinator)
	entry, err := store.BeginOwned(t.Context(), root, "owner")
	if err != nil {
		t.Fatal(err)
	}
	write(t, entry.Cand.WorkspaceRoot, "a.txt", "candidate\n")
	for step := range 3 {
		if step > 0 {
			entry, err = store.Resume(t.Context(), entry.ID, "owner")
			if err != nil {
				t.Fatal(err)
			}
		}
		ambiguous := filepath.Join(entry.Cand.WorktreeRoot, "nested")
		switch step {
		case 1:
			err = os.MkdirAll(filepath.Join(ambiguous, ".GIT"), 0700)
		case 2:
			err = os.RemoveAll(ambiguous)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Complete(t.Context(), entry, Outcome{}, nil); err != nil {
			t.Fatal(err)
		}
		got, err := store.operationSnapshot(t.Context(), entry.ID)
		if err != nil {
			t.Fatal(err)
		}
		content := got.Evidence.FinishedContent
		if step == 1 {
			if content.Complete || content.Digest != "" || content.Reason == "" {
				t.Fatalf("incomplete finish retained previous digest: %+v", content)
			}
		} else if !content.Complete || content.Digest == "" || content.Reason != "" {
			t.Fatalf("complete finish retained previous reason: %+v", content)
		}
	}
	if _, err := discardReviewed(t, store, entry.ID); err != nil {
		t.Fatal(err)
	}
}

func TestFinishedContentMergeRetainsUnknownFieldsButClearsKnownZeros(t *testing.T) {
	previous := json.RawMessage(`{"finished_content":{"scope":"workspace-content-v1","digest":"old","entries":1,"complete":true,"future":"keep"}}`)
	var before CandidateEvidence
	if err := json.Unmarshal(previous, &before); err != nil {
		t.Fatal(err)
	}
	after := before
	content := *before.FinishedContent
	content.Complete, content.Digest, content.Reason = false, "", "entry_limit"
	after.FinishedContent = &content
	next, err := json.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := mergeEvidenceFields(previous, next, &before, &after)
	if err != nil {
		t.Fatal(err)
	}
	var fields struct {
		Content map[string]any `json:"finished_content"`
	}
	if err := json.Unmarshal(merged, &fields); err != nil {
		t.Fatal(err)
	}
	if fields.Content["digest"] != "" || fields.Content["future"] != "keep" {
		t.Fatalf("merge lost snapshot identity: %s", merged)
	}
}
