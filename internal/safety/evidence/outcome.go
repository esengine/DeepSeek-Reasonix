package evidence

import (
	"path"
	"reasonix/internal/contract/hostaudit"
	"strings"

	"reasonix/internal/safety/shellsafe"
)

// stall is the check a turn is currently stuck on: which one, how long it has
// stayed failed, and how much change landed against it in the meantime. The
// three share one lifetime — a state transition ends all of them at once.
type stall struct {
	identity  string
	age       int
	mutations int
}

// OutcomeTracker is the shadow counterpart of ProgressTracker: same per-round
// receipts, scored by outcome instead of novelty. It never influences guard
// behavior — samples exist only for trajectory recording and offline analysis.
type OutcomeTracker struct {
	legacy       *ProgressTracker
	round        int
	readPaths    map[string]bool
	commands     map[string]bool
	failures     map[string]bool
	actions      map[string]bool
	verifySeen   map[string]bool
	verifyPass   map[string]bool
	mutatedBases map[string]bool
	debt         bool
	debtAge      int
	blind        int
	stall        stall
	// failedCheck is the verification identity that failed this round and
	// refailed says it had already failed, handed from scoreCommand to the
	// round tail where the streak is kept.
	failedCheck string
	refailed    bool
}

func NewOutcomeTracker() *OutcomeTracker {
	return &OutcomeTracker{
		legacy:       NewProgressTracker(),
		readPaths:    map[string]bool{},
		commands:     map[string]bool{},
		failures:     map[string]bool{},
		actions:      map[string]bool{},
		verifySeen:   map[string]bool{},
		verifyPass:   map[string]bool{},
		mutatedBases: map[string]bool{},
	}
}

// ScoreRound folds one round's receipts into the tracker and returns the
// round's outcome decomposition.
func (t *OutcomeTracker) ScoreRound(receipts []Receipt) hostaudit.OutcomeSample {
	if t == nil {
		return hostaudit.OutcomeSample{}
	}
	t.round++
	s := hostaudit.OutcomeSample{Round: t.round}
	t.failedCheck, t.refailed = "", false
	for _, r := range receipts {
		t.scoreReceipt(r, &s)
	}
	s.LegacyGain = t.legacy.ScoreRound(receipts)
	// Verification debt: a discriminating observation settles it; otherwise a
	// mutation opens it and every silent round ages it, mutation round included.
	if s.Discriminating > 0 {
		t.debt, t.debtAge, t.blind = false, 0, 0
	} else {
		if s.Churn > 0 {
			t.debt = true
			t.blind += s.Churn
		}
		if t.debt {
			t.debtAge++
		}
	}
	s.DebtAge = t.debtAge
	s.BlindMutations = t.blind
	t.trackStall(&s)
	return s
}

// trackStall follows the one check a turn is stuck on. A transition in either
// direction ends the streak because the check moved; a different check failing
// again starts a new one; everything in between is change that landed without
// moving anything.
func (t *OutcomeTracker) trackStall(s *hostaudit.OutcomeSample) {
	switch {
	case s.Objective > 0 || s.Regression > 0:
		t.stall = stall{}
	case t.failedCheck != "" && t.failedCheck != t.stall.identity:
		// Opened by the first failure, not the repeat: the change made in
		// between is the change that failed to move it, and it is already made
		// by the time the repeat proves so.
		t.stall = stall{identity: t.failedCheck}
	}
	if t.stall.identity == "" {
		return
	}
	if t.refailed {
		t.stall.age++
	}
	t.stall.mutations += s.Churn
	s.StallAge, s.StallMutations = t.stall.age, t.stall.mutations
}

// noteMutatedPaths remembers mutated file basenames so a later command that
// mentions one (running a repro script, a targeted test file) reads as a
// discriminating observation even when it is not delivery verification.
func (t *OutcomeTracker) noteMutatedPaths(paths []string) {
	for _, p := range paths {
		if base := path.Base(strings.ReplaceAll(p, "\\", "/")); len(base) >= 3 {
			t.mutatedBases[base] = true
		}
	}
}

func (t *OutcomeTracker) commandExercisesMutation(command string) bool {
	// Inspecting a mutated file (cat/grep/head) cannot falsify anything; only
	// a command that can execute it discriminates.
	if _, _, readOnly := shellsafe.CommandIsReadOnly(command); readOnly {
		return false
	}
	for base := range t.mutatedBases {
		if strings.Contains(command, base) {
			return true
		}
	}
	return false
}

func (t *OutcomeTracker) scoreReceipt(r Receipt, s *hostaudit.OutcomeSample) {
	if command := strings.TrimSpace(r.Command); command != "" {
		t.scoreCommand(command, r, s)
		return
	}
	switch {
	case r.Success && (r.Mutation || r.Write):
		// A mutation is a state transition, not proof of progress: it counts
		// as churn until a verification transition vouches for it.
		s.Churn++
		t.noteMutatedPaths(r.Paths)
	case r.Success && (r.ToolName == "task" || r.ToolName == "parallel_tasks" || r.ToolName == "fleet"):
		// A delegation return is new information at best — never objective
		// progress on its own.
		s.Exploration++
	case r.Success && (r.StepProof || r.TodoStep != nil || len(r.Todos) > 0):
		// Bookkeeping moves no outcome dimension.
	case r.Success && r.Read && r.OutputBytes > 0 && len(r.Paths) > 0:
		for _, path := range r.Paths {
			if path == "" || t.readPaths[path] {
				continue
			}
			t.readPaths[path] = true
			s.Exploration++
		}
	case r.Success:
		sig := r.ToolName + "\x00" + string(r.Args)
		if !t.actions[sig] {
			t.actions[sig] = true
			s.Exploration++
		}
	}
}

func (t *OutcomeTracker) scoreCommand(command string, r Receipt, s *hostaudit.OutcomeSample) {
	if r.Success && (r.Mutation || r.Write) {
		s.Churn++
		t.noteMutatedPaths(r.Paths)
	}
	verify := IsDeliveryVerificationCommand(command)
	if verify || t.commandExercisesMutation(command) {
		s.Discriminating++
	}
	if verify {
		s.Verification++
		// The receipt's own exit status is the pipeline's, so `go test … | head`
		// reports success while the suite failed. The host's classification is
		// what read the failing stage.
		key, passed := VerificationIdentity(command), verificationPassed(r)
		seen, wasPass := t.verifySeen[key], t.verifyPass[key]
		t.verifySeen[key] = true
		t.verifyPass[key] = passed
		if seen && passed && !wasPass {
			s.Objective++
		}
		if seen && !passed && wasPass {
			s.Regression++
		}
		if !passed {
			t.failedCheck = key
			if seen && !wasPass {
				s.Stall++
				t.refailed = true
			}
		}
	}
	if r.Success {
		if !verify && !t.commands[command] {
			s.Exploration++
		}
		t.commands[command] = true
		return
	}
	if !t.failures[command] {
		t.failures[command] = true
		s.Exploration++
	}
}
