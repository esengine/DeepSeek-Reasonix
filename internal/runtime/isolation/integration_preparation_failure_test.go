package isolation

import (
	"context"
	"errors"
	"testing"

	"reasonix/internal/state/mutation"
)

func TestFailedIntegrationPreparationReleasesOwnerAndCanBeDiscarded(t *testing.T) {
	store, original := integrationFixture(t)
	coordinator, err := mutation.New(store.workspace, t.TempDir(), nil, mutation.WithContextAdmissionCheck(func(_ context.Context, intent mutation.Intent) error {
		if intent.CallID == "isolation.integration.prepare" {
			panic("fixture interrupted preparation")
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	store.WithMutationCoordinator(coordinator)
	if prepared, err := store.PrepareIntegration(t.Context(), original.ID, "owner"); prepared != nil || !errors.Is(err, mutation.ErrCallback) {
		t.Fatalf("preparation failure transferred an incomplete owner: %v %v", prepared, err)
	}
	attempts := store.IntegrationAttempts()
	if len(attempts) != 1 || attempts[0].Active || attempts[0].Attempt == nil || attempts[0].Attempt.State != RecoveryRequired || attempts[0].Attempt.Tree != "" {
		t.Fatalf("failed preparation leaked ownership or fabricated a sealed tree: %+v", attempts)
	}
	i, err := store.OpenIntegration(t.Context(), attempts[0].ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := i.Discard(t.Context()); err != nil {
		i.Close()
		t.Fatal(err)
	}
	i.Close()
	i, err = store.OpenIntegration(t.Context(), attempts[0].ID, "owner")
	if err != nil {
		t.Fatalf("discard made an unsealed attempt unreadable: %v", err)
	}
	defer i.Close()
	if err := i.Discard(t.Context()); err != nil {
		t.Fatal(err)
	}
	if status := i.Status(); status.State != Discarded || !status.CleanupComplete || status.Tree != "" {
		t.Fatalf("unsealed discard terminal receipt: %+v", status)
	}
	if got := read(t, original.Cand.WorkspaceRoot, "a.txt"); got != "candidate\n" {
		t.Fatalf("attempt cleanup destroyed original result: %q", got)
	}
}
