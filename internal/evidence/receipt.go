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
	// ID is a stable host-issued reference for model-facing evidence citations.
	ID          string `json:"id,omitempty"`
	OperationID string `json:"operation_id,omitempty"`
	// ToolCallID links UI inspection to the source call without entering prompts.
	ToolCallID  string          `json:"-"`
	Interrupted bool            `json:"-"`
	Sequence    uint64          `json:"-"`
	ToolName    string          `json:"tool_name"`
	Args        json.RawMessage `json:"args,omitempty"`
	Profile     string          `json:"profile,omitempty"`
	Success     bool            `json:"success"`
	Command     string          `json:"command,omitempty"`
	Step        string          `json:"step,omitempty"`
	StepProof   bool            `json:"step_proof,omitempty"`
	TodoStep    *TodoStepMatch  `json:"todo_step,omitempty"`
	Paths       []string        `json:"paths,omitempty"`
	Read        bool            `json:"read,omitempty"`
	Write       bool            `json:"write,omitempty"`
	Mutation    bool            `json:"mutation,omitempty"`
	// DeliveryScope separates scratch-only execution from project delivery debt.
	// It is turn-local evidence and is never persisted or provider-visible.
	DeliveryScope WriteScope `json:"-"`
	Todos         []TodoItem `json:"todos,omitempty"`
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
	// PolicyFloor is the session quality floor in force when this write was
	// committed ("delivery" or empty). Host-only replay fact: the contract
	// rebuild reads it back so a floor change never rewrites history.
	PolicyFloor string `json:"policy_floor,omitempty"`
}

// ReceiptRef is the bounded, model-safe projection of a host receipt: enough
// to cite a fact by ID, never enough to leak local paths beyond the ones the
// call already named, internal state, or raw output.
type ReceiptRef struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Success     bool     `json:"success"`
	OperationID string   `json:"operation_id,omitempty"`
	Paths       []string `json:"paths,omitempty"`
	Digest      string   `json:"digest,omitempty"`
	// Summary is display-only. It must never be used as an identity key: that
	// is what made shell prefixes and quoting differences reject real work.
	Summary string `json:"summary,omitempty"`
}

func (r ReceiptRef) clone() ReceiptRef {
	r.Paths = append([]string(nil), r.Paths...)
	return r
}

// Ref projects the receipt into its citable form.
func (r Receipt) Ref() ReceiptRef {
	return ReceiptRef{
		ID:          r.ID,
		Kind:        r.Kind(),
		Success:     r.Success,
		OperationID: r.OperationID,
		Paths:       append([]string(nil), r.Paths...),
		Digest:      r.OutputDigest,
		Summary:     receiptSummary(r),
	}
}

// Kind classifies what a receipt proves. A command is only a verification when
// the host recognized the verifier: an unclassified command stays a command,
// which is a recorded success rather than a rejection.
func (r Receipt) Kind() string {
	switch {
	case r.Mutation || r.Write:
		return ReceiptKindMutation
	case r.ToolName == "review_report" || r.ToolName == "review":
		return ReceiptKindReview
	case r.Command != "":
		if r.Verification == VerificationPassed || r.Verification == VerificationFailed {
			return ReceiptKindVerification
		}
		if IsVerificationCommand(r.Command) {
			return ReceiptKindVerification
		}
		return ReceiptKindCommand
	case r.Read:
		return ReceiptKindRead
	default:
		return ReceiptKindCommand
	}
}

// receiptSummary is a short, bounded label for display. Long commands are
// truncated because nothing matches on this text.
func receiptSummary(r Receipt) string {
	summary := strings.TrimSpace(r.Command)
	if summary == "" {
		summary = r.ToolName
		if len(r.Paths) > 0 {
			summary += " " + r.Paths[0]
		}
	}
	if runes := []rune(summary); len(runes) > 80 {
		summary = string(runes[:80]) + "…"
	}
	return summary
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
)
