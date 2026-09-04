package execjournal

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"

	"reasonix/internal/store"
)

// Statuses. Interrupted is absent on purpose: it is derived from an open entry
// with no live owner, never written by the process that died.
const (
	statusOpen    = "open"
	statusSettled = "settled"
)

// Dispositions an opening carries. They say what the orchestration intends to
// do with this item, not how it ended.
const (
	// DispositionPending is an item the orchestration will execute.
	DispositionPending = "pending"
	// DispositionAdopted stands in for running: an earlier answer was reused, so
	// nothing executes and no owner can go missing.
	DispositionAdopted = "adopted"
)

// record is one line of the journal. An opening carries identity; a settling
// carries only the id, because identity was already declared and restating it
// would let two lines disagree.
type record struct {
	Execution   string    `json:"execution"`
	Status      string    `json:"status"`
	Group       string    `json:"group,omitempty"`
	Turn        string    `json:"turn,omitempty"`
	Kind        string    `json:"kind,omitempty"`
	Name        string    `json:"name,omitempty"`
	Grant       string    `json:"grant,omitempty"`
	Disposition string    `json:"disposition,omitempty"`
	At          time.Time `json:"at"`
}

// Opening is one delegated item as its orchestration declares it, before the
// item is allowed to run. It proves the work entered orchestration; it does not
// prove the work started, ran, or can be resumed. What is not known at that
// moment — what it waited on, what it produced — belongs to a later record or
// to the sub-agent store, never to a field this one would have to guess.
type Opening struct {
	// ID is the execution's identity, the same one the run graph draws it by,
	// so a later reader joins the two without matching two naming schemes.
	ID string
	// Group is the fan-out that owns it: the parent tool call's identity.
	Group string
	// Turn is the parent turn this was opened under, as the in-flight marker
	// names it. It is the only turn identity proven to survive a crash.
	Turn string
	Kind string
	Name string
	// Grant is the authority envelope, recorded as the caller spells it. The
	// journal does not interpret it: a second vocabulary for the same values is
	// a second place they can drift.
	Grant       string
	Disposition string
}

// Entry is one execution's whole story, folded from the journal: it was opened,
// and either the orchestration let go of it or it did not. An adopted opening
// is closed on arrival — nothing ran, so nothing can be left running.
type Entry struct {
	ID          string    `json:"id"`
	Group       string    `json:"group,omitempty"`
	Turn        string    `json:"turn,omitempty"`
	Kind        string    `json:"kind,omitempty"`
	Name        string    `json:"name,omitempty"`
	Grant       string    `json:"grant,omitempty"`
	Disposition string    `json:"disposition,omitempty"`
	OpenedAt    time.Time `json:"openedAt"`
	SettledAt   time.Time `json:"settledAt,omitempty"`
}

// Open reports whether the orchestration still held this execution when the
// journal was last written to.
func (e Entry) Open() bool {
	return e.SettledAt.IsZero() && e.Disposition != DispositionAdopted
}

// owned is what this process still holds. It is process-wide because that is
// the scope of the question it answers: two assemblies in one process are still
// one process, and a set per assembly would let each report the other's running
// work as interrupted. Claims are keyed by session, so nothing leaks between
// conversations — or between tests.
var owned = liveSet{ids: map[string]bool{}}

type liveSet struct {
	mu  sync.Mutex
	ids map[string]bool
}

// Open records a delegation before it may run, and claims it as this process's.
// The write is returned rather than logged: an execution that could not be
// recorded must not start, because the record is the only thing that would
// survive to say it ever did.
func Open(sessionPath string, o Opening) error {
	if strings.TrimSpace(o.ID) == "" {
		return nil
	}
	disposition := o.Disposition
	if disposition == "" {
		disposition = DispositionPending
	}
	if err := appendRecord(sessionPath, record{
		Execution: o.ID, Status: statusOpen, Group: o.Group, Turn: o.Turn,
		Kind: o.Kind, Name: o.Name, Grant: o.Grant, Disposition: disposition,
		At: time.Now().UTC(),
	}); err != nil {
		return err
	}
	if disposition != DispositionAdopted {
		owned.claim(sessionPath, o.ID, true)
	}
	return nil
}

// Settle records that the orchestration has let go of an execution. Best-effort:
// the result already reached its caller, and failing here would strand a live
// turn over a record whose only reader is a process that does not exist yet.
func Settle(sessionPath, id string) {
	if strings.TrimSpace(id) == "" {
		return
	}
	_ = appendRecord(sessionPath, record{Execution: id, Status: statusSettled, At: time.Now().UTC()})
	owned.claim(sessionPath, id, false)
}

// Live reports whether this process still owns an execution the journal shows
// as open.
func Live(sessionPath, id string) bool {
	owned.mu.Lock()
	defer owned.mu.Unlock()
	return owned.ids[liveKey(sessionPath, id)]
}

// Disown drops this process's claims on a session without settling them, which
// is what a restart does to the process that died. Nothing in production forgets
// a claim this way; a test uses it to stand where the next process stands.
func Disown(sessionPath string) {
	owned.mu.Lock()
	defer owned.mu.Unlock()
	for key := range owned.ids {
		if strings.HasPrefix(key, sessionPath+"\x00") {
			delete(owned.ids, key)
		}
	}
}

// Interrupted is what this process found open and did not open itself: durable
// evidence that a delegation was opened and never settled. Opened, not started
// — an item still waiting on a dependency reads the same, because the journal
// records entering the orchestration, not reaching a slot. It is not a handle
// to resume anything: offering one is how a lost execution becomes a ghost.
func Interrupted(sessionPath string) []Entry {
	var out []Entry
	for _, e := range History(sessionPath) {
		if e.Open() && !Live(sessionPath, e.ID) {
			out = append(out, e)
		}
	}
	return out
}

func (s *liveSet) claim(sessionPath, id string, held bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if held {
		s.ids[liveKey(sessionPath, id)] = true
		return
	}
	delete(s.ids, liveKey(sessionPath, id))
}

// liveKey scopes a claim to its session. Execution ids come from a provider's
// tool-call ids, which are unique within a conversation and promised nothing
// beyond it.
func liveKey(sessionPath, id string) string { return sessionPath + "\x00" + id }

// History folds the journal in the order executions were opened. Only an
// opening starts an entry, so a settling alone invents no history; the first
// settling wins, so a duplicate cannot reopen a closed one; an unreadable line
// is skipped, because a torn tail is what a crash leaves.
func History(sessionPath string) []Entry {
	path := store.SessionExecution(sessionPath)
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	index := map[string]int{}
	var out []Entry
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scan.Scan() {
		var rec record
		if err := json.Unmarshal(scan.Bytes(), &rec); err != nil || rec.Execution == "" {
			continue
		}
		at, seen := index[rec.Execution]
		if rec.Status == statusOpen {
			if seen {
				continue
			}
			index[rec.Execution] = len(out)
			out = append(out, Entry{
				ID: rec.Execution, Group: rec.Group, Turn: rec.Turn, Kind: rec.Kind,
				Name: rec.Name, Grant: rec.Grant, Disposition: rec.Disposition, OpenedAt: rec.At,
			})
			continue
		}
		if !seen || !out[at].Open() {
			continue
		}
		out[at].SettledAt = rec.At
	}
	return out
}

func appendRecord(sessionPath string, rec record) error {
	path := store.SessionExecution(sessionPath)
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
	// Durable before observable: a crash between this write and the child
	// starting is the case the record exists for.
	return f.Sync()
}
