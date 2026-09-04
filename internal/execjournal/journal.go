package execjournal

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
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
	statusQueued  = "queued"
	statusStarted = "started"
	statusSettled = "settled"
)

// How an execution with no live owner was cut. Both are derived, never written:
// the process that died had no chance to record either.
const (
	// InterruptedBeforeStart never reached a slot. Nothing it would have done
	// was done, because it had not begun to do it.
	InterruptedBeforeStart = "interrupted-before-start"
	// InterruptedDuringExecution held a slot, so whatever it had already done
	// stands and whatever it had not is simply undone.
	InterruptedDuringExecution = "interrupted-during-execution"
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

// WorkerSpec is the identity the worker layer resolved for an item. An empty
// field is itself the fact: that layer named nothing, so the parent's value
// stands. It is recorded as a whole rather than as two strings because absent
// and "named nothing" are different answers, and only presence tells them apart.
type WorkerSpec struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

// record is one line of the journal. An opening carries identity; a settling
// carries only the id, because identity was already declared and restating it
// would let two lines disagree.
type record struct {
	Execution   string      `json:"execution"`
	Status      string      `json:"status"`
	Group       string      `json:"group,omitempty"`
	Turn        string      `json:"turn,omitempty"`
	Kind        string      `json:"kind,omitempty"`
	Name        string      `json:"name,omitempty"`
	Grant       string      `json:"grant,omitempty"`
	Disposition string      `json:"disposition,omitempty"`
	DependsOn   []string    `json:"dependsOn,omitempty"`
	AdoptedFrom string      `json:"adoptedFrom,omitempty"`
	Worker      *WorkerSpec `json:"worker,omitempty"`
	Cause       string      `json:"cause,omitempty"`
	At          time.Time   `json:"at"`
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
	// DependsOn are the executions this one declared it may not start before.
	// Not scheduler state: a dependency is why an item is not yet ready, a slot
	// is what a ready item waits for, and the two have different remedies.
	DependsOn []string
	// AdoptedFrom is whose answer stood in for running this item, in whatever
	// vocabulary the graph names it. The journal does not interpret it: what
	// kind of source it is belongs to whoever reads the graph back.
	AdoptedFrom string
	// Worker is what the worker layer resolved. Nil means the writer did not
	// record it, which older entries cannot say apart from "named nothing".
	Worker *WorkerSpec
}

// Entry is one execution's whole story, folded from the journal: it was opened,
// and either the orchestration let go of it or it did not. An adopted opening
// is closed on arrival — nothing ran, so nothing can be left running.
type Entry struct {
	ID          string   `json:"id"`
	Group       string   `json:"group,omitempty"`
	Turn        string   `json:"turn,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Name        string   `json:"name,omitempty"`
	Grant       string   `json:"grant,omitempty"`
	Disposition string   `json:"disposition,omitempty"`
	DependsOn   []string `json:"dependsOn,omitempty"`
	// AdoptedFrom is empty on an adopted entry written before this was
	// recorded. That is lossy history, not corruption: the source was never
	// captured, and no other field can be read to guess it.
	AdoptedFrom string `json:"adoptedFrom,omitempty"`
	// Worker is nil on an entry written before it was recorded. A reader must
	// not read that as "named nothing": the fact was never captured, and the
	// store's resolved identity is a different layer that cannot stand in.
	Worker   *WorkerSpec `json:"worker,omitempty"`
	OpenedAt time.Time   `json:"openedAt"`
	// The timestamps carry no omitempty: it does nothing for time.Time, and a
	// tag that appears to drop a zero value while emitting 0001-01-01 reads to
	// a consumer as a transition that happened. Queued and Started answer that.
	QueuedAt time.Time `json:"queuedAt"`
	// Cause is why the scheduler denied admission the first time, never the
	// blocker that remained before execution: a measured run kept reporting
	// slots after slots had freed and the writer ceiling had taken over.
	Cause     string    `json:"cause,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	SettledAt time.Time `json:"settledAt"`
}

// Open reports whether the orchestration still held this execution when the
// journal was last written to.
func (e Entry) Open() bool {
	return e.SettledAt.IsZero() && e.Disposition != DispositionAdopted
}

// Started reports whether this execution was ever granted a slot. An opening
// that settles without one is ordinary: a branch its dependency cut is opened,
// never started, and released when the group ends.
func (e Entry) Started() bool { return !e.StartedAt.IsZero() }

// Queued reports whether the scheduler ever refused this execution admission.
// It proves more than the refusal: an item only reaches the scheduler once its
// dependencies are answered, so a queued entry crossed that gate even when
// nothing else records it doing so.
func (e Entry) Queued() bool { return !e.QueuedAt.IsZero() }

// Interruption names how an execution with no live owner was cut. Neither
// answer offers a continuation; they differ in what may already have happened.
func (e Entry) Interruption() string {
	if e.Started() {
		return InterruptedDuringExecution
	}
	return InterruptedBeforeStart
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
	if err := checkAdoption(disposition, o.AdoptedFrom); err != nil {
		return err
	}
	if err := appendRecord(sessionPath, record{
		Execution: o.ID, Status: statusOpen, Group: o.Group, Turn: o.Turn,
		Kind: o.Kind, Name: o.Name, Grant: o.Grant, Disposition: disposition,
		DependsOn: o.DependsOn, AdoptedFrom: o.AdoptedFrom, Worker: o.Worker,
		At: time.Now().UTC(),
	}); err != nil {
		return err
	}
	if disposition != DispositionAdopted {
		owned.claim(sessionPath, o.ID, true)
	}
	return nil
}

// Queue records the scheduler's first refusal, before anything can observe the
// item waiting. Cause is that first denial and is never rewritten: the blocker
// can change while an item waits, and a record that tracked it would say the
// item was held by something that was not what queued it.
func Queue(sessionPath, id, cause string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	return appendRecord(sessionPath, record{
		Execution: id, Status: statusQueued, Cause: cause, At: time.Now().UTC(),
	})
}

// checkAdoption refuses the two shapes that would make one opening say two
// things: an adoption with nobody to have adopted from, and a source on an item
// that is going to run. Only new writes are held to it — a reader accepts an
// older adopted entry whose source was never captured.
func checkAdoption(disposition, source string) error {
	adopted, named := disposition == DispositionAdopted, strings.TrimSpace(source) != ""
	switch {
	case adopted && !named:
		return errors.New("an adopted opening must name the source whose answer it reuses")
	case !adopted && named:
		return fmt.Errorf("a %s opening cannot name an adoption source", disposition)
	}
	return nil
}

// Start records that an opened execution was granted a slot, before anything
// can observe it running and before the child can act. The write is returned
// rather than logged for the same reason an opening's is: a run whose start
// could not be recorded must not act, or the boundary loses it again.
func Start(sessionPath, id string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	return appendRecord(sessionPath, record{Execution: id, Status: statusStarted, At: time.Now().UTC()})
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
				Name: rec.Name, Grant: rec.Grant, Disposition: rec.Disposition,
				DependsOn: rec.DependsOn, AdoptedFrom: rec.AdoptedFrom,
				Worker: rec.Worker, OpenedAt: rec.At,
			})
			continue
		}
		if !seen || !out[at].Open() {
			continue
		}
		// Each transition is refused once the one after it has landed, and the
		// first of each wins. A status nothing here names is ignored rather
		// than folded into the nearest one it resembles.
		switch rec.Status {
		case statusQueued:
			if !out[at].Queued() && !out[at].Started() {
				out[at].QueuedAt, out[at].Cause = rec.At, rec.Cause
			}
		case statusStarted:
			if !out[at].Started() {
				out[at].StartedAt = rec.At
			}
		case statusSettled:
			out[at].SettledAt = rec.At
		}
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
