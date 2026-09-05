package evidence

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// Receipt is the host-runtime record of one tool call. It stays in memory for
// the current agent turn and is not serialized into prompts or session state.
type Receipt struct {
	ToolName  string          `json:"tool_name"`
	Args      json.RawMessage `json:"args,omitempty"`
	Profile   string          `json:"profile,omitempty"`
	Success   bool            `json:"success"`
	Command   string          `json:"command,omitempty"`
	Step      string          `json:"step,omitempty"`
	StepProof bool            `json:"step_proof,omitempty"`
	// CitedChecks are the verification commands a complete_step named. The tool
	// refuses a citation whose command has no successful receipt, so a command
	// here is one the host already matched against what actually ran.
	CitedChecks []string       `json:"cited_checks,omitempty"`
	TodoStep    *TodoStepMatch `json:"todo_step,omitempty"`
	Paths       []string       `json:"paths,omitempty"`
	// CriteriaRewritten names existing tests this call rewrote or removed
	// (see RewrittenTestCriteria). Editing a check is legitimate; doing it
	// silently while reporting the suite as green is not.
	CriteriaRewritten []string   `json:"criteria_rewritten,omitempty"`
	Read              bool       `json:"read,omitempty"`
	Write             bool       `json:"write,omitempty"`
	Mutation          bool       `json:"mutation,omitempty"`
	Todos             []TodoItem `json:"todos,omitempty"`
	// OutputBytes is the host-observed length of the tool's (redacted, trimmed)
	// output. Content-evidence checks require it to be non-zero so a command
	// that printed nothing (head -n 0, >/dev/null) can never count as reading.
	OutputBytes int `json:"output_bytes,omitempty"`
	// OutputDigest is a bounded host-derived identity for the model-visible
	// output. Goal progress uses it to distinguish a genuinely changed read or
	// command result from an exact successful repeat without retaining content.
	OutputDigest string `json:"output_digest,omitempty"`
	// ExitCode is the status the child process actually returned. Success only
	// says the tool call itself completed, so a failing test run the tool
	// reported cleanly stays distinguishable here. Zero differs from unset.
	ExitCode *int `json:"exit_code,omitempty"`
	// Verification is the host's classification of a shell call: one of the
	// Verification* values. Empty means the host never classified this receipt.
	Verification string `json:"verification,omitempty"`
	// MutationEvidence grades Mutation: one of the Mutation* values. Empty means
	// the host never graded it, which reads as unknown — a receipt from an older
	// session cannot retroactively prove what it wrote.
	MutationEvidence string `json:"mutation_evidence,omitempty"`
	// Created lists paths this call brought into existence: absent before it
	// ran, present after. A path the host watched appear is the only one it can
	// later watch disappear and conclude the turn kept nothing there.
	Created []string `json:"created,omitempty"`
	// Showed lists the changed paths whose content this call's model-visible
	// output demonstrably carried. It is decided while the output is in hand,
	// so the ledger keeps the verdict and never the content.
	Showed []string `json:"showed,omitempty"`
	// ReportKind is what a review_report handed back: the delivery fact, read
	// off the payload. It says what the worker owed, never what it may close.
	ReportKind ReviewKind `json:"report_kind,omitempty"`
	// ReviewAuthority is what the host granted the execution that produced this
	// receipt. Nil is unknown, and stays unknown: see ReceiptProves.
	ReviewAuthority *ReviewAuthority `json:"review_authority,omitempty"`
	// SourceExecutionID names the execution this receipt came from, so a grant
	// can be joined back to the run the host issued it to.
	SourceExecutionID string `json:"source_execution_id,omitempty"`
}

// ObserveOutput records the trimmed output size and a compact digest without
// retaining model-visible content in the evidence ledger.
func (r *Receipt) ObserveOutput(output string) {
	if r == nil {
		return
	}
	trimmed := strings.TrimSpace(output)
	r.OutputBytes = len(trimmed)
	if trimmed == "" {
		r.OutputDigest = ""
		return
	}
	sum := sha256.Sum256([]byte(trimmed))
	r.OutputDigest = fmt.Sprintf("%x", sum[:16])
}

// Verification classifications mirror tool.ShellVerification*, duplicated so
// this package keeps importing nothing from the tool layer.
const (
	VerificationNotVerification = "not_verification"
	VerificationNotRun          = "not_run"
	VerificationPassed          = "passed"
	VerificationFailed          = "failed"
	VerificationInconclusive    = "inconclusive"
)
