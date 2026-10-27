package mutation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/state/workspacelease"
)

type record struct {
	Version     int        `json:"version"`
	ID          string     `json:"id"`
	SessionID   string     `json:"sessionId"`
	ExecutionID string     `json:"executionId"`
	CallID      string     `json:"callId"`
	Paths       []string   `json:"paths"`
	Order       uint64     `json:"order"`
	Active      bool       `json:"active"`
	StartedAt   time.Time  `json:"startedAt"`
	Effect      Effect     `json:"effect,omitempty"`
	Capacities  []Capacity `json:"capacities,omitempty"`
	Workspace   string     `json:"workspace,omitempty"`
	waitKind    Effect
}

type blocker struct {
	record
	kind Effect
}

func (c *Coordinator) records() ([]record, error) {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return nil, err
	}
	var records []record
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		data, err := os.ReadFile(c.recordPath(id))
		if err != nil {
			return nil, err
		}
		var r record
		if err := decodeCoordinationRecord(data, &r); err != nil {
			return nil, fmt.Errorf("%w: record %q: %w", ErrProtocol, entry.Name(), err)
		}
		if r.ID != id || r.Order == 0 || !validRecordResources(r) {
			return nil, fmt.Errorf("%w: record %q has unsupported identity or resources", ErrProtocol, entry.Name())
		}
		for _, path := range r.Paths {
			if !filepath.IsAbs(path) {
				return nil, ErrProtocol
			}
		}
		release, err := filelock.TryAcquire(c.livePath(id))
		if err == nil {
			removeErr := os.Remove(c.recordPath(id))
			_ = os.Remove(c.preparationPath(id))
			release()
			if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return nil, removeErr
			}
			continue
		}
		if !errors.Is(err, filelock.ErrHeld) {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

func (c *Coordinator) save(r record) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(c.recordPath(r.ID), data, 0600)
}

func (c *Coordinator) enqueue(ctx context.Context, r *record) error {
	unlock, err := filelock.Acquire(ctx, c.guardPath())
	if err != nil {
		return err
	}
	defer unlock()
	records, err := c.records()
	if err != nil {
		return err
	}
	if _, err := capacityLimits(append(records, *r)); err != nil {
		return err
	}
	r.Order = 1
	for _, other := range records {
		if other.Order == math.MaxUint64 {
			return ErrProtocol
		}
		r.Order = max(r.Order, other.Order+1)
	}
	return c.save(*r)
}

func (c *Coordinator) tryGrant(ctx context.Context, lease *Lease) (*blocker, error) {
	r := &lease.record
	held, releasePreparation, err := c.grantBlocker(ctx, *r)
	if held != nil || err != nil {
		return held, err
	}
	defer releasePreparation()
	var releaseCompat func()
	defer func() {
		if releaseCompat != nil {
			releaseCompat()
		}
	}()
	if len(r.Paths) != 0 {
		release, conflict, err := c.compatClaim(c.root, c.compatDir, r.SessionID, r.Paths)
		if errors.Is(err, workspacelease.ErrClaimBusy) {
			held := &record{Paths: r.Paths}
			if conflict != nil {
				held.SessionID, held.Paths = conflict.SessionID, conflict.Paths
			}
			return &blocker{record: *held, kind: NamedPaths}, nil
		}
		if err != nil {
			return nil, err
		}
		releaseCompat = release
		if releaseCompat == nil {
			return nil, ErrProtocol
		}
	}
	unlock, err := filelock.Acquire(ctx, c.guardPath())
	if err != nil {
		return nil, err
	}
	defer unlock()
	records, err := c.records()
	if err != nil {
		return nil, err
	}
	if held, err := c.recordsBlocker(*r, records); held != nil || err != nil {
		return held, err
	}
	r.Active = true
	if err := c.save(*r); err != nil {
		return nil, err
	}
	lease.releaseCompat, releaseCompat = releaseCompat, nil
	return nil, nil
}

// Queue order reserves eligibility while compatibility discovery runs outside
// the registry lock. The final locked recheck precedes active publication.
func (c *Coordinator) grantBlocker(ctx context.Context, r record) (*blocker, func(), error) {
	unlock, err := filelock.Acquire(ctx, c.guardPath())
	if err != nil {
		return nil, nil, err
	}
	defer unlock()
	records, err := c.records()
	if err != nil {
		return nil, nil, err
	}
	if held, err := c.recordsBlocker(r, records); held != nil || err != nil {
		return held, nil, err
	}
	release, err := filelock.TryAcquire(c.preparationPath(r.ID))
	return nil, release, err
}

func (c *Coordinator) recordsBlocker(r record, records []record) (*blocker, error) {
	if held := pathBlocker(r, records); held != nil {
		return &blocker{record: *held, kind: NamedPaths}, nil
	}
	held, err := c.capacityBlocker(r, records)
	if err != nil || held == nil {
		return nil, err
	}
	return &blocker{record: *held, kind: CapacityOnly}, nil
}

func pathBlocker(r record, records []record) *record {
	for _, other := range records {
		if other.ID != r.ID && (other.Active || other.Order < r.Order) && overlaps(r.Paths, other.Paths) {
			return &other
		}
	}
	return nil
}
