package mutation

import (
	"context"
	"slices"
)

type preparationCapacityKey struct{}

// WithPreparationCapacities carries descriptors, never a held execution lease.
// Each preparation acquires them atomically with its actual file resources.
func WithPreparationCapacities(ctx context.Context, capacities []Capacity) context.Context {
	return context.WithValue(ctx, preparationCapacityKey{}, slices.Clone(capacities))
}

func PreparationCapacities(ctx context.Context) []Capacity {
	capacities, _ := ctx.Value(preparationCapacityKey{}).([]Capacity)
	return slices.Clone(capacities)
}
