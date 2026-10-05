package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/session/control"
	"reasonix/internal/state/store"
)

// Reconnecting rebuilds the trajectory pane from this log, so every frame the
// pane draws a row for has to survive it. Rounds are where a turn's clock
// actually goes; dropping their frames left a replayed pane with tool marks and
// no trunk to hang them on, and no anchor for the usage that names them.
func TestWireLogKeepsTheFramesTheTimelineDrawsRowsFor(t *testing.T) {
	session := filepath.Join(testenv.TempDir(t), "session.jsonl")
	var w wireLog
	for _, frame := range []string{
		`{"kind":"turn_started"}`,
		`{"kind":"stream_attempt","streamAttempt":{"id":"sa-1-7","action":"begin"}}`,
		`{"kind":"reasoning","text":"one chunk of many"}`,
		`{"kind":"text","text":"one chunk of many"}`,
		`{"kind":"tool_dispatch","tool":{"id":"t1","name":"bash"}}`,
		`{"kind":"tool_result","tool":{"id":"t1","name":"bash","durationMs":420}}`,
		`{"kind":"graph_delta","graph":{"nodes":[{"id":"t1/fleet-1","state":"running"}]}}`,
		`{"kind":"stream_attempt","streamAttempt":{"id":"sa-1-7","action":"commit"}}`,
		`{"kind":"usage","usage":{"attemptId":"sa-1-7","totalTokens":7}}`,
		`{"kind":"turn_done","cancelled":true}`,
	} {
		w.write(session, []byte(frame))
	}

	data, err := os.ReadFile(store.SessionWireLog(session))
	if err != nil {
		t.Fatalf("read wire log: %v", err)
	}
	var kept []string
	for line := range strings.SplitSeq(strings.TrimRight(string(data), "\n"), "\n") {
		var head struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(line), &head); err != nil {
			t.Fatalf("wire log line is not a frame: %q", line)
		}
		kept = append(kept, head.Kind)
	}

	// Streamed text and reasoning arrive one frame per chunk and draw no row of
	// their own; everything else here is a row a reader expects to find, plus the
	// graph delta, which carries the shape those rows happened inside.
	want := []string{
		"turn_started", "stream_attempt", "tool_dispatch", "tool_result", "graph_delta",
		"stream_attempt", "usage", "turn_done",
	}
	if strings.Join(kept, ",") != strings.Join(want, ",") {
		t.Fatalf("wire log kept %v, want %v", kept, want)
	}
}

// trajectoryServer is the production handler over a controller bound to one
// session.
func trajectoryServer(session string) *Server {
	return trajectoryServerIn("", session)
}

// trajectoryServerIn also says where sessions live, which is what tells a run
// that has not opened one from a run that never will.
func trajectoryServerIn(dir, session string) *Server {
	bc := NewBroadcaster()
	return &Server{
		ctrl: control.New(control.Options{Sink: bc, SessionDir: dir, SessionPath: session}),
		bc:   bc,
		wire: &wireLog{},
	}
}

func readTrajectory(t *testing.T, s *Server) trajectoryView {
	t.Helper()
	rec := httptest.NewRecorder()
	s.trajectory(rec, httptest.NewRequest(http.MethodGet, "/trajectory", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /trajectory = %d %s", rec.Code, rec.Body.String())
	}
	var view trajectoryView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode trajectory: %v (%s)", err, rec.Body.String())
	}
	return view
}

// overflowWireLog fills a session's log past the cap and then offers one more
// qualifying frame, which is the write the cap has to refuse. It returns the
// frame that landed, so a caller can hold the answer to what the prefix is.
func overflowWireLog(t *testing.T, session string) string {
	t.Helper()
	big := `{"kind":"tool_result","tool":{"id":"t1","name":"bash","output":"` +
		strings.Repeat("x", wireLogMaxBytes) + `"}}`
	var w wireLog
	w.write(session, []byte(big))
	w.write(session, []byte(`{"kind":"turn_done"}`))
	if _, err := os.Stat(store.SessionWireLogMeta(session)); err != nil {
		t.Fatalf("the cap refused a frame and left no witness: %v", err)
	}
	return big
}

// A session that recorded and produced nothing is not a session nothing
// recorded. Both used to answer with the same empty array, so a reader had no
// way to tell "nothing happened" from "nothing was kept".
func TestTrajectoryCallsAnEmptyRecordedSessionComplete(t *testing.T) {
	session := filepath.Join(testenv.TempDir(t), "session.jsonl")
	view := readTrajectory(t, trajectoryServer(session))
	if view.Availability != trajectoryComplete || len(view.Events) != 0 {
		t.Fatalf("empty recorded session = %q with %d events, want complete with none",
			view.Availability, len(view.Events))
	}
}

// What comes back is the log, line for line: the replay's claim is that it
// reproduces the pane, and a reordered or re-encoded frame is a different pane.
func TestTrajectoryReturnsThePersistedPrefixVerbatim(t *testing.T) {
	session := filepath.Join(testenv.TempDir(t), "session.jsonl")
	frames := []string{
		`{"kind":"turn_started"}`,
		`{"kind":"tool_dispatch","tool":{"id":"t1","name":"bash"}}`,
		`{"kind":"turn_done"}`,
	}
	var w wireLog
	for _, f := range frames {
		w.write(session, []byte(f))
	}

	view := readTrajectory(t, trajectoryServer(session))
	if view.Availability != trajectoryComplete {
		t.Fatalf("an unbroken log = %q, want complete", view.Availability)
	}
	var got []string
	for _, e := range view.Events {
		got = append(got, string(e))
	}
	if strings.Join(got, "\n") != strings.Join(frames, "\n") {
		t.Fatalf("replay = %v, want %v", got, frames)
	}
}

// The cap refusing a frame is the one thing the log cannot say about itself:
// what is on disk stays a valid prefix, and a reader folding it would take the
// last line as the end of the session.
func TestTrajectoryReportsTruncationWhenTheCapRefusesAFrame(t *testing.T) {
	session := filepath.Join(testenv.TempDir(t), "session.jsonl")
	kept := overflowWireLog(t, session)

	view := readTrajectory(t, trajectoryServer(session))
	if view.Availability != trajectoryTruncated {
		t.Fatalf("a log the cap closed = %q, want truncated", view.Availability)
	}
	if len(view.Events) != 1 || string(view.Events[0]) != kept {
		t.Fatalf("the prefix did not survive the refusal: %d events", len(view.Events))
	}
}

// The refusal happened in a process that is gone. Nothing in the bytes it left
// behind records it, so unless the fact was made durable the next process reads
// a well-formed prefix and calls the session whole.
func TestTrajectoryStillReportsTruncationAfterTheWriterIsGone(t *testing.T) {
	session := filepath.Join(testenv.TempDir(t), "session.jsonl")
	overflowWireLog(t, session)

	// A second server with its own wireLog: no in-memory state carries over,
	// which is what a restart leaves the reader with.
	view := readTrajectory(t, trajectoryServer(session))
	if view.Availability != trajectoryTruncated {
		t.Fatalf("truncation did not survive the writer = %q, want truncated", view.Availability)
	}
}

// The control on the test above: with the witness gone, the same bytes read as
// a whole session. It is the durable record doing the work, not the log's size
// or shape — which is what a reader would go back to guessing from.
func TestTrajectoryTruncationRestsOnTheWitnessAndNothingElse(t *testing.T) {
	session := filepath.Join(testenv.TempDir(t), "session.jsonl")
	overflowWireLog(t, session)
	if err := os.Remove(store.SessionWireLogMeta(session)); err != nil {
		t.Fatalf("remove witness: %v", err)
	}

	if view := readTrajectory(t, trajectoryServer(session)); view.Availability != trajectoryComplete {
		t.Fatalf("without the witness = %q; the answer has to come from it alone", view.Availability)
	}
}

// A run with nowhere to record is not a recorded run that stayed quiet, and the
// shape is the studio window's own: desktop/next mints no session at launch, so
// a pane read before its first turn has a session directory and nothing in it.
// The answer comes from the writer's precondition, which holds before any file
// could have existed.
func TestTrajectorySaysNotRecordedBeforeASessionExists(t *testing.T) {
	view := readTrajectory(t, trajectoryServerIn(testenv.TempDir(t), ""))
	if view.Availability != trajectoryNotRecorded {
		t.Fatalf("a pane with no session yet = %q, want not_recorded", view.Availability)
	}
	if len(view.Events) != 0 {
		t.Fatalf("nothing recorded and %d events came back", len(view.Events))
	}
}

// A log that cannot be read supports no claim about the session. Answering with
// an empty array would put the reader's own failure on the agent's record.
func TestTrajectoryRefusesRatherThanReportAnUnreadableLogAsEmpty(t *testing.T) {
	session := filepath.Join(testenv.TempDir(t), "session.jsonl")
	if err := os.MkdirAll(store.SessionWireLog(session), 0o700); err != nil {
		t.Fatalf("stage an unreadable log: %v", err)
	}

	rec := httptest.NewRecorder()
	trajectoryServer(session).trajectory(rec, httptest.NewRequest(http.MethodGet, "/trajectory", nil))
	if rec.Code == http.StatusOK || !strings.Contains(rec.Body.String(), codeTrajectoryUnreadable) {
		t.Fatalf("unreadable log = %d %s, want a %s refusal", rec.Code, rec.Body.String(), codeTrajectoryUnreadable)
	}
}
