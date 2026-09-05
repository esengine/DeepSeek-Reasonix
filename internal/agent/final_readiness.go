package agent

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/ablation"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/instruction"
	"reasonix/internal/taskpolicy"
)

// Final readiness: whether a turn has earned the right to stop. It reads the
// evidence ledger, the delivery profile, and the approved plan's contract, and
// says what is missing rather than merely that something is.

type finalReadinessCheck struct {
	applies                   bool
	reason                    string
	missingProjectChecks      int
	incompleteTodos           int
	missingAcceptanceCriteria int
	missingVerification       int
	missingStructuredReview   int
	missingPathInspection     int
	missingSignoff            int
	missingMutation           int
	missingCapabilities       int
}

func (c finalReadinessCheck) progressSignature() string {
	return fmt.Sprintf("%d/%d/%d/%d/%d/%d/%d/%d/%d/%d\x00%s",
		c.missingProjectChecks,
		c.incompleteTodos,
		c.missingAcceptanceCriteria,
		c.missingVerification,
		c.missingStructuredReview,
		c.missingPathInspection,
		c.missingSignoff,
		c.missingMutation,
		c.missingCapabilities,
		boolInt(c.applies),
		c.reason,
	)
}

func (c finalReadinessCheck) missingIDs() []string {
	missing := make([]string, 0, 9)
	add := func(id string, count int) {
		if count > 0 {
			missing = append(missing, id)
		}
	}
	add("project_check", c.missingProjectChecks)
	add("todo", c.incompleteTodos)
	add("criteria", c.missingAcceptanceCriteria)
	add("verification", c.missingVerification)
	add("structured_review", c.missingStructuredReview)
	add("path_inspection", c.missingPathInspection)
	add("signoff", c.missingSignoff)
	add("mutation", c.missingMutation)
	add("capability", c.missingCapabilities)
	return missing
}

func (c finalReadinessCheck) audit(result evidence.ReadinessAuditResult, recovered bool) evidence.ReadinessAudit {
	return evidence.ReadinessAudit{
		Result:                    result,
		Recovered:                 recovered,
		MissingProjectChecks:      c.missingProjectChecks,
		IncompleteTodos:           c.incompleteTodos,
		MissingAcceptanceCriteria: c.missingAcceptanceCriteria,
		MissingVerification:       c.missingVerification,
		MissingStructuredReview:   c.missingStructuredReview,
		MissingPathInspection:     c.missingPathInspection,
		MissingSignoff:            c.missingSignoff,
		MissingMutation:           c.missingMutation,
		MissingCapabilities:       c.missingCapabilities,
	}
}

// declaredChecksRanAfter reports whether the project's own declared checks all
// ran after the latest write. A project that names its checks has defined what
// verification means there, so the host's command classifier — which cannot
// tell `python3 test_x.py` from `python3 deploy.py` — does not get a second
// say. Projects that declare nothing keep the classifier as the only floor.
func (a *Agent) declaredChecksRanAfter(writer int) bool {
	if len(a.projectChecks) == 0 {
		return false
	}
	for _, check := range a.projectChecks {
		command := strings.TrimSpace(check.Command)
		if command == "" {
			continue
		}
		if !a.task.ledger.HasSuccessfulCommandAfter(command, writer) {
			return false
		}
	}
	return true
}

// unmetProjectChecks lists the declared checks that have not run since the
// latest write, phrased as the instruction that would settle each one.
func (a *Agent) unmetProjectChecks(writer int) []string {
	var gaps []string
	for _, check := range a.projectChecks {
		command := strings.TrimSpace(check.Command)
		if command == "" {
			continue
		}
		if !a.task.ledger.HasSuccessfulCommandAfter(command, writer) {
			gaps = append(gaps, fmt.Sprintf("run %q from %s after the latest write", command, finalReadinessCheckSource(check)))
		}
	}
	return gaps
}

func (a *Agent) finalReadinessCheckFor() finalReadinessCheck {
	if a.task.ledger == nil || a.ablation.Off(ablation.Evidence) {
		return finalReadinessCheck{}
	}
	var missing []string
	out := finalReadinessCheck{}
	// Planning returns a proposal; the controller owns approval and starts a
	// fresh execution turn, which is where delivery requirements belong. A
	// workflow boundary only — tool calls still take the usual permission path.
	if a.planningPhase() {
		return out
	}
	missing = a.appendIncompleteTodoGap(&out, missing)
	writer, hasWriter := a.mutationBaseline(false)
	deliveryMutation := false
	deliveryVerificationOnly := false
	checkpoint := a.task.checkpoint
	checkpointApplies := a.turn.deliveryScopeActive && checkpoint.ScopeID == a.task.scopeID
	if a.deliveryProfile {
		if mutation, ok := a.mutationBaseline(true); ok {
			writer, hasWriter = mutation, true
			deliveryMutation = true
		} else if checkpointApplies && checkpoint.PendingMutation {
			// The mutation happened before a controller rebuild/restart. Treat it as
			// the baseline so this run can satisfy verification/review/sign-off
			// without manufacturing another write.
			writer, hasWriter = -1, true
			deliveryMutation = true
		} else if checkpointApplies && checkpoint.MutationObserved {
			deliveryMutation = true
		}
		// What a turn owes is read off the ledger, never off the task text: one
		// that changed nothing owes nothing, one that did owes the verification,
		// review, and sign-off below.
		if !hasWriter && a.task.ledger.HasSuccessfulVerificationCommand() {
			writer, hasWriter = -1, true
			deliveryVerificationOnly = true
		}
		// Required/preferred capability gates apply before the no-writer fast
		// path below: a user-required Skill/MCP must not be skippable by
		// answering from ordinary reads alone.
		if msg := a.capabilityGateFailure(); msg != "" {
			out.applies = true
			out.missingCapabilities++
			missing = append(missing, msg)
		}
		if a.task.ledger.HasSuccessfulToolReceipt("remember") && !a.task.ledger.HasSuccessfulMutationOtherThan("remember") {
			// A turn whose only mutation was a memory write has nothing a test or
			// a diff could add. Any unrelated mutation falls through to the full
			// contract below.
			out.applies = true
			if len(missing) > 0 {
				out.reason = strings.Join(missing, "; ")
			}
			return out
		}
	}
	if !hasWriter {
		if len(missing) > 0 {
			out.reason = strings.Join(missing, "; ")
		}
		return out
	}
	// A turn declared blocked still owes the check that establishes the blocker;
	// what it cannot owe is a passing one, since the task being impossible is
	// exactly why the check does not pass. Nothing else about the turn is waived.
	verified, blockedWithCheck := a.postWriteVerification(writer)
	missing = a.appendVerificationGap(&out, missing, writer, blockedWithCheck, verified)
	missing = a.appendReviewGap(&out, missing)
	missing = a.appendUnprovenMutationGap(&out, missing)
	if a.turnHasNothingToAnswerFor(out, missing) {
		return finalReadinessCheck{}
	}
	out.applies = true
	if a.deliveryProfile {
		a.emitTurnPhase(event.TurnPhaseVerifying)
		criteriaEstablished := a.turn.deliveryCriteriaEstablished || (checkpointApplies && checkpoint.CriteriaEstablished)
		if !criteriaEstablished {
			out.missingAcceptanceCriteria++
			missing = append(missing, "establish concrete acceptance criteria with todo_write before changing state")
		}
		hasCompleteStep := a.task.ledger.HasSuccessfulCompleteStepAfter(writer)
		if !hasCompleteStep {
			out.missingSignoff++
			missing = append(missing, "call complete_step after the latest mutation")
		}
		// A sign-off that cited a passing check but lacks the review is missing
		// the review, not the verification. Reporting both sends the model to
		// re-run and re-cite a check it already ran, cited, and watched pass.
		if !a.task.ledger.HasSuccessfulDeliverySignoffAfter(writer) && !blockedWithCheck &&
			!a.task.ledger.HasCitedVerificationAfter(writer) {
			out.missingVerification++
			missing = append(missing, "run relevant verification after the latest mutation and cite that successful command in complete_step")
		}
		if deliveryMutation {
			missing = a.appendSelfInspectionGap(&out, missing, writer)
		}
		// The capability gate already ran before the no-writer fast path above.
	}
	if !deliveryVerificationOnly {
		missing = a.appendProjectCheckGaps(&out, missing, writer)
	}

	outstanding := a.outstandingPlanCriteria()
	out.missingAcceptanceCriteria += len(outstanding)
	missing = append(missing, outstanding...)
	if len(missing) == 0 {
		return out
	}
	out.reason = strings.Join(missing, "; ")
	return out
}

// appendIncompleteTodoGap records a list the turn left open. It needs a progress
// receipt to fire: a list nothing ever acted on says what the turn planned, not
// what it owes.
func (a *Agent) appendIncompleteTodoGap(out *finalReadinessCheck, missing []string) []string {
	incomplete, hasTodos := a.task.ledger.IncompleteLatestTodos()
	if !hasTodos && a.task.ledger.HasAnySuccessfulReceipt() {
		incomplete, hasTodos = a.incompleteCanonicalTodos()
	}
	if !hasTodos || len(incomplete) == 0 || !a.task.ledger.HasSuccessfulTodoProgressReceipt() {
		return missing
	}
	out.applies = true
	out.incompleteTodos = len(incomplete)
	return append(missing, finalReadinessIncompleteTodos(incomplete))
}

// appendProjectCheckGaps records the project's own declared checks that have
// not run since the latest write, and shadows the same question against the
// ledger's obligations without letting the answer reach the gate.
func (a *Agent) appendProjectCheckGaps(out *finalReadinessCheck, missing []string, writer int) []string {
	gaps := a.unmetProjectChecks(writer)
	out.missingProjectChecks += len(gaps)
	a.probeProjectChecks(len(gaps), writer)
	return append(missing, gaps...)
}

// appendSelfInspectionGap requires the turn to have looked at every file it
// changed. Reading one of them was the old floor, and it is the floor that let
// a five-file change ship on one file's worth of attention.
func (a *Agent) appendSelfInspectionGap(out *finalReadinessCheck, missing []string, writer int) []string {
	paths := productionPaths(a.task.ledger.PathsSince(writer))
	if len(paths) == 0 {
		// An opaque change names nothing to require, so the most that can
		// honestly be asked is that the turn looked at something.
		if a.task.ledger.HasSuccessfulReviewAfter(writer) {
			return missing
		}
		out.missingPathInspection++
		return append(missing, "inspect the changed result after the latest mutation — read the touched file, or diff it with whatever version control this workspace has")
	}
	gaps := a.task.ledger.UninspectedWritePaths(paths)
	if len(gaps) == 0 {
		return missing
	}
	out.missingPathInspection++
	slash := make([]string, 0, len(gaps))
	for _, p := range gaps {
		slash = append(slash, filepath.ToSlash(p))
	}
	return append(missing, "inspect every file this turn changed; not looked at since it was written: "+strings.Join(slash, ", "))
}

// appendReviewGap records what this turn owes for structured review. It runs
// ahead of the role-setting split: how much review a change owes is the
// delivery contract's to demand, but one that ran and refused is honored
// wherever it happened. reviewGateFailure keeps those two apart.
func (a *Agent) appendReviewGap(out *finalReadinessCheck, missing []string) []string {
	msg := a.reviewGateFailure()
	if msg == "" {
		return missing
	}
	out.applies = true
	out.missingStructuredReview++
	return append(missing, msg)
}

// checkEstablished reports that something after the latest write stands as its
// check: one the table recognised, a project's declared one, or one a
// completion named and the ledger corroborated.
func (a *Agent) checkEstablished(writer int, verified bool) bool {
	return verified ||
		a.declaredChecksRanAfter(writer) ||
		a.task.ledger.HasCorroboratedCitedCheckAfter(writer)
}

// postWriteVerification reads what the checks after the latest write establish.
// verified needs a passing check with none of them still standing failed — one
// check passing cannot answer for another that did not. blockedWithCheck is the
// declared-impossible case, which still owes the run that establishes it.
func (a *Agent) postWriteVerification(writer int) (verified, blockedWithCheck bool) {
	ledger := a.task.ledger
	verified = ledger.HasSuccessfulVerificationCommandAfter(writer) &&
		!ledger.HasFailedVerificationAfter(writer)
	blockedWithCheck = ledger.HasBlockedConclusionAfter(writer) &&
		ledger.HasVerificationCommandAfter(writer)
	return verified, blockedWithCheck
}

// obligations asks the ledger what it owes. Readiness and the delta the model
// is handed after each call both read this one derivation, so what blocks a
// turn is what the model was already told it was carrying.
func (a *Agent) obligations() []evidence.Obligation {
	owed := a.task.ledger.Obligations(a.checkContract())
	return append(owed, evidence.BaselineTestObligations(a.baselineFacts(), a.mutationEpoch())...)
}

// mutationEpoch is what a baseline result is bound to: the host's count of the
// mutations it observed, the same epoch ordinary verification answers under.
func (a *Agent) mutationEpoch() uint64 {
	if a.task.ledger == nil {
		return 0
	}
	at, ok := a.task.ledger.LatestSuccessfulMutationIndex()
	if !ok {
		return 0
	}
	return uint64(at) + 1
}

// checkContract pairs what the task began requiring with what the workspace
// declares now. The baseline comes from the checkpoint, never from the files:
// re-reading them is how a rewritten declaration would come to speak for the
// requirement it replaced.
func (a *Agent) checkContract() evidence.CheckContract {
	return evidence.CaptureCheckContract(a.task.checkpoint.BaselineChecks, a.declaredChecks())
}

// DeclaredProjectChecks is the declaration this process loaded, for a host that
// has to freeze what a Goal is accepted under. It is what the project asks for
// now, which is not the same claim as what a running Goal is held to.
func (a *Agent) DeclaredProjectChecks() []string {
	if a == nil {
		return nil
	}
	return a.declaredChecks()
}

// declaredChecks is what the project asks for right now, which the contract
// pairs against the baseline rather than trusting on its own.
func (a *Agent) declaredChecks() []string {
	current := make([]string, 0, len(a.projectChecks))
	for _, check := range a.projectChecks {
		current = append(current, check.Command)
	}
	return current
}

// captureBaselineChecks fixes what this task is held to, and never recaptures:
// after it the project may declare what it likes. It runs once at the turn's
// start, where nothing else is running — asking for it lazily made the capture
// a write that every parallel tool call reached at the moment it finished.
func (a *Agent) captureBaselineChecks() {
	if len(a.task.checkpoint.BaselineChecks) == 0 {
		a.task.checkpoint.BaselineChecks = evidence.CaptureCheckContract(a.declaredChecks(), nil).Baseline()
	}
}

// turnHasNothingToAnswerFor reports the turn no gate has a subject in: nothing
// the project declared, no list left open, nothing owed. Outside Delivery that
// is a turn to let go, since every remaining requirement is one nobody asked for.
func (a *Agent) turnHasNothingToAnswerFor(out finalReadinessCheck, missing []string) bool {
	return !a.deliveryProfile && len(a.projectChecks) == 0 &&
		!a.task.ledger.HasSuccessfulTodoWrite() && len(missing) == 0 && out.missingMutation == 0
}

// appendUnprovenMutationGap records the debt an unprovable change leaves. The
// host cannot say what it touched, so every check from before it is spent and
// only one that ran after can answer. The debt is recorded for every preset —
// it is the host's, not the model's — but only Delivery may not finish owing it.
func (a *Agent) appendUnprovenMutationGap(out *finalReadinessCheck, missing []string) []string {
	if !slices.ContainsFunc(a.obligations(), func(o evidence.Obligation) bool {
		return o.Kind == evidence.ObligationUnprovenMutation
	}) {
		return missing
	}
	out.applies = true
	out.missingMutation++
	if !a.deliveryProfile {
		return missing
	}
	return append(missing, "a change ran whose extent the host could not establish, and no check can establish it: "+
		"make the change again through a tool that reports what it touched, or call conclude_blocked with what cannot be established")
}

// appendVerificationGap records what this turn owes for verification. Only what
// the turn itself failed to do fails the turn: where the host cannot read what
// ran, it declines to judge rather than guessing.
func (a *Agent) appendVerificationGap(out *finalReadinessCheck, missing []string, writer int, blockedWithCheck, verified bool) []string {
	if a.deliveryProfile || !a.turn.policySet || a.turn.policy.Verification < taskpolicy.VerifyTargeted ||
		!toolPresent(a.svc.tools, "bash") || blockedWithCheck || a.checkEstablished(writer, verified) {
		return missing
	}
	gap, owed := a.verificationGap(writer)
	if !owed {
		return missing
	}
	out.applies = true
	out.missingVerification++
	return append(missing, gap)
}

// verificationGap says what the turn owes for verification, and whether it owes
// anything at all. A check whose exit status belonged to a later stage of the
// same command did run, so asking for "a verification command" reads as false
// to the model that just ran one; what it needs is the command named and a
// shape whose status answers for the check.
func (a *Agent) verificationGap(writer int) (string, bool) {
	const ask = "run a relevant verification command after the latest write for the current role setting"
	if unreadable, ok := a.task.ledger.LatestUnreadableVerificationAfter(writer); ok && strings.TrimSpace(unreadable.Command) != "" {
		return fmt.Sprintf("%s — %q ran, but its exit status is the last stage's, not the check's, "+
			"so it proves nothing either way; re-run the check on its own", ask, strings.TrimSpace(unreadable.Command)), true
	}
	// A check that ran and failed leaves two honest ways out, and a model told
	// only to "run a verification command" can see neither: it already ran one.
	if a.task.ledger.HasVerificationCommandAfter(writer) && toolPresent(a.svc.tools, "conclude_blocked") {
		return "the check you ran after the latest write did not pass: either make it pass, or — if it cannot pass as specified — call conclude_blocked with the evidence for why", true
	}
	// Commands ran and the table read none as a check. The host cannot tell a
	// runner from a deploy, so it owes nothing and says nothing: it has no
	// honest sentence, and failing on the guess is the gate over-reaching.
	if len(a.projectChecks) == 0 {
		if _, ok := a.task.ledger.LatestUnrecognizedCommandAfter(writer); ok {
			return "", false
		}
	}
	return ask, true
}

func finalReadinessCheckSource(check instruction.VerifyCheck) string {
	source := strings.TrimSpace(check.SourcePath)
	if source == "" {
		source = "project memory"
	}
	if check.Line > 0 {
		return fmt.Sprintf("%s:%d", source, check.Line)
	}
	return source
}

func finalReadinessIncompleteTodos(items []evidence.TodoStepMatch) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		label := strings.TrimSpace(item.Content)
		if label == "" {
			label = fmt.Sprintf("todo %d", item.Index)
		}
		parts = append(parts, fmt.Sprintf("%s: %s", label, item.Status))
	}
	return "latest successful todo_write still has incomplete items: " + strings.Join(parts, ", ")
}

// PrepareReadinessContinuation is the same authorization for a continuation the
// host runs itself rather than one the user asked for. Without it the next run
// starts from an empty ledger, where a turn that owed verification owes
// nothing: the gap would read as closed because the record of it was dropped.
func (a *Agent) PrepareReadinessContinuation() bool {
	return a.prepareEvidenceContinuation()
}

func (a *Agent) prepareEvidenceContinuation() bool {
	if !a.pending.deliveryRecovery {
		return false
	}
	a.pending.preserveEvidence = true
	a.pending.deliveryRecovery = false
	return true
}

// ReadinessResult is the host-consumable outcome of the Delivery final-answer
// readiness check. The Controller reads it after each goal turn; plain turns
// receive the same outcome as a FinalReadinessError.
type ReadinessResult struct {
	// Ready is true when no missing requirement remains.
	Ready bool
	// Missing lists stable category ids of the missing requirements
	// (project_check, todo, criteria, verification, review, signoff, action,
	// mutation, capability). Empty when Ready.
	Missing []string
	// Reason is the user-facing summary of what is still missing.
	Reason string
	// ProgressKey is the host-verifiable progress signature of the current
	// evidence state. Identical ProgressKey across consecutive goal turns
	// means no host-observable progress was made.
	ProgressKey string
	// Why a candidate on the ledger does not close an obligation. Orthogonal to
	// Missing: "nothing offered" and "offered, unattributable" differ, and no
	// candidate means no entry. Reason is one sentence for the model, not this.
	Proofs []evidence.ReviewProof
}

// ReadinessResult returns the current final-readiness outcome for the host.
func (a *Agent) ReadinessResult() ReadinessResult {
	check := a.finalReadinessCheckFor()
	out := ReadinessResult{Ready: check.reason == "", ProgressKey: check.progressSignature(), Proofs: a.reviewProofs()}
	if !out.Ready {
		out.Missing, out.Reason = check.missingIDs(), check.reason
	}
	return out
}

// reviewProofs reads the ledger's classification of every candidate that failed
// to prove — independent of readiness, because a run that owes nothing today
// may owe something after its next write.
func (a *Agent) reviewProofs() []evidence.ReviewProof {
	if a == nil || a.task.ledger == nil {
		return nil
	}
	var out []evidence.ReviewProof
	for _, kind := range evidence.ReviewKinds() {
		if proof := a.task.ledger.ReviewProofFor(kind); proof.Gap != evidence.ReviewProofNone {
			out = append(out, proof)
		}
	}
	return out
}
