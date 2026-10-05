package evidence

import (
	"encoding/json"
	"slices"
	"strings"

	"reasonix/internal/base/shellparse"
	"reasonix/internal/safety/shellsafe"
)

// PipeStatusCanDecideVerification reports whether per-stage exit statuses could
// answer the check inside command: a pipeline that could end the command, whose
// stages have distinct widths, and whose check sits before the stage that
// decides the status. Shape only — the caller says whether the shell in hand
// reports those statuses.
func PipeStatusCanDecideVerification(command string) bool {
	candidates, ok := shellparse.TerminalPipelines(command)
	if !ok {
		return false
	}
	// Widths must differ across candidates, or the captured statuses could not
	// say which pipeline produced them.
	widths := map[int]int{}
	for _, stages := range candidates {
		widths[len(stages)]++
	}
	for _, stages := range candidates {
		if len(stages) < 2 || widths[len(stages)] > 1 {
			continue
		}
		if slices.ContainsFunc(stages[:len(stages)-1], CommandRunsVerification) {
			return true
		}
	}
	return false
}

// VerificationExitConclusive reports whether a zero exit status would prove the
// verification inside command passed. A pipe, `;`, or `||` can hand the status
// to a later stage — `go test ./... | head` exits 0 on a failing suite — while
// `&&` still short-circuits on the suite. A command no static pass can
// decompose is inconclusive by the same caution.
func VerificationExitConclusive(command string) bool {
	proven, ok := shellparse.ExitZeroImplies(command)
	if !ok {
		return false
	}
	for _, segment := range proven {
		normalized, safeRedirects := shellsafe.NormalizeBashSafeRedirectsForMatch(segment)
		if !safeRedirects {
			continue
		}
		if fields, malformed := shellparse.StaticFields(normalized); malformed == "" && bashSegmentIsVerification(fields) {
			return true
		}
	}
	return false
}

// VerificationOutcomeFromPipeStatus reads the verdict off per-stage statuses
// the host captured, so a check whose exit status a later stage swallowed is
// still answered rather than shrugged at. It returns "" when the statuses do
// not line up with the pipeline, or when no stage of it is a verification.
func VerificationOutcomeFromPipeStatus(command string, status []int) string {
	if len(status) == 0 {
		return ""
	}
	stages, ok := shellparse.PipelineForStatus(command, len(status))
	if !ok {
		return ""
	}
	verdict := ""
	for i, stage := range stages {
		if !bashContainsVerificationSegment(stage) {
			continue
		}
		if status[i] != 0 {
			return VerificationFailed
		}
		verdict = VerificationPassed
	}
	return verdict
}

// HasVerificationCommandAfter reports whether a check ran after the boundary at
// all, whatever it concluded. A turn the model declared blocked still owes the
// run that establishes the blocker; what it cannot owe is a passing one.
func (l *Ledger) HasVerificationCommandAfter(after int) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.ContainsFunc(l.receipts[max(after+1, 0):], ReceiptRunsVerification)
}

// HasFailedVerificationAfter reports whether any check still stands failed
// after the boundary. Without it `go test; go vet` reads as verified on a red
// suite, since the shell's status is go vet's. Outcomes fold per check, so
// re-running one until it passes clears it.
func (l *Ledger) HasFailedVerificationAfter(after int) bool {
	return len(l.FailedVerificationsAfter(after)) > 0
}

// FailedVerificationsAfter names the checks that stand failed after the
// boundary, latest run last. A model told only that "the check did not pass"
// has to guess which of the ones it ran the host means, and a check it cannot
// name is one it can neither re-run nor conclude on.
func (l *Ledger) FailedVerificationsAfter(after int) []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	latest := map[string]string{}
	var order []string
	for _, r := range l.receipts[max(after+1, 0):] {
		if !ReceiptRunsVerification(r) {
			continue
		}
		outcome := VerificationOutcome(r)
		if outcome == "" {
			continue
		}
		id := VerificationIdentity(r.Command)
		if _, seen := latest[id]; !seen {
			order = append(order, id)
		}
		latest[id] = outcome
	}
	var failed []string
	for _, id := range order {
		if latest[id] == VerificationFailed {
			failed = append(failed, id)
		}
	}
	return failed
}

// HasBlockedConclusionAfter reports whether the model declared, after the
// boundary, that the task cannot be completed as specified. The tool checked
// the claim's evidence before the receipt could succeed.
func (l *Ledger) HasBlockedConclusionAfter(after int) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts[max(after+1, 0):] {
		if r.ToolName == "conclude_blocked" && r.Success {
			return true
		}
	}
	return false
}

// LatestUnreadableVerificationAfter returns the most recent check that ran
// after the boundary and whose outcome the host could not read. Telling a model
// to "run a verification" when it just ran one it cannot see the result of
// sends it round the same loop; naming the command is what breaks it.
func (l *Ledger) LatestUnreadableVerificationAfter(after int) (Receipt, bool) {
	if l == nil {
		return Receipt{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.receipts) - 1; i > after && i >= 0; i-- {
		if r := l.receipts[i]; r.Success && r.Verification == VerificationInconclusive {
			return r, true
		}
	}
	return Receipt{}, false
}

// LatestUnrecognizedCommandAfter returns the most recent successful command
// after the boundary that the host did not read as a check. It answers a
// different question from "no check ran": the model ran something and the
// classifier, which cannot tell a project's check from a deploy, saw nothing.
func (l *Ledger) LatestUnrecognizedCommandAfter(after int) (Receipt, bool) {
	if l == nil {
		return Receipt{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.receipts) - 1; i > after && i >= 0; i-- {
		r := l.receipts[i]
		if r.Success && strings.TrimSpace(r.Command) != "" && !ReceiptRunsVerification(r) {
			return r, true
		}
	}
	return Receipt{}, false
}

// ReceiptRunsVerification reports whether a receipt carries a verification. The
// host classified the call as it ran — including anything the static tables
// could not read alone — so the receipt is the answer, and the tables only
// stand in for receipts recorded before that classification existed.
func ReceiptRunsVerification(r Receipt) bool {
	switch r.Verification {
	case VerificationNotRun, VerificationNotVerification:
		return false
	case "":
		return r.ToolName == "bash" && bashContainsVerificationSegment(r.Command)
	default:
		return true
	}
}

// WholeCommandExitConclusive reports whether a zero exit proves every top-level
// segment of command succeeded. VerificationExitConclusive asks the same of the
// segments a name table recognized; this one asks it of the whole line, so a
// caller that already knows a command is a check can read its exit status as
// the verdict without the table having an opinion about it.
func WholeCommandExitConclusive(command string) bool {
	segments, _, ok := shellparse.SplitTopLevel(command)
	if !ok || len(segments) == 0 {
		return false
	}
	proven, ok := shellparse.ExitZeroImplies(command)
	if !ok || len(proven) != len(segments) {
		return false
	}
	// A pipeline is one segment whose proven entry is only its last stage, so
	// equal counts still need the segments themselves to line up.
	for i, segment := range segments {
		if strings.TrimSpace(segment) != strings.TrimSpace(proven[i]) {
			return false
		}
	}
	return true
}

// CommandExitShapeReadable reports whether any static pass can decide what a
// zero exit proves. It is false for a subshell, a here-doc, a background job,
// or a negation — shapes where the host must say it cannot read the command
// rather than name a cause it never found.
func CommandExitShapeReadable(command string) bool {
	_, ok := shellparse.ExitZeroImplies(command)
	return ok
}

// CommandRunsVerification reports whether command runs a verification at all,
// whatever else it also runs. IsDeliveryVerificationCommand asks the stricter
// question — whether the command runs *only* verification and read-only work —
// and stays the gate for delivery sign-off. This one decides whether a receipt
// carries a verification verdict, so a check is not discarded for its company.
func CommandRunsVerification(command string) bool {
	return bashContainsVerificationSegment(command)
}

// VerificationOutcome is a verification receipt's host-read verdict:
// VerificationPassed, VerificationFailed, or "" when the shell's exit status
// proved neither. A receipt the host never classified carries only its own
// outcome, so that is what it answers with.
func VerificationOutcome(r Receipt) string {
	switch r.Verification {
	case VerificationPassed:
		return VerificationPassed
	case VerificationFailed:
		return VerificationFailed
	case VerificationInconclusive, VerificationNotRun:
		return ""
	}
	// Unclassified leaves only the shell's status, which answers for the check
	// only when no later stage could have taken it over: `go test ./... | tail`
	// exits 0 on a red suite, and would clear the failure it is hiding.
	if !VerificationExitConclusive(r.Command) {
		return ""
	}
	if r.Success {
		return VerificationPassed
	}
	return VerificationFailed
}

func verificationPassed(r Receipt) bool {
	return VerificationOutcome(r) == VerificationPassed
}

// VerificationIdentity reduces command to the verification it runs, so the same
// check re-run inside a different wrapper stays one item whose latest outcome
// supersedes the earlier one. It drops spelling and plumbing, never context: a
// segment before the check decides where and under what it runs. Commands whose
// segments cannot be isolated keep their own text.
func VerificationIdentity(command string) string {
	command = strings.TrimSpace(command)
	segments, ok := verifierSearchSegments(command)
	if !ok {
		return command
	}
	var kept []string
	lastVerifier := -1
	for _, segment := range segments {
		normalized, safeRedirects := shellsafe.NormalizeBashSafeRedirectsForMatch(segment)
		if !safeRedirects {
			return command
		}
		fields, malformed := shellparse.StaticFields(normalized)
		if malformed != "" {
			return command
		}
		verifier := bashSegmentIsVerification(fields)
		if !verifier && !segmentSetsContext(fields) {
			continue
		}
		if verifier {
			lastVerifier = len(kept)
		}
		kept = append(kept, strings.Join(fields, " "))
	}
	if lastVerifier < 0 {
		return command
	}
	// Everything after the last check is where its output went, which cannot
	// change what it already proved.
	return strings.Join(kept[:lastVerifier+1], " && ")
}

// IsVerificationToolCall reports whether a persisted tool call is a bash
// command whose exit status provides implementation evidence. Diagnostic
// readers share the delivery classifier so "verification" means the same thing
// in a report as it does in the gate that accepts a sign-off.
func IsVerificationToolCall(toolName string, args json.RawMessage) bool {
	if !strings.EqualFold(strings.TrimSpace(toolName), "bash") {
		return false
	}
	command, ok := bashCommandFromArgs(args)
	if !ok {
		return false
	}
	return bashCommandIsVerification(command)
}

// segmentSetsContext reports whether a segment decides where or under what a
// later check runs. Only these join the check in its identity: a listing or a
// status standing beside it changes nothing it proves, and keeping one would
// split a single check into two items that never supersede each other.
func segmentSetsContext(fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	if shellparse.IsAssignment(fields[0]) {
		return true
	}
	switch shellparse.WordBase(fields[0]) {
	case "cd", "pushd", "popd", "export", "set", "unset", "source", ".":
		return true
	}
	return false
}

// verifierSearchSegments decomposes command for verifier matching. A here-doc
// costs SplitTopLevel the whole command, which used to take the checks standing
// beside the body down with it; the body itself is unread either way.
func verifierSearchSegments(command string) ([]string, bool) {
	if segments, _, ok := shellparse.SplitTopLevel(command); ok {
		return segments, true
	}
	return shellparse.SplitOutsideHereDoc(command)
}
