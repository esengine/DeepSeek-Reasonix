package control

import (
	"context"
	"fmt"
	"testing"
	"time"

	"reasonix/internal/state/mutation"
)

func TestBranchMutationPreparationRetainsCallerDeadline(t *testing.T) {
	c, root := branchFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	checked := false
	coordinator, err := mutation.New(root, t.TempDir(), nil, mutation.WithContextAdmissionCheck(func(check context.Context, _ mutation.Intent) error {
		checked = true
		if actual, ok := check.Deadline(); !ok || actual != deadline {
			return fmt.Errorf("resource contention budget truncated Git preparation: %v", actual)
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	c.mutations = coordinator
	if _, ok, err := c.SwitchWorkspaceBranch(ctx, "next"); err != nil || !ok || !checked {
		t.Fatalf("checkout preparation failed without a competing writer: checked=%v ok=%v err=%v", checked, ok, err)
	}
}
