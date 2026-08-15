// Package taskcontract is the convergence point for the host's task state:
// one Contract assembled purely from signals the runtime already produced —
// the taskintent classification, planner-gate features, plan acceptance
// criteria, and the evidence ledger's receipts. Building or updating a
// contract never makes a model call; every termination arbiter reads the
// same record instead of keeping its own.
package taskcontract

import (
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/taskintent"
)

// Risk is the highest risk any upstream signal assigned to the task.
type Risk uint8

const (
	RiskLow Risk = iota
	RiskMedium
	RiskHigh
)

// Status is a requirement's or check's lifecycle.
type Status uint8

const (
	Pending Status = iota
	Satisfied
	Failed
	// Stale marks a satisfaction whose proof predates the latest mutation:
	// it was true once, and must be re-proven against the current code.
	Stale
	// Suppressed marks a check that cannot run for a structured host reason
	// (user forbid, permission deny, dependency unavailable, reviewer unavailable).
	// Suppressed is never treated as Satisfied.
	Suppressed
)

// SuppressReason classifies why a check or requirement was suppressed.
type SuppressReason string

const (
	SuppressUserForbidden         SuppressReason = "user_forbidden"
	SuppressPermissionDenied      SuppressReason = "permission_denied"
	SuppressDependencyUnavailable SuppressReason = "dependency_unavailable"
	SuppressReviewerUnavailable   SuppressReason = "reviewer_unavailable"
)

// EvidenceKind classifies what a receipt proved.
type EvidenceKind uint8

const (
	EvidenceRead EvidenceKind = iota
	EvidenceMutation
	EvidenceVerification
	EvidenceReview
)

// EvidenceRef points at one ledger receipt without copying its content.
type EvidenceRef struct {
	Kind          EvidenceKind
	MutationEpoch uint64 // ledger sequence at attach time
	Source        string // tool name
	Success       bool
}

// Requirement is one acceptance criterion; Required=false records a
// nice-to-have that must not block completion. Auto requirements are
// satisfied by the first successful receipt of AutoKind — the opt-in for
// tasks whose ask IS the evidence (fix a typo: the mutation proves it).
type Requirement struct {
	ID       string
	Kind     string // optional taxonomy, e.g. "behavior", "regression"
	Text     string
	Required bool
	Status   Status
	Evidence []EvidenceRef
	Auto     bool
	AutoKind EvidenceKind
	// SuppressReason is set when Status is Suppressed.
	SuppressReason SuppressReason
}

// CheckKind selects what proves a check: a verification command, or any
// successful workspace mutation (scoped to Scope.Paths when set).
type CheckKind uint8

const (
	CheckCommand CheckKind = iota
	CheckMutation
)

// Check is an expected proof; for CheckCommand an empty Command accepts any
// verification-classified command.
type Check struct {
	Kind     CheckKind
	Command  string
	Status   Status
	Evidence []EvidenceRef
	// SuppressReason is set when Status is Suppressed.
	SuppressReason SuppressReason
}

// Scope is where the task is expected to act, from prompt-shape signals.
type Scope struct {
	Anchored     bool // names concrete files or targets
	MultiFile    bool
	CrossSurface bool
	Paths        []string
}

// Signals is plain data extracted by callers from machinery this package
// must not import (planner-gate features, delivery risk); merging them here
// keeps the contract the single record without inverting layering.
type Signals struct {
	HighRisk     bool
	MediumRisk   bool
	Anchored     bool
	MultiFile    bool
	CrossSurface bool
	Paths        []string
}

// Contract is the unified task record.
type Contract struct {
	Intent       taskintent.Intent
	Risk         Risk
	Scope        Scope
	Requirements []Requirement
	Checks       []Check

	epoch uint64
}

// New classifies input with the existing taskintent heuristics and returns
// an otherwise empty contract; no model call is made.
func New(input string) *Contract {
	return &Contract{Intent: taskintent.Classify(input)}
}

// Atomic is the zero-overhead contract for a simple ask: the ask itself is
// the one requirement (auto-satisfied by mutation evidence) and the one
// check is "the workspace changed". No planner, no evaluator, no extra
// round — the host synthesizes it and the ledger completes it.
func Atomic(input string) *Contract {
	c := New(input)
	c.Requirements = append(c.Requirements, Requirement{
		ID: "r1", Text: input, Required: true, Auto: true, AutoKind: EvidenceMutation,
	})
	c.Checks = append(c.Checks, Check{Kind: CheckMutation})
	return c
}

// PlanFacts is a completed full plan's contract-relevant output, extracted
// by the coordinator (plain data; this package never sees the plan text).
type PlanFacts struct {
	AcceptanceCriteria []PlanCriterion
	Regressions        []PlanCriterion // must-keep-passing criteria
	Optional           []PlanCriterion // nice-to-have; recorded but never blocking
	Verifications      []string        // command-level checks; "" entries mean any
	Risky              bool
	Touchpoints        []string
}

// PlanCriterion is one criterion with the identity the plan gave it. The id
// travels rather than being regenerated here: a proof cites the criterion the
// user approved, and a boundary that re-keys identity breaks that citation.
type PlanCriterion struct {
	ID   string
	Text string
}

// FromPlan builds the contract straight from a plan the planner already
// produced, so the executor consumes the plan's own acceptance criteria
// instead of re-deriving a parallel set: Planner → Contract → Executor.
func FromPlan(input string, facts PlanFacts) *Contract {
	c := New(input)
	add := func(criteria []PlanCriterion, kind, fallback string, required bool) {
		for i, criterion := range criteria {
			id := criterion.ID
			if id == "" {
				id = fmt.Sprintf("%s%d", fallback, i+1)
			}
			c.Requirements = append(c.Requirements, Requirement{
				ID: id, Kind: kind, Text: criterion.Text, Required: required,
			})
		}
	}
	add(facts.AcceptanceCriteria, "behavior", "r", true)
	add(facts.Regressions, "regression", "g", true)
	add(facts.Optional, "behavior", "o", false)
	for _, command := range facts.Verifications {
		c.AddCheck(command)
	}
	c.MergeSignals(Signals{
		MediumRisk: facts.Risky,
		Anchored:   len(facts.Touchpoints) > 0,
		MultiFile:  len(facts.Touchpoints) > 1,
		Paths:      facts.Touchpoints,
	})
	return c
}

// ExecutionView renders the contract as the executor's todo list — a view,
// not a parallel task description: requirements become steps, checks become
// verify steps, and satisfied entries arrive already completed.
func (c *Contract) ExecutionView() []evidence.TodoItem {
	var todos []evidence.TodoItem
	status := func(s Status) string {
		if s == Satisfied {
			return "completed"
		}
		return "pending"
	}
	for _, req := range c.Requirements {
		if !req.Required {
			continue
		}
		todos = append(todos, evidence.TodoItem{Content: req.Text, Status: status(req.Status)})
	}
	for _, check := range c.Checks {
		label := check.Command
		if label == "" {
			if check.Kind == CheckMutation {
				label = "apply the change"
			} else {
				label = "run verification"
			}
		} else {
			label = "verify: " + label
		}
		todos = append(todos, evidence.TodoItem{Content: label, Status: status(check.Status)})
	}
	return todos
}

// Trivial reports whether the contract is simple enough to route
// executor-only and skip every arbiter beyond the ledger itself.
func (c *Contract) Trivial() bool {
	return c.Risk == RiskLow &&
		!c.Scope.MultiFile && !c.Scope.CrossSurface &&
		len(c.Requirements) <= 1 && len(c.Checks) <= 1
}

// MergeSignals folds prompt-shape and risk signals in; risk only ratchets up.
func (c *Contract) MergeSignals(s Signals) {
	if s.HighRisk {
		c.Risk = RiskHigh
	} else if s.MediumRisk && c.Risk < RiskMedium {
		c.Risk = RiskMedium
	}
	c.Scope.Anchored = c.Scope.Anchored || s.Anchored
	c.Scope.MultiFile = c.Scope.MultiFile || s.MultiFile
	c.Scope.CrossSurface = c.Scope.CrossSurface || s.CrossSurface
	c.Scope.Paths = appendNew(c.Scope.Paths, s.Paths)
}

// AddRequirement records one acceptance criterion (e.g. a plan's acceptance
// criteria or a goal spec requirement). Duplicate IDs update the text.
func (c *Contract) AddRequirement(id, text string, required bool) {
	for i := range c.Requirements {
		if c.Requirements[i].ID == id {
			c.Requirements[i].Text = text
			c.Requirements[i].Required = required
			return
		}
	}
	c.Requirements = append(c.Requirements, Requirement{ID: id, Text: text, Required: required})
}

// AddCheck records an expected verification command ("" = any verification).
func (c *Contract) AddCheck(command string) {
	for _, check := range c.Checks {
		if check.Command == command {
			return
		}
	}
	c.Checks = append(c.Checks, Check{Command: command})
}

// Observe folds one ledger receipt into the contract. Mutations advance the
// epoch and stale every verification-backed satisfaction recorded before
// them — a test that passed against older code proves nothing about the
// current code. Mutation-kind evidence never stales: the change happened;
// whether the fix still holds is verification's job.
func (c *Contract) Observe(r evidence.Receipt) {
	if r.Mutation || r.Write {
		c.epoch++
		c.staleOutdated()
	}
	ref := refFor(c.epoch, r)
	for i := range c.Checks {
		if !c.checkMatches(c.Checks[i], r, ref) {
			continue
		}
		c.Checks[i].Evidence = append(c.Checks[i].Evidence, ref)
		if r.Success {
			c.Checks[i].Status = Satisfied
		} else if c.Checks[i].Status != Satisfied {
			c.Checks[i].Status = Failed
		}
	}
	for i := range c.Requirements {
		req := &c.Requirements[i]
		if req.Auto && req.Status != Satisfied && r.Success && ref.Kind == req.AutoKind {
			req.Status = Satisfied
			req.Evidence = append(req.Evidence, ref)
		}
	}
}

// staleOutdated demotes satisfactions whose entire proof is verification or
// review evidence from before the current epoch.
func (c *Contract) staleOutdated() {
	for i := range c.Checks {
		if c.Checks[i].Status == Satisfied && c.Checks[i].Kind == CheckCommand && allProofOutdated(c.Checks[i].Evidence, c.epoch) {
			c.Checks[i].Status = Stale
		}
	}
	for i := range c.Requirements {
		req := &c.Requirements[i]
		if req.Status == Satisfied && len(req.Evidence) > 0 && allProofOutdated(req.Evidence, c.epoch) {
			req.Status = Stale
		}
	}
}

// allProofOutdated reports whether every proof is stale-able (verification
// or review) and predates epoch; any current or mutation-kind ref keeps the
// satisfaction alive.
func allProofOutdated(refs []EvidenceRef, epoch uint64) bool {
	sawProof := false
	for _, ref := range refs {
		if !ref.Success {
			continue
		}
		sawProof = true
		if ref.Kind == EvidenceMutation || ref.MutationEpoch >= epoch {
			return false
		}
	}
	return sawProof
}

// Resolve sets a requirement's status with the evidence that justified it.
func (c *Contract) Resolve(id string, status Status, refs ...EvidenceRef) bool {
	for i := range c.Requirements {
		if c.Requirements[i].ID == id {
			c.Requirements[i].Status = status
			c.Requirements[i].Evidence = append(c.Requirements[i].Evidence, refs...)
			return true
		}
	}
	return false
}

// Epoch is the mutation epoch: how many workspace mutations the contract
// has observed. Reads and verifications never advance it.
func (c *Contract) Epoch() uint64 { return c.epoch }

// Complete reports whether every required requirement and every check is
// satisfied — the one answer the termination arbiters share.
func (c *Contract) Complete() bool {
	for _, req := range c.Requirements {
		if req.Required && req.Status != Satisfied {
			return false
		}
	}
	for _, check := range c.Checks {
		if check.Status != Satisfied {
			return false
		}
	}
	return true
}

// ReadyToFinalize reports whether the host should signal the model to stop:
// the contract has real content and every bit of it is satisfied with fresh
// evidence. Stale semantics make "latest mutation verified" implicit — an
// unverified mutation leaves a verification check Stale, blocking this.
func (c *Contract) ReadyToFinalize() bool {
	return (len(c.Requirements) > 0 || len(c.Checks) > 0) && c.Complete()
}

// FinalizeSignal is the light host nudge injected once the contract is
// fully proven; empty while anything is outstanding. The wording leaves the
// model one exit: concrete evidence of an unresolved requirement.
func (c *Contract) FinalizeSignal() string {
	if !c.ReadyToFinalize() {
		return ""
	}
	required := 0
	for _, req := range c.Requirements {
		if req.Required {
			required++
		}
	}
	return fmt.Sprintf(
		"All required acceptance evidence is satisfied (%d/%d requirements, %d/%d checks, latest mutation verified). Finalize now unless you have concrete evidence of an unresolved requirement.",
		required, required, len(c.Checks), len(c.Checks))
}

// Verdict is the deterministic goal outcome the contract can decide without
// a model call; Uncertain is the only case that still needs the bounded LLM
// evaluator — it becomes the fallback, not the completion hot path.
type Verdict uint8

const (
	VerdictUncertain Verdict = iota
	VerdictContinue
	VerdictBlocked
	VerdictComplete
	// VerdictPartial means mutations may be kept but verification or review
	// was forbidden or unavailable. Goals must not auto-complete on Partial.
	VerdictPartial
)

func (v Verdict) String() string {
	switch v {
	case VerdictContinue:
		return "continue"
	case VerdictBlocked:
		return "blocked"
	case VerdictComplete:
		return "complete"
	case VerdictPartial:
		return "partial"
	default:
		return "uncertain"
	}
}

// GoalVerdict decides the goal outcome from the evidence graph alone.
// Missing or stale evidence means Continue (the next action is knowable);
// a requirement explicitly resolved Failed — an arbiter's judgment that it
// cannot be met — means Blocked; everything fresh-satisfied means Complete.
// Suppressed required evidence yields Partial (never Complete).
// Only a contract with nothing to prove returns Uncertain.
func (c *Contract) GoalVerdict() Verdict {
	if len(c.Requirements) == 0 && len(c.Checks) == 0 {
		return VerdictUncertain
	}
	missing, blocked, partial := false, false, false
	for _, req := range c.Requirements {
		if !req.Required {
			continue
		}
		switch req.Status {
		case Failed:
			blocked = true
		case Suppressed:
			partial = true
		case Pending, Stale:
			missing = true
		}
	}
	for _, check := range c.Checks {
		switch check.Status {
		case Satisfied:
			// ok
		case Suppressed:
			partial = true
		default:
			// A failed check run is actionable — fix it and re-run — so it
			// keeps the goal in Continue, never Blocked.
			missing = true
		}
	}
	switch {
	case missing:
		return VerdictContinue
	case blocked:
		return VerdictBlocked
	case partial:
		return VerdictPartial
	case c.Complete():
		return VerdictComplete
	}
	return VerdictUncertain
}

// SuppressCheck marks the first matching check (by command; empty matches any
// unsatisfied command check) as Suppressed with a structured reason.
func (c *Contract) SuppressCheck(command string, reason SuppressReason) bool {
	if c == nil {
		return false
	}
	for i := range c.Checks {
		if command != "" && c.Checks[i].Command != command {
			continue
		}
		if c.Checks[i].Status == Satisfied {
			continue
		}
		c.Checks[i].Status = Suppressed
		c.Checks[i].SuppressReason = reason
		return true
	}
	return false
}

// SuppressRequirement marks a requirement as Suppressed with a structured reason.
func (c *Contract) SuppressRequirement(id string, reason SuppressReason) bool {
	if c == nil {
		return false
	}
	for i := range c.Requirements {
		if c.Requirements[i].ID != id {
			continue
		}
		c.Requirements[i].Status = Suppressed
		c.Requirements[i].SuppressReason = reason
		return true
	}
	return false
}

// HasSuppressed reports whether any required requirement or check is Suppressed.
func (c *Contract) HasSuppressed() bool {
	if c == nil {
		return false
	}
	for _, req := range c.Requirements {
		if req.Required && req.Status == Suppressed {
			return true
		}
	}
	for _, check := range c.Checks {
		if check.Status == Suppressed {
			return true
		}
	}
	return false
}

// Complete reports whether every required requirement and every check is
// satisfied — Suppressed never counts as complete.
func (c *Contract) CompleteStrict() bool {
	return c.Complete() && !c.HasSuppressed()
}

// GateMutation is the mutation-after-green guard: while anything is
// unproven mutations flow freely, but once the contract is fully proven a
// further mutation must bind to a still-unsatisfied requirement ID or the
// challenge comes back as the tool result — no extra round. The gate lifts
// by itself after a landed mutation, since the staled proof ends green.
func (c *Contract) GateMutation(requirementID string) (allowed bool, challenge string) {
	if !c.ReadyToFinalize() {
		return true, ""
	}
	if requirementID != "" {
		for _, req := range c.Requirements {
			if req.ID == requirementID && req.Status != Satisfied {
				return true, ""
			}
		}
	}
	return false, "All acceptance evidence is satisfied. Name the unsatisfied requirement ID this mutation addresses, or finalize instead of editing."
}

// FinalRejection is the host's answer to a premature "done": empty when the
// contract is fully proven (or has no content to prove), otherwise a
// directive naming exactly what lacks fresh evidence — "verify R2" carries
// more information than any generic ask-a-reviewer round.
func (c *Contract) FinalRejection() string {
	if len(c.Requirements) == 0 && len(c.Checks) == 0 {
		return ""
	}
	if c.Complete() {
		return ""
	}
	var out strings.Builder
	out.WriteString("Final not accepted: the contract is not fully proven.\n")
	for _, req := range c.Requirements {
		if !req.Required || req.Status == Satisfied {
			continue
		}
		fmt.Fprintf(&out, "- requirement %s (%s): %s\n", req.ID, req.Text, directiveFor(req.Status, "verify"))
	}
	for _, check := range c.Checks {
		if check.Status == Satisfied {
			continue
		}
		label := check.Command
		if label == "" {
			label = "verification"
			if check.Kind == CheckMutation {
				label = "the required change"
			}
		}
		fmt.Fprintf(&out, "- check %s: %s\n", label, directiveFor(check.Status, "run"))
	}
	return out.String()
}

func directiveFor(status Status, verb string) string {
	switch status {
	case Stale:
		return "evidence predates the latest mutation — re-" + verb + " it"
	case Failed:
		return "last evidence shows failure — fix it, then re-" + verb
	case Suppressed:
		return "suppressed by host constraint — not counted as passed"
	default:
		return "no fresh evidence — " + verb + " it"
	}
}

// Summary is the one-line contract status for logs and host notes.
func (c *Contract) Summary() string {
	reqDone, reqAll := 0, 0
	for _, req := range c.Requirements {
		if !req.Required {
			continue
		}
		reqAll++
		if req.Status == Satisfied {
			reqDone++
		}
	}
	checkDone, stale := 0, 0
	for _, check := range c.Checks {
		switch check.Status {
		case Satisfied:
			checkDone++
		case Stale:
			stale++
		}
	}
	s := fmt.Sprintf("requirements %d/%d · checks %d/%d · epoch %d", reqDone, reqAll, checkDone, len(c.Checks), c.epoch)
	if stale > 0 {
		s += fmt.Sprintf(" · stale %d", stale)
	}
	return s
}

// Outstanding lists what still blocks completion, requirements first.
func (c *Contract) Outstanding() []string {
	var out []string
	for _, req := range c.Requirements {
		if req.Required && req.Status != Satisfied {
			// Stale is said out loud here as it already is for checks: "verify
			// it" and "re-verify it because the code moved" are different
			// instructions, and only one of them is actionable after a change.
			entry := fmt.Sprintf("requirement %s: %s", req.ID, req.Text)
			if req.Status == Stale {
				entry += " (stale: re-verify after the latest mutation)"
			}
			out = append(out, entry)
		}
	}
	for _, check := range c.Checks {
		if check.Status != Satisfied {
			label := check.Command
			if label == "" {
				label = "any verification"
			}
			if check.Status == Stale {
				label += " (stale: re-verify after the latest mutation)"
			}
			out = append(out, "check: "+label)
		}
	}
	return out
}

// Graph renders the evidence graph: each requirement and check with the
// proofs attached to it, staleness made visible.
func (c *Contract) Graph() string {
	var b []byte
	statusName := map[Status]string{Pending: "pending", Satisfied: "satisfied", Failed: "failed", Stale: "STALE"}
	kindName := map[EvidenceKind]string{EvidenceRead: "read", EvidenceMutation: "mutation", EvidenceVerification: "verification", EvidenceReview: "review"}
	node := func(label string, status Status, refs []EvidenceRef) {
		b = fmt.Appendf(b, "%s [%s]\n", label, statusName[status])
		for i, ref := range refs {
			branch := "├──"
			if i == len(refs)-1 {
				branch = "└──"
			}
			mark := ""
			if ref.Kind != EvidenceMutation && ref.MutationEpoch < c.epoch {
				mark = " (stale)"
			}
			b = fmt.Appendf(b, " %s E%d %s@%d %s success=%v%s\n", branch, i+1, kindName[ref.Kind], ref.MutationEpoch, ref.Source, ref.Success, mark)
		}
	}
	for _, req := range c.Requirements {
		node(req.ID+" "+req.Text, req.Status, req.Evidence)
	}
	for _, check := range c.Checks {
		label := "check " + check.Command
		if check.Command == "" {
			label = "check (any verification)"
			if check.Kind == CheckMutation {
				label = "check (mutation)"
			}
		}
		node(label, check.Status, check.Evidence)
	}
	return string(b)
}

func refFor(epoch uint64, r evidence.Receipt) EvidenceRef {
	kind := EvidenceRead
	switch {
	case r.ToolName == "review_report":
		kind = EvidenceReview
	case r.Command != "" && evidence.IsVerificationCommand(r.Command):
		kind = EvidenceVerification
	case r.Mutation || r.Write:
		kind = EvidenceMutation
	}
	return EvidenceRef{Kind: kind, MutationEpoch: epoch, Source: r.ToolName, Success: r.Success}
}

func (c *Contract) checkMatches(check Check, r evidence.Receipt, ref EvidenceRef) bool {
	if check.Kind == CheckMutation {
		if ref.Kind != EvidenceMutation {
			return false
		}
		if len(c.Scope.Paths) == 0 {
			return true
		}
		return pathsIntersect(c.Scope.Paths, r.Paths)
	}
	if r.Command == "" {
		return false
	}
	if check.Command == "" {
		return evidence.IsVerificationCommand(r.Command)
	}
	return evidence.CommandMatches(check.Command, r.Command)
}

func pathsIntersect(scope, got []string) bool {
	for _, s := range scope {
		if slices.Contains(got, s) {
			return true
		}
	}
	return false
}

func appendNew(dst, add []string) []string {
	seen := make(map[string]bool, len(dst))
	for _, p := range dst {
		seen[p] = true
	}
	for _, p := range add {
		if !seen[p] {
			seen[p] = true
			dst = append(dst, p)
		}
	}
	return dst
}
