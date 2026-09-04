package control

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"

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
)

type barrierRecord struct {
	Barrier string    `json:"barrier"`
	Kind    string    `json:"kind"`
	Status  string    `json:"status"`
	Summary string    `json:"summary,omitempty"`
	At      time.Time `json:"at"`
}

// AdjudicationEntry is one barrier's whole story, folded from the journal: it
// was opened, and either it ended or it did not. Disposition is empty while a
// barrier is still open — whether that means waiting or interrupted depends on
// this process, not on the record.
type AdjudicationEntry struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Summary     string    `json:"summary,omitempty"`
	Disposition string    `json:"disposition,omitempty"`
	OpenedAt    time.Time `json:"openedAt"`
	EndedAt     time.Time `json:"endedAt,omitempty"`
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
	return appendBarrier(c.SessionPath(), barrierRecord{
		Barrier: id, Kind: kind, Status: barrierOpen, Summary: summary, At: time.Now().UTC(),
	})
}

// closeBarrier records how a barrier ended. A best-effort write: the answer
// already reached its owner, and refusing it here would strand a live turn.
func (c *Controller) closeBarrier(id, status string) {
	_ = appendBarrier(c.SessionPath(), barrierRecord{
		Barrier: id, Status: status, At: time.Now().UTC(),
	})
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

// RequestContext is what the host owes the next request about work that did not
// finish. It states provenance, not a continuation: there is no identity to
// answer with, because no owner is left to answer to. Derived every request, so
// nothing records having shown it.
func (c *Controller) RequestContext() []string {
	interrupted := c.InterruptedAdjudications()
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
