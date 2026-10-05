package schedule

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// CreateRequest is a person's confirmed intent. ToolCeiling and
// AllowReadOnlyBash are the host's limits for the observe posture, supplied by
// the caller because this package knows no tool registry.
type CreateRequest struct {
	Label             string
	Trigger           Trigger
	Target            Target
	Prompt            string
	Model             Model
	Grant             Grant
	Budget            *Budget
	ConfirmedBy       string
	ToolCeiling       []string
	AllowReadOnlyBash bool
}

// Create validates the request against the policy and stores a new active
// schedule. Only a human frontend confirmation is accepted; the model field is
// stored as given, so the caller pins it from user-level configuration.
func (s *Store) Create(ctx context.Context, p Policy, req CreateRequest) (Schedule, error) {
	var out Schedule
	err := s.update(ctx, func(m *Manifest) error {
		now := s.clock()
		if req.ConfirmedBy != ConfirmedByHuman {
			return ErrNotHuman
		}
		if int64(len(m.Schedules)) >= p.MaxSchedules {
			return fmt.Errorf("%w: %d", ErrTooMany, p.MaxSchedules)
		}
		sc, err := s.build(p, req, now)
		if err != nil {
			return err
		}
		m.Schedules = append(m.Schedules, sc)
		out = sc
		return nil
	})
	return out, err
}

func (s *Store) build(p Policy, req CreateRequest, now time.Time) (Schedule, error) {
	tr := req.Trigger
	tr.At, tr.Anchor = tr.At.UTC().Truncate(time.Second), tr.Anchor.UTC().Truncate(time.Second)
	if err := tr.validate(p, now); err != nil {
		return Schedule{}, err
	}
	if tr.Kind == TriggerEvery {
		tr.Anchor = now
	} else {
		tr.Anchor = time.Time{}
	}
	budget := p.DefaultBudget()
	if req.Budget != nil {
		budget = *req.Budget
	}
	if err := p.fits(budget); err != nil {
		return Schedule{}, err
	}
	if err := checkContent(req); err != nil {
		return Schedule{}, err
	}
	grant, err := checkGrant(req)
	if err != nil {
		return Schedule{}, err
	}
	sc := Schedule{
		ID: newID(), Label: req.Label, Trigger: tr, Target: req.Target, Prompt: req.Prompt,
		Model: req.Model, Grant: grant, Budget: budget, Status: StatusActive,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(p.Expiry()),
	}
	sc.Confirmed = Confirmation{At: now, By: ConfirmedByHuman, Digest: Digest(sc)}
	return sc, nil
}

func checkContent(req CreateRequest) error {
	switch {
	case strings.TrimSpace(req.Prompt) == "" || len(req.Prompt) > MaxPromptBytes:
		return fmt.Errorf("%w: prompt must be 1..%d bytes", ErrInvalid, MaxPromptBytes)
	case utf8.RuneCountInString(req.Label) > MaxLabelRunes:
		return fmt.Errorf("%w: label exceeds %d runes", ErrInvalid, MaxLabelRunes)
	case req.Target.Workspace == "" || !filepath.IsAbs(req.Target.Workspace):
		return fmt.Errorf("%w: target workspace must be an absolute path", ErrInvalid)
	case req.Model.Provider == "" || req.Model.Model == "":
		return fmt.Errorf("%w: model must be pinned", ErrInvalid)
	}
	return nil
}

func checkGrant(req CreateRequest) (Grant, error) {
	g := req.Grant
	g.Tools = slices.Clone(g.Tools)
	slices.Sort(g.Tools)
	g.Tools = slices.Compact(g.Tools)
	for _, t := range g.Tools {
		if !slices.Contains(req.ToolCeiling, t) {
			return Grant{}, fmt.Errorf("%w: %q", ErrGrantExceedsCeil, t)
		}
	}
	if g.ReadOnlyBash && !req.AllowReadOnlyBash {
		return Grant{}, fmt.Errorf("%w: read-only bash is not available on this host", ErrGrantExceedsCeil)
	}
	if len(g.ReadRoots) == 0 {
		g.ReadRoots = []string{req.Target.Workspace}
	}
	for _, r := range g.ReadRoots {
		if !filepath.IsAbs(r) {
			return Grant{}, fmt.Errorf("%w: read root must be an absolute path", ErrInvalid)
		}
	}
	return g, nil
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("schedule: crypto/rand unavailable: " + err.Error())
	}
	return "sch_" + hex.EncodeToString(b[:])
}

// Pause stops a schedule from being claimed until a person resumes it.
func (s *Store) Pause(ctx context.Context, id string) error {
	return s.setStatus(ctx, id, func(sc *Schedule) error {
		if sc.Status != StatusActive {
			return fmt.Errorf("%w: %s", ErrNotActive, sc.Status)
		}
		sc.Status, sc.PausedReason = StatusPaused, PauseUser
		return nil
	})
}

// Resume reopens a paused or circuit-open schedule and clears its failure
// streak. It never resets lifetime spend: a schedule whose next run no longer
// fits its lifetime budget is refused with ErrLifetimeSpent and stays as it is.
func (s *Store) Resume(ctx context.Context, id string) error {
	return s.setStatus(ctx, id, func(sc *Schedule) error {
		if sc.Status != StatusPaused && sc.Status != StatusCircuitOpen {
			return fmt.Errorf("%w: %s", ErrNotActive, sc.Status)
		}
		if sc.SpentLifetimeTokens+sc.Budget.PerRunTokens > sc.Budget.LifetimeTokens {
			return ErrLifetimeSpent
		}
		sc.Status, sc.PausedReason, sc.ConsecutiveFailures = StatusActive, "", 0
		return nil
	})
}

// Renew extends expiry from now by the policy lifetime. A one-shot schedule
// that already fired cannot be renewed into firing again.
func (s *Store) Renew(ctx context.Context, p Policy, id string) error {
	return s.setStatus(ctx, id, func(sc *Schedule) error {
		if sc.Trigger.Kind == TriggerAt && sc.Status == StatusExpired {
			return fmt.Errorf("%w: one-shot schedule is spent", ErrNotActive)
		}
		sc.ExpiresAt = s.clock().Add(p.Expiry())
		if sc.Status == StatusExpired {
			sc.Status = StatusActive
		}
		return nil
	})
}

func (s *Store) setStatus(ctx context.Context, id string, fn func(*Schedule) error) error {
	return s.update(ctx, func(m *Manifest) error {
		sc := m.schedule(id)
		if sc == nil {
			return ErrNotFound
		}
		if err := fn(sc); err != nil {
			return err
		}
		sc.UpdatedAt = s.clock()
		return nil
	})
}

// Delete removes a schedule. Its run records stay for the windows they count in.
func (s *Store) Delete(ctx context.Context, id string) error {
	return s.update(ctx, func(m *Manifest) error {
		i := slices.IndexFunc(m.Schedules, func(sc Schedule) bool { return sc.ID == id })
		if i < 0 {
			return ErrNotFound
		}
		m.Schedules = slices.Delete(m.Schedules, i, i+1)
		return nil
	})
}

// SetPausedAll flips the global switch. Resuming clears a corruption pause too:
// that is the person's acknowledgement of the quarantine.
func (s *Store) SetPausedAll(ctx context.Context, paused bool) error {
	return s.update(ctx, func(m *Manifest) error {
		m.PausedAll = paused
		m.PausedReason = ""
		if paused {
			m.PausedReason = PauseUser
		}
		return nil
	})
}
