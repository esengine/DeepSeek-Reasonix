package rootenv

import (
	"context"

	"reasonix/internal/state/mutation"
)

func (s *Store) preparationCapacities(ctx context.Context) []mutation.Capacity {
	if capacities := mutation.PreparationCapacities(ctx); len(capacities) != 0 {
		return capacities
	}
	return s.preparation
}
