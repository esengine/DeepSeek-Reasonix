package mutation

import (
	"cmp"
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/base/filelock"
	"reasonix/internal/state/workspacelease"
)

// Capacity is a host-owned execution pool requested atomically with paths.
// A pool ID denotes one immutable limit; changing the limit needs a new ID.
type Capacity struct {
	ID    string `json:"id"`
	Limit uint32 `json:"limit"`
	Units uint32 `json:"units"`
}

func normalizedCapacities(in []Capacity) ([]Capacity, error) {
	out := slices.Clone(in)
	slices.SortFunc(out, func(a, b Capacity) int { return cmp.Compare(a.ID, b.ID) })
	for i, c := range out {
		if strings.TrimSpace(c.ID) == "" || len(c.ID) > 256 || c.Limit == 0 || c.Units == 0 || c.Units > c.Limit || (i > 0 && out[i-1].ID == c.ID) {
			return nil, ErrInvalidIntent
		}
	}
	return out, nil
}

func coversCapacities(held, requested []Capacity) bool {
	for _, want := range requested {
		if !slices.ContainsFunc(held, func(h Capacity) bool { return h.ID == want.ID && h.Limit == want.Limit && h.Units >= want.Units }) {
			return false
		}
	}
	return true
}

func validRecordResources(r record) bool {
	if r.Version == 1 {
		// The original path-only protocol permitted an omitted effect.
		knownEffect := r.Effect == "" || r.Effect == NamedPaths || r.Effect == Workspace || r.Effect == GitMetadata
		return len(r.Paths) > 0 && len(r.Capacities) == 0 && knownEffect
	}
	if r.Version != 2 || len(r.Capacities) == 0 || !filepath.IsAbs(r.Workspace) {
		return false
	}
	if _, err := normalizedCapacities(r.Capacities); err != nil {
		return false
	}
	if r.Effect == CapacityOnly {
		return len(r.Paths) == 0
	}
	return len(r.Paths) > 0 && (r.Effect == NamedPaths || r.Effect == Workspace || r.Effect == GitMetadata)
}

func acquisitionContext(ctx context.Context, effect Effect) (context.Context, context.CancelFunc) {
	if effect == CapacityOnly {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, DefaultWaitLimit)
}

func capacityLimits(records []record) (map[string]uint64, error) {
	limits := map[string]uint64{}
	for _, r := range records {
		for _, c := range r.Capacities {
			if existing, ok := limits[c.ID]; ok && existing != uint64(c.Limit) {
				return nil, ErrProtocol
			}
			limits[c.ID] = uint64(c.Limit)
		}
	}
	return limits, nil
}

func consumeCapacity(remaining map[string]uint64, capacities []Capacity) bool {
	for _, c := range capacities {
		if remaining[c.ID] < uint64(c.Units) {
			return false
		}
	}
	for _, c := range capacities {
		remaining[c.ID] -= uint64(c.Units)
	}
	return true
}

func (c *Coordinator) capacityBlocker(r record, records []record) (*record, error) {
	remaining, err := capacityLimits(records)
	if err != nil {
		return nil, err
	}
	if len(r.Capacities) == 0 {
		return nil, nil
	}
	var claims []record
	for _, other := range records {
		if other.ID != r.ID && other.Active {
			if !consumeCapacity(remaining, other.Capacities) {
				return nil, ErrProtocol
			}
			claims = append(claims, other)
		}
	}
	ordered := slices.Clone(records)
	slices.SortFunc(ordered, func(a, b record) int { return cmp.Compare(a.Order, b.Order) })
	for _, other := range ordered {
		if other.Active || other.Order >= r.Order || pathBlocker(other, records) != nil {
			continue
		}
		if len(other.Capacities) == 0 {
			continue
		}
		if len(other.Paths) != 0 {
			blocked, err := c.compatibilityBlocked(other)
			if blocked {
				continue
			}
			if err != nil {
				return nil, err
			}
		}
		// Reserve queue priority even when the older request cannot yet fit.
		// Otherwise smaller requests can indefinitely consume its next free unit.
		for _, capacity := range other.Capacities {
			remaining[capacity.ID] -= min(remaining[capacity.ID], uint64(capacity.Units))
		}
		claims = append(claims, other)
	}
	for _, requested := range r.Capacities {
		if remaining[requested.ID] >= uint64(requested.Units) {
			continue
		}
		for _, claim := range claims {
			if slices.ContainsFunc(claim.Capacities, func(c Capacity) bool { return c.ID == requested.ID }) {
				return &claim, nil
			}
		}
		return nil, ErrProtocol
	}
	return nil, nil
}

// A preparing transaction may already own its compatibility claim. The OS
// marker distinguishes that reservation from an unrelated legacy blocker.
func (c *Coordinator) compatibilityBlocked(r record) (bool, error) {
	release, err := filelock.TryAcquire(c.preparationPath(r.ID))
	if errors.Is(err, filelock.ErrHeld) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	release()
	_, err = workspacelease.ProbeClaimPaths(r.Workspace, c.compatDir, r.Paths)
	if errors.Is(err, workspacelease.ErrClaimBusy) {
		return true, nil
	}
	return false, err
}
