package isolation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"reasonix/internal/base/filelock"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/mutation"
)

func integrationFixture(t *testing.T) (*Store, *Entry) {
	t.Helper()
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
	if _, err := store.Complete(t.Context(), entry, Outcome{}, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Each test retains the original until its assertions have completed.
		_, _ = discardReviewed(t, store, entry.ID)
	})
	return store, entry
}

func TestIntegrationCandidateCombinesCurrentSourceWithImmutableResult(t *testing.T) {
	store, entry := integrationFixture(t)
	before, err := os.ReadFile(entryRecordPath(entry))
	if err != nil {
		t.Fatal(err)
	}
	write(t, store.workspace, "parent.txt", "parent continued\n")
	integration, err := store.PrepareIntegration(t.Context(), entry.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer integration.Close()
	if got := read(t, integration.WorkspaceRoot(), "a.txt"); got != "candidate\n" {
		t.Fatalf("original delta missing: %q", got)
	}
	if got := read(t, integration.WorkspaceRoot(), "parent.txt"); got != "parent continued\n" {
		t.Fatalf("current source omitted: %q", got)
	}
	if got := read(t, store.workspace, "a.txt"); got != "one\n" {
		t.Fatalf("preparation wrote source: %q", got)
	}
	after, err := os.ReadFile(entryRecordPath(entry))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("preparation rewrote original result: %v", err)
	}
	status := integration.Status()
	if status.ID == entry.ID || status.ResultGeneration != entry.RunGeneration || status.Verification != "unknown" || !status.SourceContent.Complete || !status.SourceGit.Complete {
		t.Fatalf("integration omitted provenance or invented validity: %+v", status)
	}
	if err := integration.BeginChecks(t.Context(), []CheckContract{{Identity: "host-selected-check", CommandDigest: "command"}}); err != nil {
		t.Fatal(err)
	}
	// A candidate's long check owns its execution identity, not the source.
	lease, err := store.mutations.Acquire(t.Context(), mutation.Intent{Effect: mutation.NamedPaths,
		Paths: []string{store.workspace}, CallID: "parent.continues"})
	if err != nil {
		t.Fatalf("checking candidate retained source resources: %v", err)
	}
	lease.End(mutation.NotExecuted)
	if _, err := store.OpenIntegration(t.Context(), status.ID, "owner"); !errors.Is(err, filelock.ErrHeld) {
		t.Fatalf("second owner entered running attempt: %v", err)
	}
	integration.Close()
	recovered, err := store.OpenIntegration(t.Context(), status.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if got := recovered.Status(); got.State != Interrupted || got.Verification != "unknown" || !strings.Contains(got.Reason, "interrupted") {
		t.Fatalf("restart pretended checks kept running: %+v", got)
	}
}

func TestIntegrationRefusesSameFileConflictAndOwnerMismatch(t *testing.T) {
	store, entry := integrationFixture(t)
	if _, err := store.PrepareIntegration(t.Context(), entry.ID, "other-owner"); err == nil {
		t.Fatal("integration bypassed original owner")
	}
	write(t, store.workspace, "a.txt", "parent edit\n")
	_, err := store.PrepareIntegration(t.Context(), entry.ID, "owner")
	if err == nil {
		t.Fatal("same-file source change silently merged")
	}
	if got := read(t, store.workspace, "a.txt"); got != "parent edit\n" {
		t.Fatalf("conflicted preparation overwrote source: %q", got)
	}
	if got := read(t, entry.Cand.WorkspaceRoot, "a.txt"); got != "candidate\n" {
		t.Fatalf("conflict destroyed original result: %q", got)
	}
}

func TestIntegrationMissingOrStaleEvidenceCannotPublish(t *testing.T) {
	store, entry := integrationFixture(t)
	i, err := store.PrepareIntegration(t.Context(), entry.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer i.Close()
	authorizerCalled := false
	err = i.Commit(t.Context(), func(_ context.Context, _ []string) error { authorizerCalled = true; return nil })
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != CodeUnverified || authorizerCalled {
		t.Fatalf("unverified result reached source publication: %v", err)
	}
	if err := i.BeginChecks(t.Context(), []CheckContract{{Identity: "check", CommandDigest: "command"}}); err != nil {
		t.Fatal(err)
	}
	if err := i.FinishChecks(t.Context(), []CheckEvidence{{ExecutionID: "host", Contract: "check", Success: true}}); err != nil {
		t.Fatal(err)
	}
	if i.Status().Verification != "unknown" {
		t.Fatal("command success without launch inputs became valid evidence")
	}
	if err := i.BeginChecks(t.Context(), []CheckContract{{Identity: "check", CommandDigest: "command"}}); err != nil {
		t.Fatal(err)
	}
	// Two booleans and matching file hashes are not actual launch evidence.
	input := evidence.CheckInputs{Completed: true, Stable: true, Before: i.Status().Content, After: i.Status().Content}
	if err := i.FinishChecks(t.Context(), []CheckEvidence{{ExecutionID: "host", Contract: "check", CommandDigest: "command", Success: true, Inputs: []evidence.CheckInputs{input}}}); err != nil {
		t.Fatal(err)
	}
	if i.Status().Verification != "unknown" {
		t.Fatal("missing executable, command, environment and directory identity became valid")
	}
	write(t, i.WorkspaceRoot(), "a.txt", "changed during checks\n")
	if err := i.BeginChecks(t.Context(), []CheckContract{{Identity: "check", CommandDigest: "command"}}); err == nil || i.Status().Verification != "invalid" {
		t.Fatalf("candidate drift reused old check inputs: %v, %+v", err, i.Status())
	}
	if got := read(t, store.workspace, "a.txt"); got != "one\n" {
		t.Fatalf("invalid evidence wrote source: %q", got)
	}
}

func TestIntegrationUnknownFieldsAndFutureVersionArePreserved(t *testing.T) {
	store, entry := integrationFixture(t)
	i, err := store.PrepareIntegration(t.Context(), entry.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	id := i.ID()
	path, _ := store.integrationPath(id)
	i.Close()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["future_extension"] = json.RawMessage(`{"retain":true}`)
	raw, _ = json.Marshal(fields)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	i, err = store.OpenIntegration(t.Context(), id, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := i.BeginChecks(t.Context(), []CheckContract{{Identity: "check", CommandDigest: "command"}}); err != nil {
		t.Fatal(err)
	}
	i.Close()
	raw, err = os.ReadFile(path)
	if err != nil || !bytes.Contains(raw, []byte(`"future_extension":{"retain":true}`)) {
		t.Fatalf("unknown fields erased: %s, %v", raw, err)
	}
	_ = json.Unmarshal(raw, &fields)
	fields["version"] = json.RawMessage(`999`)
	raw, _ = json.Marshal(fields)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OpenIntegration(t.Context(), id, "owner"); err == nil {
		t.Fatal("future version accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatalf("unsupported record rewritten: %v", err)
	}
}

func TestIntegrationDiscardRetainsOriginalAndItsOwnTerminalReceipt(t *testing.T) {
	store, original := integrationFixture(t)
	i, err := store.PrepareIntegration(t.Context(), original.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	id := i.ID()
	if err := i.Discard(t.Context()); err != nil {
		t.Fatal(err)
	}
	i.Close()
	i, err = store.OpenIntegration(t.Context(), id, "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer i.Close()
	if err := i.Discard(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := i.Status(); got.State != Discarded || !got.CleanupComplete {
		t.Fatalf("discard receipt lost: %+v", got)
	}
	if got := read(t, original.Cand.WorkspaceRoot, "a.txt"); got != "candidate\n" {
		t.Fatalf("discarding the integration attempt destroyed original: %q", got)
	}
	if attempts := store.IntegrationAttempts(); len(attempts) != 1 || attempts[0].Attempt == nil || attempts[0].Attempt.State != Discarded {
		t.Fatalf("terminal attempt disappeared from durable inventory: %+v", attempts)
	}
}
