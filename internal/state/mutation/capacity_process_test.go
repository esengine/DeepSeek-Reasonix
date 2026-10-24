package mutation

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestCapacityProcessDeathHelper(t *testing.T) {
	root := os.Getenv("REASONIX_CAPACITY_DEATH_ROOT")
	if root == "" {
		return
	}
	c := testCoordinator(t, root, os.Getenv("REASONIX_CAPACITY_DEATH_STATE"))
	if _, err := c.Acquire(t.Context(), Intent{Effect: CapacityOnly, Capacities: []Capacity{capacity("root-total")}}); err != nil {
		t.Fatal(err)
	}
	os.Exit(77)
}

func TestCapacityReclaimedAfterRealProcessExit(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCapacityProcessDeathHelper$")
	cmd.Env = append(os.Environ(), "REASONIX_CAPACITY_DEATH_ROOT="+root, "REASONIX_CAPACITY_DEATH_STATE="+state)
	out, err := cmd.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 77 {
		t.Fatalf("child: %v\n%s", err, out)
	}
	c := testCoordinator(t, root, state)
	l, err := c.Acquire(ctx, Intent{Effect: CapacityOnly, Capacities: []Capacity{capacity("root-total")}})
	if err != nil {
		t.Fatalf("dead process retained capacity: %v", err)
	}
	l.End(NotExecuted)
}
