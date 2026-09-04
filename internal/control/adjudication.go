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
	open := loadOpenBarriers(c.SessionPath())
	if len(open) == 0 {
		return nil
	}
	live := c.approval.liveBarrierIDs()
	out := make([]InterruptedAdjudication, 0, len(open))
	for _, rec := range open {
		if live[rec.Barrier] {
			continue
		}
		out = append(out, InterruptedAdjudication{ID: rec.Barrier, Kind: rec.Kind, Summary: rec.Summary})
	}
	return out
}

// loadOpenBarriers folds the log: a barrier is open until a later record
// closes it. Unreadable or truncated lines are skipped rather than failing the
// read — a torn tail is exactly what a crash leaves.
func loadOpenBarriers(sessionPath string) []barrierRecord {
	path := store.SessionAdjudication(sessionPath)
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	opened := map[string]barrierRecord{}
	var order []string
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scan.Scan() {
		var rec barrierRecord
		if err := json.Unmarshal(scan.Bytes(), &rec); err != nil || rec.Barrier == "" {
			continue
		}
		if rec.Status == barrierOpen {
			if _, seen := opened[rec.Barrier]; !seen {
				order = append(order, rec.Barrier)
			}
			opened[rec.Barrier] = rec
			continue
		}
		delete(opened, rec.Barrier)
	}
	out := make([]barrierRecord, 0, len(opened))
	for _, id := range order {
		if rec, ok := opened[id]; ok {
			out = append(out, rec)
		}
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
