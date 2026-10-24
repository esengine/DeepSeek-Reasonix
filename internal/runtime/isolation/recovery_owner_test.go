package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/tool"
)

func interruptedPublication(t *testing.T) (*Store, *Entry, string) {
	t.Helper()
	store, original, attempt := checkedIntegration(t)
	attempt.attempt.State = Applying
	if err := attempt.save(); err != nil {
		t.Fatal(err)
	}
	original.State = Applying
	if err := persist(original); err != nil {
		t.Fatal(err)
	}
	id := attempt.ID()
	attempt.Close()
	return store, original, id
}

func TestRecoveryOwnerPreservesLegacyLocationAndUnknownContractFields(t *testing.T) {
	store, original, id := interruptedPublication(t)
	legacyPath := filepath.Join(filepath.Dir(original.Cand.WorktreeRoot), legacyEntryFile)
	if err := os.Rename(entryPath(original.Cand), legacyPath); err != nil {
		t.Fatal(err)
	}
	path, err := store.integrationPath(id)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["check_contracts"].([]any)[0].(map[string]any)["future_contract"] = "retained"
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	preview, err := store.PreviewRecovery(t.Context())
	if err != nil || preview == nil {
		t.Fatalf("legacy-location recovery unavailable: %+v %v", preview, err)
	}
	if err := store.RecoverExpected(t.Context(), preview.Revision, func(context.Context, []string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["check_contracts"].([]any)[0].(map[string]any)["future_contract"] != "retained" {
		t.Fatal("recovery dropped unknown frozen contract fields")
	}
	raw, err = os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &fields); err != nil || fields["state"] != string(Conflict) {
		t.Fatalf("legacy-location result was replaced instead of recovered: %s %v", raw, err)
	}
}

func TestRecoveryOwnerReconcilesApplyingWithoutJournal(t *testing.T) {
	for _, publication := range []string{"before-journal", "after-publication", "external-edit", "after-original-receipt"} {
		t.Run(publication, func(t *testing.T) {
			store, original, id := interruptedPublication(t)
			if publication == "after-original-receipt" {
				original.State = Conflict
				if err := persist(original); err != nil {
					t.Fatal(err)
				}
			}
			want := "one\n"
			switch publication {
			case "after-publication":
				want = "candidate\n"
			case "external-edit":
				want = "user changes\n"
			}
			write(t, store.workspace, "a.txt", want)
			preview, err := store.PreviewRecovery(t.Context())
			if err != nil || preview == nil || preview.Revision == "" || len(preview.Paths) == 0 {
				t.Fatalf("missing lifecycle recovery preview: %+v %v", preview, err)
			}
			if err := store.RecoverExpected(t.Context(), preview.Revision, func(context.Context, []string) error { return nil }); err != nil {
				t.Fatal(err)
			}
			if got := read(t, store.workspace, "a.txt"); got != want {
				t.Fatalf("no-journal recovery rewrote source: %q", got)
			}
			current, err := store.operationSnapshot(t.Context(), original.ID)
			if err != nil || current.State != Conflict {
				t.Fatalf("original remains unusable: %+v %v", current, err)
			}
			recovered, _, err := store.readIntegration(id)
			if err != nil || recovered.State != Conflict || recovered.Verification != "unknown" {
				t.Fatalf("attempt retains publication/evidence claim: %+v %v", recovered, err)
			}
			if _, err := store.PreviewDiscard(t.Context(), original.ID); err != nil {
				t.Fatalf("recovered original cannot be managed: %v", err)
			}
			if _, err := store.PreviewDiscard(t.Context(), id); err != nil {
				t.Fatalf("recovered attempt cannot be managed: %v", err)
			}
			if next, err := store.PreviewRecovery(t.Context()); err != nil || next != nil {
				t.Fatalf("recovery is not terminal: %+v %v", next, err)
			}
		})
	}
}

func TestRecoveryOwnerDoesNotAlterRecordsOnDeniedOrStaleGeneration(t *testing.T) {
	store, original, id := interruptedPublication(t)
	denied := errors.New("original source grant was revoked")
	preview, err := store.PreviewRecovery(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverExpected(t.Context(), preview.Revision, func(context.Context, []string) error { return denied }); !errors.Is(err, denied) {
		t.Fatalf("recovery ignored revocation: %v", err)
	}
	original.RunGeneration = "another-generation"
	if err := persist(original); err != nil {
		t.Fatal(err)
	}
	preview, err = store.PreviewRecovery(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	err = store.RecoverExpected(t.Context(), preview.Revision, func(context.Context, []string) error { return nil })
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != CodeStaleExecution {
		t.Fatalf("recovery accepted a newer execution: %v", err)
	}
	current, err := store.operationSnapshot(t.Context(), original.ID)
	if err != nil || current.State != Applying || current.RunGeneration != "another-generation" {
		t.Fatalf("recovery changed the newer original: %+v %v", current, err)
	}
	attempt, _, err := store.readIntegration(id)
	if err != nil || attempt.State != Applying {
		t.Fatalf("recovery rewrote stale attempt identity: %+v %v", attempt, err)
	}
}

func TestRecoveryOwnerRejectsChangedRecordAndActiveAttempt(t *testing.T) {
	store, _, id := interruptedPublication(t)
	preview, err := store.PreviewRecovery(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.OpenIntegration(t.Context(), id, "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer active.Close()
	err = store.RecoverExpected(t.Context(), preview.Revision, func(context.Context, []string) error { return nil })
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != CodeBusy {
		t.Fatalf("recovery waited on an active attempt while holding source: %v", err)
	}
	active.attempt.Reason = "another owner changed this record"
	if err := active.save(); err != nil {
		t.Fatal(err)
	}
	active.Close()
	err = store.RecoverExpected(t.Context(), preview.Revision, func(context.Context, []string) error { return nil })
	if !errors.As(err, &refusal) || refusal.Code != CodeStaleExecution {
		t.Fatalf("old preview accepted changed lifecycle: %v", err)
	}
}
