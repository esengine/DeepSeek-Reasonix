package evidence

import "strings"

// Evidence-gain weights: what one tool round contributed beyond what the turn
// already knew. Positive = the round moved the investigation; zero = motion
// without information (repeats); negative = burning rounds on a known failure.
const (
	gainNewRead       = 1 // first successful read of a path
	gainNewCommand    = 1 // first successful run of a command
	gainNewAction     = 1 // first successful (tool, args) call — covers MCP/custom tools
	gainNewFailure    = 2 // a command failing for the first time localizes an error
	gainStateChange   = 2 // a previously failing command now passes (or vice versa)
	gainMutation      = 3 // a successful write/mutation
	gainTaskProgress  = 1 // todo/step bookkeeping that advances task state
	gainDelegated     = 2 // a sub-agent round-trip that returned
	gainRepeatFailure = -2
)

// explorationRunLimit is how many consecutive look-only rounds still count as
// progress. Deep investigation rarely runs longer before touching something;
// a wandering loop runs until someone notices.
const explorationRunLimit = 6

// ProgressTracker scores each tool round's receipts for new evidence so the
// agent can react to stalled investigation adaptively instead of at a fixed
// round count. State is per user turn, like the ledger it observes.
type ProgressTracker struct {
	readPaths   map[string]bool
	commandRuns map[string]bool
	commandFail map[string]bool
	actionSigs  map[string]bool
	exploreRun  int
}

func NewProgressTracker() *ProgressTracker {
	return &ProgressTracker{
		readPaths:   map[string]bool{},
		commandRuns: map[string]bool{},
		commandFail: map[string]bool{},
		actionSigs:  map[string]bool{},
	}
}

// ScoreRound folds one round's receipts into the tracker and returns the
// round's evidence gain. Reading something new is progress only while it leads
// somewhere: past explorationRunLimit look-only rounds the novelty stops
// counting, because a loop that keeps opening files it has never opened scored
// positive every round and so could never reach the no-progress ladder.
func (t *ProgressTracker) ScoreRound(receipts []Receipt) int {
	if t == nil {
		return 0
	}
	gain := 0
	acted := false
	for _, r := range receipts {
		gain += t.scoreReceipt(r)
		acted = acted || roundActedOn(r)
	}
	if acted {
		t.exploreRun = 0
		return gain
	}
	t.exploreRun++
	if t.exploreRun > explorationRunLimit {
		return 0
	}
	return gain
}

// roundActedOn reports whether a receipt did something other than look: a
// mutation, or any command run. Either one means the exploration led somewhere
// and the run starts over.
func roundActedOn(r Receipt) bool {
	return r.Mutation || r.Write || strings.TrimSpace(r.Command) != ""
}

func (t *ProgressTracker) scoreReceipt(r Receipt) int {
	if command := strings.TrimSpace(r.Command); command != "" {
		return t.scoreCommand(command, r.Success)
	}
	switch {
	case r.Success && (r.Mutation || r.Write):
		return gainMutation
	case r.Success && (r.ToolName == "task" || r.ToolName == "parallel_tasks" || r.ToolName == "fleet"):
		return gainDelegated
	case r.Success && (r.StepProof || r.TodoStep != nil || len(r.Todos) > 0):
		return gainTaskProgress
	case r.Success && r.Read && r.OutputBytes > 0 && len(r.Paths) > 0:
		return t.scoreReads(r.Paths)
	case r.Success:
		// Unknown tools (MCP, custom): a first (tool, args) call is evidence;
		// resending the identical call is not.
		sig := r.ToolName + "\x00" + string(r.Args)
		if t.actionSigs[sig] {
			return 0
		}
		t.actionSigs[sig] = true
		return gainNewAction
	default:
		return 0
	}
}

func (t *ProgressTracker) scoreReads(paths []string) int {
	gain := 0
	for _, path := range paths {
		if path == "" || t.readPaths[path] {
			continue
		}
		t.readPaths[path] = true
		gain += gainNewRead
	}
	return gain
}

func (t *ProgressTracker) scoreCommand(command string, success bool) int {
	seen := t.commandRuns[command]
	failedBefore := t.commandFail[command]
	t.commandRuns[command] = true
	if !success {
		t.commandFail[command] = true
		if failedBefore {
			return gainRepeatFailure
		}
		return gainNewFailure
	}
	delete(t.commandFail, command)
	if failedBefore {
		return gainStateChange
	}
	if seen {
		return 0
	}
	return gainNewCommand
}

// ReceiptsSince returns a copy of the receipts recorded at or after index.
func (l *Ledger) ReceiptsSince(index int) []Receipt {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if index < 0 {
		index = 0
	}
	if index >= len(l.receipts) {
		return nil
	}
	return append([]Receipt(nil), l.receipts[index:]...)
}

// Receipts returns a copy of every receipt recorded this turn, in order —
// the replay feed for shadow observers that must not share ledger memory.
func (l *Ledger) Receipts() []Receipt {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Receipt(nil), l.receipts...)
}
