package schedule

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"
)

// ReapGrace is how long a claimed run may sit without a live lease before it
// is presumed dead.
const ReapGrace = 2 * time.Minute

type SkipReason string

const (
	SkipMissed       SkipReason = "missed"
	SkipLeaseHeld    SkipReason = "lease_held"
	SkipBudgetWindow SkipReason = "budget_window"
	SkipConcurrency  SkipReason = "concurrency"
)

// Claim records the slot as started, before anything is spawned. The trigger
// id is unique, so of any number of concurrent or restarted claimers exactly
// one gets nil and the rest get ErrAlreadyClaimed. The full per-run cap is
// reserved as the run's charge until Finish or Reap replaces it.
func (s *Store) Claim(ctx context.Context, p Policy, scheduleID string, slot time.Time) (Run, error) {
	var run Run
	var refused error
	err := s.update(ctx, func(m *Manifest) error {
		sc := m.schedule(scheduleID)
		if sc == nil {
			return ErrNotFound
		}
		slot = slot.UTC().Truncate(time.Second)
		id := TriggerID(scheduleID, slot)
		if m.run(id) != nil {
			return ErrAlreadyClaimed
		}
		if !sc.Trigger.IsSlot(slot) {
			return ErrNotDue
		}
		now := s.clock()
		if slot.After(now) {
			return fmt.Errorf("%w: slot is in the future", ErrNotDue)
		}
		if latest, _ := sc.Trigger.LatestSlot(now); !slot.Equal(latest) {
			return ErrSlotStale
		}
		if sc.Confirmed.By != ConfirmedByHuman || sc.Confirmed.Digest != Digest(*sc) {
			return ErrDigestMismatch
		}
		if err := Admit(p, *m, *sc, now); err != nil {
			if errors.Is(err, ErrExpired) && sc.Status == StatusActive {
				sc.Status, sc.UpdatedAt = StatusExpired, now
				refused = err
				return nil
			}
			return err
		}
		run = Run{TriggerID: id, ScheduleID: scheduleID, SlotAt: slot, ClaimedAt: now,
			State: RunClaimed, Charged: sc.Budget.PerRunTokens, PerRunCap: sc.Budget.PerRunTokens}
		m.Runs = append(m.Runs, run)
		if sc.Trigger.Kind == TriggerAt {
			sc.Status, sc.UpdatedAt = StatusExpired, now
		}
		prune(m, now)
		return nil
	})
	if err != nil {
		return Run{}, err
	}
	return run, refused
}

// RecordSkip stores a slot that was deliberately not run, so the same slot is
// not reconsidered by another ticker.
func (s *Store) RecordSkip(ctx context.Context, scheduleID string, slot time.Time, why SkipReason) error {
	return s.update(ctx, func(m *Manifest) error {
		if m.schedule(scheduleID) == nil {
			return ErrNotFound
		}
		slot = slot.UTC().Truncate(time.Second)
		id := TriggerID(scheduleID, slot)
		if m.run(id) != nil {
			return ErrAlreadyClaimed
		}
		now := s.clock()
		m.Runs = append(m.Runs, Run{TriggerID: id, ScheduleID: scheduleID, SlotAt: slot,
			ClaimedAt: now, EndedAt: now, State: RunSkipped, SkipReason: string(why)})
		prune(m, now)
		return nil
	})
}

// MarkRunning notes that the supervisor spawned the run and returns the start
// token. Only the token's hash is stored: whoever holds the token, which is the
// child the supervisor spawned, is the one executor Start will admit.
func (s *Store) MarkRunning(ctx context.Context, triggerID string) (string, error) {
	token := newToken()
	err := s.update(ctx, func(m *Manifest) error {
		r := m.run(triggerID)
		if r == nil {
			return ErrRunNotFound
		}
		if r.State != RunClaimed {
			return ErrRunSettled
		}
		r.State, r.StartHash = RunRunning, tokenHash(token)
		return nil
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

// Start is the one transition that lets a run's work begin: the run must be
// running, the token must be the one MarkRunning issued, and nothing may have
// started it before. It commits with the manifest, so a crash or a power loss
// cannot leave a run that could start twice.
func (s *Store) Start(ctx context.Context, triggerID, token string) error {
	return s.update(ctx, func(m *Manifest) error {
		r := m.run(triggerID)
		if r == nil {
			return ErrRunNotFound
		}
		if !r.inFlight() || r.State != RunRunning {
			return ErrRunSettled
		}
		if r.Started {
			return ErrRunStarted
		}
		if r.StartHash == "" || subtle.ConstantTimeCompare([]byte(r.StartHash), []byte(tokenHash(token))) != 1 {
			return ErrRunToken
		}
		r.Started = true
		return nil
	})
}

func newToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("schedule: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Finish settles a claimed or running run: it replaces the reservation with
// ChargeFor, moves the schedule's lifetime spend and failure streak, and opens
// the circuit when a breaker trips.
func (s *Store) Finish(ctx context.Context, p Policy, triggerID string, o Outcome) error {
	switch o.State {
	case RunSucceeded, RunFailed, RunBlocked, RunBudgetStopped, RunInterrupted:
	default:
		return fmt.Errorf("%w: %q is not a settling state", ErrInvalid, o.State)
	}
	return s.update(ctx, func(m *Manifest) error { return s.settle(m, p, triggerID, o) })
}

func (s *Store) settle(m *Manifest, p Policy, triggerID string, o Outcome) error {
	r := m.run(triggerID)
	if r == nil {
		return ErrRunNotFound
	}
	if !r.inFlight() {
		return ErrRunSettled
	}
	now := s.clock()
	r.State, r.EndedAt, r.Observed = o.State, now, max(o.Observed, 0)
	r.Charged = ChargeFor(r.PerRunCap, o)
	sc := m.schedule(r.ScheduleID)
	if sc == nil {
		return nil
	}
	sc.SpentLifetimeTokens += r.Charged
	sc.UpdatedAt = now
	switch o.State {
	case RunSucceeded:
		sc.ConsecutiveFailures = 0
	case RunFailed, RunInterrupted, RunBudgetStopped:
		sc.ConsecutiveFailures++
	}
	if sc.Status != StatusActive {
		return nil
	}
	switch {
	case sc.SpentLifetimeTokens+sc.Budget.PerRunTokens > sc.Budget.LifetimeTokens:
		sc.Status, sc.PausedReason = StatusCircuitOpen, PauseLifetime
	case int64(sc.ConsecutiveFailures) >= p.MaxConsecutiveFails:
		sc.Status, sc.PausedReason = StatusCircuitOpen, PauseFailures
	}
	return nil
}

// ShouldReap is the crash-recovery decision: an in-flight run whose session
// lease is not held and which is older than the grace is presumed dead. A held
// lease means the run is alive, whatever its age. A negative age (the clock
// moved back) leaves the lease as the only evidence, so the run is not pinned
// in flight forever.
func ShouldReap(r Run, leaseHeld bool, now time.Time) bool {
	age := now.Sub(r.ClaimedAt)
	return r.inFlight() && !leaseHeld && (age >= ReapGrace || age < 0)
}

// Reap settles every presumed-dead run as interrupted at the full per-run
// cap. It never re-runs anything. leaseHeld asks the OS-level session lease; it
// is called outside the transaction lock, so it may take any lock, and the
// commit skips any run whose state changed since the snapshot. It returns the reaped ids.
func (s *Store) Reap(ctx context.Context, p Policy, leaseHeld func(Run) bool) ([]string, error) {
	m, _, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	now := s.clock()
	dead := map[string]RunState{}
	for _, r := range m.Runs {
		if ShouldReap(r, leaseHeld(r), now) {
			dead[r.TriggerID] = r.State
		}
	}
	if len(dead) == 0 {
		return nil, nil
	}
	var reaped []string
	err = s.update(ctx, func(m *Manifest) error {
		for id, seen := range dead {
			if r := m.run(id); r == nil || r.State != seen {
				continue
			}
			if err := s.settle(m, p, id, Outcome{State: RunInterrupted}); err != nil {
				return err
			}
			reaped = append(reaped, id)
		}
		return nil
	})
	return reaped, err
}

// MaxSkips bounds skipped-slot records, which count in no budget window and so
// must never crowd out a charged record.
const MaxSkips = 200

// prune drops settled records past retention, the oldest skips beyond MaxSkips,
// and, over MaxRuns, the oldest settled records already outside the 7-day
// window. A charged record inside the window is never dropped.
func prune(m *Manifest, now time.Time) {
	m.Runs = slices.DeleteFunc(m.Runs, func(r Run) bool { return !r.inFlight() && r.ClaimedAt.Before(now.Add(-RunRetention)) })
	skips := 0
	for _, r := range m.Runs {
		if r.State == RunSkipped {
			skips++
		}
	}
	m.Runs = slices.DeleteFunc(m.Runs, func(r Run) bool {
		if r.State != RunSkipped || skips <= MaxSkips {
			return false
		}
		skips--
		return true
	})
	windowStart := now.Add(-Week)
	for len(m.Runs) > MaxRuns {
		i := slices.IndexFunc(m.Runs, func(r Run) bool { return !r.inFlight() && r.ClaimedAt.Before(windowStart) })
		if i < 0 {
			return
		}
		m.Runs = slices.Delete(m.Runs, i, i+1)
	}
}
