package teamroster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/base/fileutil"
)

// SlotCause is why a member was called to speak, which decides whether its
// turn counts against the group's relay limit.
type SlotCause string

const (
	CauseUser    SlotCause = "user"
	CauseMention SlotCause = "mention"
	CauseHand    SlotCause = "hand"
	CausePost    SlotCause = "post"
)

// Slot is one member waiting for its turn. By is who called it, and Ceiling
// the approval-mode ceiling an agent's call carries into that turn.
type Slot struct {
	Member  string    `json:"member"`
	Cause   SlotCause `json:"cause"`
	By      string    `json:"by,omitempty"`
	Ceiling string    `json:"ceiling,omitempty"`
}

// Speaking is the slot whose turn is in flight; Key is the delivery that
// started it.
type Speaking struct {
	Slot
	Key         string    `json:"key"`
	StartedAt   time.Time `json:"startedAt"`
	LastEventAt time.Time `json:"lastEventAt"`
}

// RoundState is where a group's turn-taking stands. Seen is, per member, the
// last log sequence number its session has been shown.
type RoundState struct {
	Round    int64            `json:"round"`
	Step     int64            `json:"step"`
	Queue    []Slot           `json:"queue"`
	Speaking *Speaking        `json:"speaking,omitempty"`
	Working  []string         `json:"working"`
	Relays   int              `json:"relays"`
	Parked   []Slot           `json:"parked"`
	Seen     map[string]int64 `json:"seen"`
}

func (r *RoundState) check() error {
	slots := append(append([]Slot{}, r.Queue...), r.Parked...)
	if r.Speaking != nil {
		slots = append(slots, r.Speaking.Slot)
	}
	for _, s := range slots {
		if !agentID.MatchString(s.Member) || (s.By != "" && s.By != ByUser && !agentID.MatchString(s.By)) {
			return fieldErr("round.member", ErrInvalid)
		}
		if !ceiling.MatchString(s.Ceiling) {
			return fieldErr("ceiling", ErrInvalid)
		}
		switch s.Cause {
		case CauseUser, CauseMention, CauseHand, CausePost:
		default:
			return fieldErr("round.cause", ErrInvalid)
		}
	}
	for _, m := range r.Working {
		if !agentID.MatchString(m) {
			return fieldErr("round.member", ErrInvalid)
		}
	}
	for m := range r.Seen {
		if !agentID.MatchString(m) {
			return fieldErr("round.member", ErrInvalid)
		}
	}
	if r.Relays < 0 || r.Round < 0 || r.Step < 0 {
		return fieldErr("round", ErrInvalid)
	}
	return nil
}

// Round reads the group's round state; a group that never ran a round has the
// zero state.
func (s Store) Round(group string) (RoundState, error) {
	dir, err := s.groupDir(group)
	if err != nil {
		return RoundState{}, err
	}
	data, err := os.ReadFile(filepath.Join(dir, RoundFile))
	if errors.Is(err, os.ErrNotExist) {
		return RoundState{Seen: map[string]int64{}}, nil
	}
	if err != nil {
		return RoundState{}, err
	}
	var r RoundState
	if err := json.Unmarshal(data, &r); err != nil {
		return RoundState{}, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	if err := r.check(); err != nil {
		return RoundState{}, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	if r.Seen == nil {
		r.Seen = map[string]int64{}
	}
	return r, nil
}

// SaveRound publishes the group's round state atomically, under the same lock
// its log is appended under.
func (s Store) SaveRound(ctx context.Context, group string, r RoundState) error {
	if err := r.check(); err != nil {
		return err
	}
	dir, err := s.groupDir(group)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return s.withLock(ctx, dir, GroupLogFile, func() error {
		return fileutil.AtomicWriteFile(filepath.Join(dir, RoundFile), data, 0o600)
	})
}
