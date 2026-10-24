package isolation

import (
	"context"

	"reasonix/internal/state/mutation"
)

// WithPreparationCapacities carries descriptors, never a held execution lease.
// Snapshot and Git preparation request them atomically with their file resources.
func WithPreparationCapacities(ctx context.Context, capacities []mutation.Capacity) context.Context {
	return mutation.WithPreparationCapacities(ctx, capacities)
}

func preparationCapacities(ctx context.Context) []mutation.Capacity {
	return mutation.PreparationCapacities(ctx)
}

// AdmitRuntimePreparation bounds kernel construction after Git resources are
// released. Callers end this lease before running the prepared agent.
func (s *Store) AdmitRuntimePreparation(ctx context.Context, execution string) (func(), error) {
	capacities := preparationCapacities(ctx)
	if s.mutations == nil || len(capacities) == 0 {
		return func() {}, nil
	}
	lease, err := s.mutations.Acquire(ctx, mutation.Intent{Effect: mutation.CapacityOnly, Capacities: capacities,
		ExecutionID: execution, CallID: "isolation.build"})
	if err != nil {
		return nil, err
	}
	return func() { lease.End(mutation.NotExecuted) }, nil
}
