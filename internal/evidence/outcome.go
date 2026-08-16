package evidence

import (
	"path"
	"sort"
	"strings"

	"reasonix/internal/shellsafe"
)

// OutcomeSample decomposes one tool round's receipts by outcome: information
// gathered (Exploration), verification attempts run, and verification-command
// state transitions (Objective fail→pass, Regression pass→fail). Counts are
// unit-weighted; policy weighting is an offline concern.
type OutcomeSample struct {
	Round        int
	Exploration  int
	Verification int
	Objective    int
	Regression   int
	Churn        int
	// LegacyGain is the live novelty scorer's verdict on the same receipts, so
	// offline analysis can compare the two policies without replaying.
	LegacyGain int
	// Discriminating counts observations able to falsify the working
	// hypothesis: verification commands, or commands exercising a mutated
	// file — deliberately broader than delivery verification (repro scripts).
	Discriminating int
	// DebtAge counts consecutive rounds carrying an unverified mutation with
	// no discriminating observation; 0 while no verification debt is open.
	DebtAge int
	// BlindMutations counts mutations since the last discriminating
	// observation — the EBM policy's trigger input.
	BlindMutations int
	// EBMEligible/EBMFired mark the Evidence-Before-More-Mutation trigger
	// holding and its nudge firing; the agent stamps both so every arm —
	// baseline included — carries the eligibility shadow.
	EBMEligible bool
	EBMFired    bool
	// LocalExecSeen reports whether this turn has executed any local
	// interpreter/test command yet — the self-check-propensity observable
	// (studied set: python/node/go run/pytest; ecosystem bias documented).
	LocalExecSeen bool
	// GovernorEligible/GovernorEngaged mark the reasoning governor's
	// exploration trigger holding and its depth override riding requests;
	// eligibility is stamped on every arm so baselines carry the shadow.
	GovernorEligible bool
	GovernorEngaged  bool
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
	localExec    bool
}

// OutcomeSeed is the fork-portable slice of tracker state: what a
// counterfactual continuation must inherit for its shadow to stay continuous.
type OutcomeSeed struct {
	MutatedBases   []string `json:"mutated_bases,omitempty"`
	DebtAge        int      `json:"debt_age"`
	BlindMutations int      `json:"blind_mutations"`
	LocalExecSeen  bool     `json:"local_exec_seen"`
}

// ForkSeed exports the state a counterfactual fork must carry so post-fork
// discriminating detection stays continuous with the original run.
func (t *OutcomeTracker) ForkSeed() OutcomeSeed {
	seed := OutcomeSeed{DebtAge: t.debtAge, BlindMutations: t.blind, LocalExecSeen: t.localExec}
	for base := range t.mutatedBases {
		seed.MutatedBases = append(seed.MutatedBases, base)
	}
	sort.Strings(seed.MutatedBases)
	return seed
}

// RestoreOutcomeTracker rebuilds a tracker from a fork seed. Novelty maps
// start empty — post-fork exploration novelty is intentionally relative to the
// fork point, while debt state continues from the original trajectory.
func RestoreOutcomeTracker(seed OutcomeSeed) *OutcomeTracker {
	t := NewOutcomeTracker()
	for _, base := range seed.MutatedBases {
		t.mutatedBases[base] = true
	}
	t.debtAge = seed.DebtAge
	t.blind = seed.BlindMutations
	t.debt = seed.DebtAge > 0 || seed.BlindMutations > 0
	t.localExec = seed.LocalExecSeen
	return t
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
func (t *OutcomeTracker) ScoreRound(receipts []Receipt) OutcomeSample {
	if t == nil {
		return OutcomeSample{}
	}
	t.round++
	s := OutcomeSample{Round: t.round}
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
	s.LocalExecSeen = t.localExec
	return s
}

// localExecCommand matches the exact command families the affordance study
// validated. Deliberately narrow and Python-ecosystem biased for now;
// generalizing to Local Discriminating Execution needs cross-language
// replication first.
func localExecCommand(command string) bool {
	for _, marker := range []string{"python", "node ", "go run", "pytest", "py.test"} {
		if strings.Contains(command, marker) {
			return true
		}
	}
	return false
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
	if !shellsafe.ClassifyBash(command).AnyMutation() {
		return false
	}
	for base := range t.mutatedBases {
		if strings.Contains(command, base) {
			return true
		}
	}
	return false
}

func (t *OutcomeTracker) scoreReceipt(r Receipt, s *OutcomeSample) {
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
		fresh := 0
		for _, path := range r.Paths {
			if path == "" || t.readPaths[path] {
				continue
			}
			t.readPaths[path] = true
			fresh++
		}
		// A path already read can still answer a question never asked: a new
		// grep pattern over the same package, the next window of a long file.
		if newQuestion := t.noteQuestion(r); fresh == 0 && newQuestion {
			fresh = 1
		}
		s.Exploration += fresh
	case r.Success:
		if t.noteQuestion(r) {
			s.Exploration++
		}
	}
}

func (t *OutcomeTracker) noteQuestion(r Receipt) bool {
	sig := r.ToolName + "\x00" + string(r.Args)
	if t.actions[sig] {
		return false
	}
	t.actions[sig] = true
	return true
}

func (t *OutcomeTracker) scoreCommand(command string, r Receipt, s *OutcomeSample) {
	if r.Success && (r.Mutation || r.Write) {
		s.Churn++
		t.noteMutatedPaths(r.Paths)
	}
	verify := IsVerificationCommand(command)
	if verify || t.commandExercisesMutation(command) {
		s.Discriminating++
	}
	if localExecCommand(command) {
		t.localExec = true
	}
	if verify {
		s.Verification++
		seen, wasPass := t.verifySeen[command], t.verifyPass[command]
		t.verifySeen[command] = true
		t.verifyPass[command] = r.Success
		if seen && r.Success && !wasPass {
			s.Objective++
		}
		if seen && !r.Success && wasPass {
			s.Regression++
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
