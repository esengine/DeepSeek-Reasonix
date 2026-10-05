package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
)

// Headless machine streams keep 1.x's line contract (per-line envelope plus
// turn_status, user_message, tool_started, turn_done). The synchronous
// controller path publishes no turn lifecycle, so this transport supplies it.

// runStreamKinds is the vocabulary both machine streams carry: the kinds 1.x
// emitted. The rest describe host-internal state (leases, inbox, graph, tabs).
var runStreamKinds = map[event.Kind]bool{
	event.TurnStarted: true, event.Reasoning: true, event.Text: true, event.Message: true,
	event.ToolDispatch: true, event.ToolResult: true, event.ToolProgress: true, event.Usage: true,
	event.Notice: true, event.Phase: true, event.TurnPhase: true, event.ApprovalRequest: true,
	event.AskRequest: true, event.TurnDone: true, event.CompactionStarted: true,
	event.CompactionDone: true, event.MCPSurfaceReady: true, event.Retrying: true, event.Steer: true,
	event.GuardianAssessment: true, event.ExtensionSurface: true, event.ExtensionStatus: true,
	event.StreamAttempt: true, event.ContextMaintenanceEvent: true, event.WorkspaceChanged: true,
	event.CompletionSummary: true,
}

const (
	runTurnQueued      = "queued"
	runTurnInProgress  = "in_progress"
	runTurnCompleted   = "completed"
	runTurnFailed      = "failed"
	runTurnInterrupted = "interrupted"
)

// runTurnEnvelope is the one top-level turn a headless run drives.
type runTurnEnvelope struct {
	sessionID string
	turnID    string
	prompt    string
	begun     bool
	announced bool
}

type streamJSONLine struct {
	eventwire.Event
	SessionID string `json:"sessionId,omitempty"`
	TurnID    string `json:"turnId,omitempty"`
	Seq       uint64 `json:"seq"`
	Status    string `json:"status,omitempty"`
}

func newRunTurnID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "turn_" + hex.EncodeToString(b[:])
}

func runTurnTerminalStatus(runErr error, completion runCompletion) string {
	switch {
	case !completion.isError:
		return runTurnCompleted
	case errors.Is(runErr, context.Canceled):
		return runTurnInterrupted
	default:
		return runTurnFailed
	}
}

// BeginTurn opens the turn the run is about to submit and reports it queued.
func (s *runOutputSink) BeginTurn(sessionID, prompt string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turn = runTurnEnvelope{sessionID: sessionID, turnID: newRunTurnID(), prompt: prompt, begun: true}
	s.writeLifecycle(event.Event{}, "turn_status", runTurnQueued)
}

// RecordTurnCompletion counts a turn the controller admitted and finished.
func (s *runOutputSink) RecordTurnCompletion() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turns++
}

// writeStreamEvent writes one kernel event, bracketed by the lifecycle records
// 1.x placed around it.
func (s *runOutputSink) writeStreamEvent(e event.Event) {
	if !runStreamKinds[e.Kind] || s.err != nil {
		return
	}
	if e.Kind == event.ToolResult && e.Tool.Executed {
		s.writeLifecycle(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
			ID: e.Tool.ID, Name: e.Tool.Name, ReadOnly: e.Tool.ReadOnly, StartedAt: e.Tool.StartedAt,
			ParentID: e.Tool.ParentID, Issuer: e.Tool.Issuer,
		}}, "tool_started", s.inTurnStatus())
	}
	if s.format == runOutputStreamJSON {
		s.sequence++
		s.err = s.encoder.Encode(s.streamLine(eventwire.ToWire(e), s.inTurnStatus()))
	} else {
		s.sequence++
		s.err = s.encoder.Encode(s.machineEventRecordFor(e, s.sequence))
	}
	if e.Kind == event.TurnStarted && s.turn.begun && !s.turn.announced {
		s.turn.announced = true
		s.writeLifecycle(event.Event{Text: s.turn.prompt}, "user_message", s.inTurnStatus())
	}
}

// finishTurn closes a begun turn with its terminal status.
func (s *runOutputSink) finishTurn(runErr error, completion runCompletion) {
	if !s.turn.begun || s.err != nil {
		return
	}
	done := event.Event{Kind: event.TurnDone, Err: runErr, Outcome: completion.outcome, Cancelled: errors.Is(runErr, context.Canceled)}
	s.writeLifecycle(done, "turn_done", runTurnTerminalStatus(runErr, completion))
}

// writeLifecycle writes a record this transport supplies under kind. The
// event gives its payload; for events-jsonl only content-free fields survive.
func (s *runOutputSink) writeLifecycle(e event.Event, kind, status string) {
	if s.err != nil || (s.format != runOutputStreamJSON && s.format != runOutputEventsJSONL) {
		return
	}
	s.sequence++
	if s.format == runOutputStreamJSON {
		w := eventwire.ToWire(e)
		w.Kind = kind
		s.err = s.encoder.Encode(s.streamLine(w, status))
		return
	}
	record := machineEventRecord{SchemaVersion: machineSchemaVersion, Sequence: s.sequence, Kind: kind}
	if e.Kind == event.ToolDispatch || e.Kind == event.TurnDone {
		record = s.machineEventRecordFor(e, s.sequence)
		record.Kind = kind
	}
	s.err = s.encoder.Encode(record)
}

func (s *runOutputSink) streamLine(w eventwire.Event, status string) streamJSONLine {
	return streamJSONLine{Event: w, SessionID: s.turn.sessionID, TurnID: s.turn.turnID, Seq: s.sequence, Status: status}
}

func (s *runOutputSink) inTurnStatus() string {
	if !s.turn.begun {
		return ""
	}
	return runTurnInProgress
}
