package delegation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

// errLineageUnproven marks a lineage the on-disk records cannot establish, as
// opposed to one that could not be read.
var errLineageUnproven = errors.New("session lineage not established")

var (
	ErrSubagentNotFound = errors.New("no sub-agent run answers to that reference")
	ErrSubagentNotOwned = errors.New("that sub-agent run does not belong to this conversation")
)

// RunState is what a reader is told about a run. Running means this process is
// executing it now; a stored run that says running while nothing holds it was
// left by an owner that is gone, and is reported as interrupted.
type RunState string

const (
	RunRunning     RunState = "running"
	RunCompleted   RunState = "completed"
	RunFailed      RunState = "failed"
	RunCancelled   RunState = "cancelled"
	RunInterrupted RunState = "interrupted"
)

// defaultTranscriptPage bounds one read, so a long run is fetched in pages.
const defaultTranscriptPage = 500

// RunInfo describes one sub-agent run of a conversation.
type RunInfo struct {
	Ref           string
	Name          string
	Kind          string
	Model         string
	Effort        string
	State         RunState
	ParentSession string
	// ParentToolCallID is the call in the parent's transcript that spawned the
	// run; it is how a reader pairs a task card with its run.
	ParentToolCallID string
	StartedAt        time.Time
	UpdatedAt        time.Time
}

// RunTranscript is one page of a run's conversation: Messages are those from
// index After, Next is where the following page starts, Total how many exist now.
type RunTranscript struct {
	Info     RunInfo
	Messages []provider.Message
	After    int
	Next     int
	Total    int
	Live     bool
}

func (s *SubagentStore) setLive(ref string, sess *sessionstore.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess == nil {
		delete(s.live, ref)
		delete(s.steer.sent, ref)
		return
	}
	s.live[ref] = &liveRun{sess: sess}
}

func (s *SubagentStore) liveSession(ref string) *sessionstore.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.live[ref]; r != nil {
		return r.sess
	}
	return nil
}

// checkOwner is the one rule for who may read a run: the conversation that
// spawned it or one it descends from, in the workspace it ran in.
func (s *SubagentStore) checkOwner(ref string, meta sessionstore.SubagentMeta, parentSession, workspaceRoot string) error {
	parentSession = strings.TrimSpace(parentSession)
	owner := strings.TrimSpace(meta.ParentSession)
	if owner != parentSession {
		ok, err := s.isAncestorSession(owner, parentSession)
		if errors.Is(err, errLineageUnproven) {
			return fmt.Errorf("%w: subagent reference %q ownership could not be verified: %w", ErrSubagentNotOwned, ref, err)
		}
		if err != nil {
			return fmt.Errorf("subagent reference %q ownership could not be verified: %w", ref, err)
		}
		if !ok {
			return fmt.Errorf("%w: %q is outside the current conversation lineage", ErrSubagentNotOwned, ref)
		}
	}
	if want := strings.TrimSpace(workspaceRoot); want != "" && strings.TrimSpace(meta.WorkspaceRoot) != want {
		return fmt.Errorf("%w: %q belongs to a different workspace", ErrSubagentNotOwned, ref)
	}
	return nil
}

func (s *SubagentStore) infoOf(meta sessionstore.SubagentMeta) RunInfo {
	state := RunState(meta.Status)
	if meta.Status == sessionstore.SubagentRunning && !s.held(meta.Ref) {
		state = RunInterrupted
	}
	return RunInfo{
		Ref: meta.Ref, Name: meta.Name, Kind: meta.Kind, Model: meta.Model, Effort: meta.Effort,
		State: state, ParentSession: meta.ParentSession, ParentToolCallID: meta.ParentToolCallID,
		StartedAt: meta.CreatedAt, UpdatedAt: meta.UpdatedAt,
	}
}

// Runs lists the sub-agent runs a conversation spawned, newest first.
func (s *SubagentStore) Runs(parentSession, workspaceRoot string) ([]RunInfo, error) {
	if s == nil || strings.TrimSpace(parentSession) == "" {
		return nil, nil
	}
	artifacts, err := sessionstore.ListSubagentsByParent(filepath.Dir(s.dir), parentSession)
	if err != nil {
		return nil, err
	}
	out := make([]RunInfo, 0, len(artifacts))
	for _, a := range artifacts {
		if want := strings.TrimSpace(workspaceRoot); want != "" && strings.TrimSpace(a.Meta.WorkspaceRoot) != want {
			continue
		}
		meta := a.Meta
		meta.Ref = a.Ref
		out = append(out, s.infoOf(meta))
	}
	slices.SortFunc(out, func(a, b RunInfo) int { return b.StartedAt.Compare(a.StartedAt) })
	return out, nil
}

// Transcript reads a run's conversation from index after, at most limit
// messages (a page when zero). A run this process is executing is read from
// memory; any other from its stored transcript.
func (s *SubagentStore) Transcript(ref, parentSession, workspaceRoot string, after, limit int) (RunTranscript, error) {
	if s == nil || !sessionstore.ValidSubagentRef(strings.TrimSpace(ref)) {
		return RunTranscript{}, ErrSubagentNotFound
	}
	ref = strings.TrimSpace(ref)
	meta, err := s.LoadMeta(ref)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RunTranscript{}, ErrSubagentNotFound
		}
		return RunTranscript{}, err
	}
	meta.Ref = ref
	if err := s.checkOwner(ref, meta, parentSession, workspaceRoot); err != nil {
		return RunTranscript{}, err
	}
	if after < 0 {
		after = 0
	}
	if limit <= 0 {
		limit = defaultTranscriptPage
	}
	out := RunTranscript{Info: s.infoOf(meta), After: after}
	sess := s.liveSession(ref)
	if sess != nil {
		// A run is recorded complete a moment before its owner lets go of it:
		// only one that still says running is being written.
		out.Live = meta.Status == sessionstore.SubagentRunning
	} else {
		loaded, err := sessionstore.LoadSession(s.sessionPath(ref))
		if err != nil {
			return RunTranscript{}, fmt.Errorf("load subagent transcript %q: %w", ref, err)
		}
		sess = loaded
	}
	out.Total = sess.Len()
	out.Messages = sess.MessageRange(after, after+limit)
	out.Next = after + len(out.Messages)
	return out, nil
}
