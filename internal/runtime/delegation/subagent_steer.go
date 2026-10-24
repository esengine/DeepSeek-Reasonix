package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
)

var (
	ErrSubagentNotRunning = errors.New("that sub-agent is not running, so it cannot read guidance")
	ErrSubagentSettling   = errors.New("that sub-agent is still held by this session but cannot take guidance this moment (it is starting or finishing); send it again shortly, or wait for its result")
	ErrSteerEmpty         = errors.New("guidance for a sub-agent needs some text")
	ErrSteerTooLarge      = errors.New("that guidance is larger than the limit for one message to a sub-agent")
	ErrSteerRateLimited   = errors.New("too much guidance was sent to this sub-agent in the last minute")
)

// SteerSource says who the guidance is from, which the child is told: the
// person speaking, or the delegating session.
type SteerSource int

const (
	SteerFromUser SteerSource = iota
	SteerFromParent
)

// SteerLimits bound what one sub-agent may be sent. Zero lifts a limit.
type SteerLimits struct {
	MaxBytes  int
	PerMinute int
}

// liveRun is a run this process is executing: its conversation, and the hold on
// the agent that executes it once that agent exists.
type liveRun struct {
	sess  *sessionstore.Session
	steer agent.Steerer
}

type steerState struct {
	limits func() SteerLimits
	sent   map[string][]time.Time
	now    func() time.Time
}

// WithSteerLimits sets where the size and rate limits are read, at each message,
// so a changed setting reaches a run that is already going.
func (s *SubagentStore) WithSteerLimits(fn func() SteerLimits) *SubagentStore {
	if s != nil {
		s.mu.Lock()
		s.steer.limits = fn
		s.mu.Unlock()
	}
	return s
}

// attachSteerer hands a live run the agent that executes it.
func (s *SubagentStore) attachSteerer(sess *sessionstore.Session, st agent.Steerer) {
	if s == nil || sess == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.live {
		if r.sess == sess {
			r.steer = st
			return
		}
	}
}

func (s *SubagentStore) clock() time.Time {
	if s.steer.now != nil {
		return s.steer.now()
	}
	return time.Now()
}

// admit applies the named limits to one message for ref and records it. The
// rate window is the last minute, per run.
func (s *SubagentStore) admit(ref, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var limits SteerLimits
	if s.steer.limits != nil {
		limits = s.steer.limits()
	}
	if limits.MaxBytes > 0 && len(text) > limits.MaxBytes {
		return fmt.Errorf("%w (%d bytes, limit %d)", ErrSteerTooLarge, len(text), limits.MaxBytes)
	}
	if limits.PerMinute > 0 {
		now := s.clock()
		recent := s.steer.sent[ref][:0:0]
		for _, at := range s.steer.sent[ref] {
			if now.Sub(at) < time.Minute {
				recent = append(recent, at)
			}
		}
		if len(recent) >= limits.PerMinute {
			s.steer.sent[ref] = recent
			return fmt.Errorf("%w (limit %d)", ErrSteerRateLimited, limits.PerMinute)
		}
		if s.steer.sent == nil {
			s.steer.sent = map[string][]time.Time{}
		}
		s.steer.sent[ref] = append(recent, now)
	}
	return nil
}

// Steer sends guidance to a sub-agent this process is running, to be read at its
// next tool boundary. Only the conversation that owns the run may, the limits
// apply before anything is queued, and a child that is not inside a run to take
// it refuses (ErrSubagentNotRunning) rather than holding text no one will read.
func (s *SubagentStore) Steer(ref, parentSession, workspaceRoot, text string, from SteerSource) error {
	if s == nil || !sessionstore.ValidSubagentRef(strings.TrimSpace(ref)) {
		return ErrSubagentNotFound
	}
	ref = strings.TrimSpace(ref)
	meta, err := s.LoadMeta(ref)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrSubagentNotFound
		}
		return err
	}
	meta.Ref = ref
	if err := s.checkOwner(ref, meta, parentSession, workspaceRoot); err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ErrSteerEmpty
	}
	s.mu.Lock()
	var st agent.Steerer
	if r := s.live[ref]; r != nil {
		st = r.steer
	}
	s.mu.Unlock()
	if st == nil {
		return ErrSubagentNotRunning
	}
	if err := s.admit(ref, text); err != nil {
		return err
	}
	var ok bool
	if from == SteerFromParent {
		ok = st.SteerHostNotice("From the parent session that delegated this task:\n" + text)
	} else {
		ok = st.Steer(text)
	}
	if !ok {
		return ErrSubagentNotRunning
	}
	return nil
}

// steerable has the agent that executes this run hand itself to the store, so
// the run can be spoken to while it works.
func (t *TaskTool) steerable(opts agent.Options, sess *sessionstore.Session) agent.Options {
	opts.OnStart = func(st agent.Steerer) { t.transcripts.attachSteerer(sess, st) }
	opts.AdmissionSaver = t.transcripts.admissionSaver(sess)
	return opts
}

// Execute is task's entry. A call that names a running sub-agent with task_id
// is guidance to it and starts nothing; a call that names a finished one
// continues its transcript as continue_from does. Every other call is a new
// sub-agent.
func (t *TaskTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var call map[string]json.RawMessage
	if err := json.Unmarshal(args, &call); err != nil {
		return t.executeTask(ctx, args)
	}
	var ref string
	if raw, ok := call["task_id"]; ok {
		_ = json.Unmarshal(raw, &ref)
	}
	if ref = strings.TrimSpace(ref); ref == "" {
		return t.executeTask(ctx, args)
	}
	var guidance string
	_ = json.Unmarshal(call["prompt"], &guidance)
	err := t.transcripts.Steer(ref, agent.ParentSession(ctx), t.workspaceRoot, guidance, SteerFromParent)
	switch {
	case err == nil:
		return fmt.Sprintf("Guidance delivered to sub-agent %s. It reads it at its next tool call; collect its result with wait or read_subagent_result when it finishes.", ref), nil
	case errors.Is(err, ErrSubagentNotRunning) && t.transcripts.held(ref):
		// Continuing would start a second writer on a transcript this session
		// still holds; the hold is what tells "finished" from "not ready yet".
		return "", fmt.Errorf("%s: %w", steerCode(ErrSubagentSettling), ErrSubagentSettling)
	case errors.Is(err, ErrSubagentNotRunning):
		delete(call, "task_id")
		call["continue_from"], _ = json.Marshal(ref)
		continued, merr := json.Marshal(call)
		if merr != nil {
			return "", merr
		}
		return t.executeTask(ctx, continued)
	}
	return "", fmt.Errorf("%s: %w", steerCode(err), err)
}

func steerCode(err error) string {
	switch {
	case errors.Is(err, ErrSubagentNotFound):
		return "subagent.not_found"
	case errors.Is(err, ErrSubagentNotOwned):
		return "subagent.not_owned"
	case errors.Is(err, ErrSubagentSettling):
		return "subagent.settling"
	case errors.Is(err, ErrSteerEmpty):
		return "subagent.steer_empty"
	case errors.Is(err, ErrSteerTooLarge):
		return "subagent.steer_too_large"
	case errors.Is(err, ErrSteerRateLimited):
		return "subagent.steer_rate_limited"
	}
	return "subagent.steer_failed"
}
