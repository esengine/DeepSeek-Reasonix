package serve

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"sync"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/state/store"
)

// The trajectory pane is built from wire frames, so the frames are what gets
// kept: replaying them reproduces the pane row for row, with no second schema
// to map through. internal/state/trajectory records a richer audit stream for offline
// analysis and answers a different question.
const wireLogMaxBytes = 8 << 20

// Only the kinds the pane draws a row for. Streamed text and reasoning arrive
// one frame per chunk and would be most of the file while contributing nothing
// to read back; keeping them out is what lets each write close its handle.
var wireLogKinds = map[string]bool{
	"turn_started": true, "turn_done": true, "message": true,
	"tool_dispatch": true, "tool_result": true, "usage": true,
	// The round is the row a turn's time lands on, and usage is addressed by
	// the attempt id it carries. Without these frames a replayed pane loses
	// both, and it is this list's row-for-row claim that breaks.
	"stream_attempt":      true,
	"guardian_assessment": true, "approval_request": true, "ask_request": true,
	"compaction_started": true, "compaction_done": true, "retrying": true,
	"steer": true, "context_maintenance": true, "completion_summary": true,
	"notice": true,
	// The run graph is the one shape a replay cannot re-derive: a dependency and
	// an adopted answer exist nowhere else in the stream, so without these frames
	// a reopened window draws a run that never waited on anything.
	"graph_delta": true,
	// Whether the plan advanced or was only rewritten is derivable from nothing
	// else in the log: the task list rides the tool frames, but the verdict on
	// what a write did to it does not.
	"todo_progress": true,
	// Whether the run stopped producing observable effects is the host's own
	// reading; nothing else in the log carries the round count it rests on.
	"progress_watch": true,
	// What serialising writers across sessions actually cost this one. Nothing
	// else in the log carries it: the notice only fires past the grace, and it
	// carries a sentence rather than a number.
	"workspace_lease": true,
}

// wireLogSkipped names the kinds deliberately left out, so every kind is
// classified by one list or the other. A frame in neither is a decision nobody
// made — which is how the plan verdict was absent from every replay while the
// kernel was emitting it.
var wireLogSkipped = map[string]bool{
	// Stream deltas: one frame per chunk, most of the file, nothing to read back
	// that the message frame does not already carry.
	"reasoning": true, "text": true, "phase": true,
	"tool_progress": true, "compaction_progress": true,
	// Live surfaces a reopened window re-reads from the host rather than replays.
	"mcp_surface_ready": true, "extension_surface": true, "extension_status": true,
	"workspace_changed": true, "turn_phase": true, "inbox_changed": true,
	// Content-free invalidation: a reopened window reads /adjudications, and
	// replaying the notice would tell it to re-read something it just read.
	"adjudications_changed": true,
	// The agent's tabs are read from the session that owns them.
	"browser_tabs_changed": true,
}

// codeTrajectoryUnreadable refuses a read that could not establish coverage.
// It is not "the session has no frames": that answer is a claim, and a log this
// handler could not read supports none.
const codeTrajectoryUnreadable = "trajectory.unreadable"

// trajectoryAvailability is what the returned frames cover. The three answers
// are kept apart because a reader that cannot tell them apart reads absence as
// a quiet session: a run nothing records, a log the cap stopped extending, and
// a recorded session that has produced no frames yet all used to answer with
// the same empty array.
type trajectoryAvailability string

const (
	// trajectoryComplete: every qualifying frame this session produced, from its
	// start to what is durable now, is in Events. Empty Events under this answer
	// means the session produced none — not that none were kept.
	trajectoryComplete trajectoryAvailability = "complete"
	// trajectoryTruncated: Events is a trustworthy prefix and nothing more. What
	// follows the last one is unknown, never "nothing happened".
	trajectoryTruncated trajectoryAvailability = "truncated"
	// trajectoryNotRecorded: nothing recorded this session, so there is no prefix
	// at all. Answered from the writer's own precondition, never from a missing
	// file, which cannot tell this from a log that has not been written to yet.
	trajectoryNotRecorded trajectoryAvailability = "not_recorded"
)

// trajectoryView is the read model: the frames, and what they cover. Coverage
// travels with the frames because a caller cannot reconstruct it from them —
// the last line of a log the cap closed looks exactly like the last line of a
// session that ended there.
type trajectoryView struct {
	Availability trajectoryAvailability `json:"availability"`
	Events       []json.RawMessage      `json:"events"`
}

// wireLogMeta is what the log does not contain. One field, because the boundary
// itself would be a seq and the broadcaster numbers frames per process: a
// resumed session restarts at one, so a number here would name a numbering that
// no longer exists. The last retained line is readable from the log anyway.
type wireLogMeta struct {
	Truncated bool `json:"truncated"`
}

type wireLog struct {
	mu sync.Mutex
	// The log this process has already witnessed a refusal against. Every frame
	// after the first refused one is refused too; keyed by path because /resume
	// and /new move the writer to another log.
	witnessed string
}

// attachWireLog mirrors qualifying broadcast frames to the current session's
// log. Subscribing is how the SSE handler reads the same stream, so the log
// sees exactly what a connected client would have.
func (s *Server) attachWireLog() {
	ch, unsubscribe := s.bc.Subscribe()
	go func() {
		defer unsubscribe()
		for frame := range ch {
			// The session is read per frame, not cached: /resume and /new swap it
			// underneath and the next row belongs to the new one.
			s.wire.write(s.recordedSession(), frame.Data)
		}
	}()
}

// recordedSession is the session whose frames are being recorded, and empty
// when nothing records them. Both the writer and the reader ask it, so what
// /trajectory reports about coverage is the writer's own precondition rather
// than an inference from the filesystem.
func (s *Server) recordedSession() string {
	if s.wire == nil {
		return ""
	}
	return s.ctl().SessionPath()
}

func (w *wireLog) write(sessionPath string, frame []byte) {
	path := store.SessionWireLog(sessionPath)
	if path == "" {
		return
	}
	var head struct {
		Kind           string `json:"kind"`
		NonPersistable bool   `json:"nonPersistable"`
	}
	if err := json.Unmarshal(frame, &head); err != nil || !wireLogKinds[head.Kind] {
		return
	}
	if head.NonPersistable {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	// Reopened per write rather than held: a retained handle blocks the session
	// file's directory from being removed on Windows, and the filtered volume
	// makes the cost irrelevant.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	// A runaway turn must not fill the disk; drop rather than truncate, so what
	// is on disk stays a prefix of what happened. The prefix is only honest if
	// it says so, which is what the witness is for.
	if st, err := f.Stat(); err == nil && st.Size() > wireLogMaxBytes {
		w.witness(sessionPath)
		return
	}
	_, _ = f.Write(append(frame, '\n'))
}

// witness records that the cap refused a frame. Written on the refusal and not
// on the crossing, so a log over the cap with no witness is a session that
// stopped before the next frame arrived rather than one that dropped it.
func (w *wireLog) witness(sessionPath string) {
	path := store.SessionWireLogMeta(sessionPath)
	if path == "" || w.witnessed == path {
		return
	}
	body, err := json.Marshal(wireLogMeta{Truncated: true})
	if err != nil {
		return
	}
	if err := fileutil.AtomicWriteFile(path, body, 0o600); err != nil {
		return
	}
	w.witnessed = path
}

// truncationWitnessed reads the log's refusal record. Missing is the ordinary
// answer and means no frame was refused; anything else is unreadable, and an
// unreadable witness may not be reported as an intact log.
func truncationWitnessed(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var meta wireLogMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return false, err
	}
	return meta.Truncated, nil
}

// trajectory answers with the persisted frames and what they cover. A read that
// fails is refused rather than answered: an empty array is a claim about the
// session, and a log that could not be read supports no claim at all.
func (s *Server) trajectory(w http.ResponseWriter, _ *http.Request) {
	sessionPath := s.recordedSession()
	if sessionPath == "" {
		writeJSON(w, trajectoryView{Availability: trajectoryNotRecorded, Events: []json.RawMessage{}})
		return
	}
	s.wire.mu.Lock()
	data, err := os.ReadFile(store.SessionWireLog(sessionPath))
	witnessed, witnessErr := truncationWitnessed(store.SessionWireLogMeta(sessionPath))
	s.wire.mu.Unlock()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		refuse(w, http.StatusInternalServerError, codeTrajectoryUnreadable, err.Error(), nil)
		return
	}
	if witnessErr != nil {
		refuse(w, http.StatusInternalServerError, codeTrajectoryUnreadable, witnessErr.Error(), nil)
		return
	}
	// The witness is the only authority. Past the cap the next frame will be
	// refused, but until one is nothing has been dropped — reading truncation off
	// a log's size reports a session that grew large as one that lost frames.
	view := trajectoryView{Availability: trajectoryComplete, Events: []json.RawMessage{}}
	if witnessed {
		view.Availability = trajectoryTruncated
	}
	for line := range strings.SplitSeq(strings.TrimRight(string(data), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		view.Events = append(view.Events, json.RawMessage(line))
	}
	writeJSON(w, view)
}
