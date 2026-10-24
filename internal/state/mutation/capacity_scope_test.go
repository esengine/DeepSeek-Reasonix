package mutation

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestCapacityLeaseCannotAuthorizeFilesystemWrites(t *testing.T) {
	c := testCoordinator(t, t.TempDir(), t.TempDir())
	l, err := c.Acquire(t.Context(), Intent{Effect: CapacityOnly, Capacities: []Capacity{capacity("total")}})
	if err != nil {
		t.Fatal(err)
	}
	defer l.End(NotExecuted)
	ctx := l.Context(t.Context())
	if l.HoldingPaths() {
		t.Fatal("capacity-only admission impersonated a file mutation lease")
	}
	if _, err := c.Acquire(ctx, intent(filepath.Join(c.root, "a"))); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("file upgrade allowed: %v", err)
	}
	if _, err := c.Acquire(ctx, Intent{Effect: CapacityOnly, Capacities: []Capacity{capacity("other")}}); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("capacity upgrade allowed: %v", err)
	}
	child, err := c.Acquire(ctx, Intent{Effect: CapacityOnly, Capacities: []Capacity{capacity("total")}})
	if err != nil {
		t.Fatal(err)
	}
	child.End(NotExecuted)
	blocked, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	_, err = c.Acquire(blocked, Intent{Effect: CapacityOnly, Capacities: []Capacity{capacity("total")}})
	if !errors.Is(err, ErrCapacityWaitTimeout) || errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("incorrect capacity timeout: %v", err)
	}
}

func TestCapacityRejectsAmbiguousOrInvalidRequests(t *testing.T) {
	c := testCoordinator(t, t.TempDir(), t.TempDir())
	for _, request := range []Intent{
		{Effect: CapacityOnly},
		{Effect: CapacityOnly, Paths: []string{"a"}, Capacities: []Capacity{capacity("total")}},
		{Effect: ReadOnly, Capacities: []Capacity{capacity("total")}},
		{Effect: External, Capacities: []Capacity{capacity("total")}},
		{Effect: CapacityOnly, Capacities: []Capacity{capacity("total"), capacity("total")}},
		{Effect: CapacityOnly, Capacities: []Capacity{{ID: "total", Limit: 1, Units: 2}}},
		{Effect: CapacityOnly, Capacities: []Capacity{{ID: "total", Units: 1}}},
	} {
		if _, err := c.Acquire(t.Context(), request); !errors.Is(err, ErrInvalidIntent) {
			t.Fatalf("request %+v: %v", request, err)
		}
	}
}
