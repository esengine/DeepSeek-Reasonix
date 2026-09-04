package control

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/store"
)

// A barrier is a historical fact: this turn stopped and waited on a person.
// Nothing about current state can re-derive it once the waiting process dies,
// so it is written before anyone can be asked, and never rewritten.

// Barrier dispositions. Interrupted is absent on purpose: it is derived from an
// open record with no live owner, never written by the process that died.
const (
	barrierOpen      = "open"
	barrierResolved  = "resolved"
	barrierCancelled = "cancelled"
	// barrierSuperseded ends a barrier no live waiter can: a later turn carried
	// its context and took the work over. The edge names that turn, so the
	// journal answers why the question stopped being asked with a cause rather
	// than with "someone saw it".
	barrierSuperseded = "superseded"
)

type barrierRecord struct {
	Barrier   string    `json:"barrier"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status"`
	Summary   string    `json:"summary,omitempty"`
	Successor string    `json:"successor,omitempty"`
	At        time.Time `json:"at"`
}

// AdjudicationEntry is one barrier's whole story, folded from the journal: it
// was opened, and either it ended or it did not. Disposition is empty while a
// barrier is still open — whether that means waiting or interrupted depends on
// this process, not on the record.
type AdjudicationEntry struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Summary     string `json:"summary,omitempty"`
	Disposition string `json:"disposition,omitempty"`
	// SupersededBy names the turn that took the barrier over, empty otherwise.
	SupersededBy string    `json:"supersededBy,omitempty"`
	OpenedAt     time.Time `json:"openedAt"`
	EndedAt      time.Time `json:"endedAt,omitempty"`
}

// Open reports whether nothing has ended this barrier yet.
func (e AdjudicationEntry) Open() bool { return e.Disposition == "" }

// InterruptedAdjudication is a barrier this process found open and did not
// open itself. It is not a Decision: no owner is waiting on an answer, and
// offering one as answerable is how a lost obligation becomes a ghost.
type InterruptedAdjudication struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Summary string `json:"summary,omitempty"`
}

// openBarrier records a barrier before it can be observed. A failure to write
// is returned, not logged: the alternative is asking a person a question the
// host has no record of owing.
func (c *Controller) openBarrier(id, kind, summary string) error {
	err := appendBarrier(c.SessionPath(), barrierRecord{
		Barrier: id, Kind: kind, Status: barrierOpen, Summary: summary, At: time.Now().UTC(),
	})
	c.noteAdjudicationsChanged()
	return err
}

// noteAdjudicationsChanged tells clients the list moved without saying how:
// they read it back, so one authority answers all of them.
func (c *Controller) noteAdjudicationsChanged() {
	if c == nil || c.sink == nil {
		return
	}
	c.sink.Emit(event.Event{Kind: event.AdjudicationsChanged})
}

// closeBarrier records how a barrier ended. A best-effort write: the answer
// already reached its owner, and refusing it here would strand a live turn.
func (c *Controller) closeBarrier(id, status string) {
	_ = appendBarrier(c.SessionPath(), barrierRecord{
		Barrier: id, Status: status, At: time.Now().UTC(),
	})
	c.noteAdjudicationsChanged()
}

func appendBarrier(sessionPath string, rec barrierRecord) error {
	path := store.SessionAdjudication(sessionPath)
	if path == "" {
		return nil
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	// Durable before observable: a crash between this write and the prompt is
	// the case the record exists for.
	return f.Sync()
}

// InterruptedAdjudications returns the barriers this session recorded and never
// closed. In a process that opened none itself, every one of them is a turn
// that died waiting: durable evidence of the wait, with no continuation behind
// it. The suspended response cannot be resumed, and this does not claim it can.
func (c *Controller) InterruptedAdjudications() []InterruptedAdjudication {
	if c == nil {
		return nil
	}
	live := c.approval.liveBarrierIDs()
	var out []InterruptedAdjudication
	for _, e := range AdjudicationHistory(c.SessionPath()) {
		if !e.Open() || live[e.ID] {
			continue
		}
		out = append(out, InterruptedAdjudication{ID: e.ID, Kind: e.Kind, Summary: e.Summary})
	}
	return out
}

// AdjudicationHistory folds the journal in the order barriers were opened.
// Only an open record starts an entry, so a terminal one alone invents no
// history; the first terminal wins, so a duplicate cannot restate a settled
// barrier; an unreadable line is skipped, because a torn tail is what a crash
// leaves.
func AdjudicationHistory(sessionPath string) []AdjudicationEntry {
	path := store.SessionAdjudication(sessionPath)
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	index := map[string]int{}
	var out []AdjudicationEntry
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scan.Scan() {
		var rec barrierRecord
		if err := json.Unmarshal(scan.Bytes(), &rec); err != nil || rec.Barrier == "" {
			continue
		}
		at, seen := index[rec.Barrier]
		if rec.Status == barrierOpen {
			if seen {
				continue
			}
			index[rec.Barrier] = len(out)
			out = append(out, AdjudicationEntry{
				ID: rec.Barrier, Kind: rec.Kind, Summary: rec.Summary, OpenedAt: rec.At,
			})
			continue
		}
		if !seen || !out[at].Open() {
			continue
		}
		out[at].Disposition, out[at].EndedAt = rec.Status, rec.At
		out[at].SupersededBy = rec.Successor
	}
	return out
}

// barrierSummary describes the question well enough for a later process to say
// what was being asked, without carrying anything it would need to resume it.
func barrierSummary(questions []event.AskQuestion) string {
	parts := make([]string, 0, len(questions))
	for _, q := range questions {
		if text := strings.TrimSpace(q.Prompt); text != "" {
			parts = append(parts, text)
		} else if header := strings.TrimSpace(q.Header); header != "" {
			parts = append(parts, header)
		}
	}
	return strings.Join(parts, " / ")
}

// withInheritedInterruptions records the handover and passes the marker on. It
// sits on markInFlightTurn's return so every path that starts a turn records it
// once the turn is identified and durable, and before the model is called.
// Only what the turn actually receives is claimed: a barrier nothing showed it
// is not one it took over.
func (c *Controller) withInheritedInterruptions(marker agent.InFlightTurnMeta) agent.InFlightTurnMeta {
	c.inheritInterruptions(marker.ID)
	return marker
}

func (c *Controller) inheritInterruptions(successor string) {
	if successor == "" {
		return
	}
	inherited := c.InterruptedAdjudications()
	for _, item := range inherited {
		_ = appendBarrier(c.SessionPath(), barrierRecord{
			Barrier: item.ID, Status: barrierSuperseded, Successor: successor, At: time.Now().UTC(),
		})
	}
	if len(inherited) > 0 {
		c.noteAdjudicationsChanged()
	}
}

// inheritedByRunningTurn returns the barriers a turn took over and did not
// finish. The successor is read off the in-flight marker on disk, so this
// answers the same way in the process that wrote the edge and in one that
// found it after a crash: an interruption is never both un-inherited and
// invisible, whichever side of the boundary the question is asked from.
func (c *Controller) inheritedByRunningTurn() []InterruptedAdjudication {
	running := runningTurnID(c.SessionPath())
	if running == "" {
		return nil
	}
	var out []InterruptedAdjudication
	for _, e := range AdjudicationHistory(c.SessionPath()) {
		if e.Disposition == barrierSuperseded && e.SupersededBy == running {
			out = append(out, InterruptedAdjudication{ID: e.ID, Kind: e.Kind, Summary: e.Summary})
		}
	}
	return out
}

// runningTurnID is the turn the session is in the middle of, or empty when the
// last one committed. A completed turn clears its marker, which is what stops
// an inherited interruption from following every later request.
func runningTurnID(sessionPath string) string {
	meta, ok, err := agent.LoadBranchMeta(sessionPath)
	if err != nil || !ok || meta.InFlightTurn == nil {
		return ""
	}
	return meta.InFlightTurn.ID
}

// Adjudications splits the journal the way a reader needs it: what the user
// still has to be told about, and what already ended. Active is derived, not
// stored — a barrier with a live owner is a question being asked, not an
// interruption — so no client can compute this set from the journal alone.
func (c *Controller) Adjudications() (active, history []AdjudicationEntry) {
	if c == nil {
		return nil, nil
	}
	live := c.approval.liveBarrierIDs()
	for _, e := range AdjudicationHistory(c.SessionPath()) {
		switch {
		case !e.Open():
			history = append(history, e)
		case !live[e.ID]:
			active = append(active, e)
		}
	}
	return active, history
}

// RequestContext is what the host owes the next request about work that did not
// finish. It states provenance, not a continuation: there is no identity to
// answer with, because no owner is left to answer to. Derived every request, so
// nothing records having shown it.
func (c *Controller) RequestContext() []string {
	interrupted := append(c.InterruptedAdjudications(), c.inheritedByRunningTurn()...)
	if len(interrupted) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("<interrupted-adjudication>\n")
	b.WriteString("A previous run stopped and waited for a person, and that run is gone.\n")
	for _, item := range interrupted {
		b.WriteString("\n- " + item.Kind)
		if item.Summary != "" {
			b.WriteString(": " + item.Summary)
		}
	}
	b.WriteString("\n\nThe suspended response cannot be resumed, and nothing it deferred was executed. ")
	b.WriteString("Treat this as context for what the user may refer to, not as a question to ")
	b.WriteString("answer or work to continue: any action still wanted has to be proposed again.\n")
	b.WriteString("</interrupted-adjudication>")
	return []string{b.String()}
}

// OpenBarrierForTest and CloseBarrierForTest let a frontend-side test build a
// journal without driving a real ask, which needs a blocking waiter. They write
// exactly what the production paths write.
func (c *Controller) OpenBarrierForTest(id, kind, summary string) {
	_ = c.openBarrier(id, kind, summary)
}

func (c *Controller) CloseBarrierForTest(id, status, successor string) {
	_ = appendBarrier(c.SessionPath(), barrierRecord{
		Barrier: id, Status: status, Successor: successor, At: time.Now().UTC(),
	})
	c.noteAdjudicationsChanged()
}
