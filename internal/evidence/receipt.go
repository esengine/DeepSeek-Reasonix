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
	Sequence  uint64          `json:"-"`
	ToolName  string          `json:"tool_name"`
	Args      json.RawMessage `json:"args,omitempty"`
	Profile   string          `json:"profile,omitempty"`
	Success   bool            `json:"success"`
	Command   string          `json:"command,omitempty"`
	Step      string          `json:"step,omitempty"`
	StepProof bool            `json:"step_proof,omitempty"`
	TodoStep  *TodoStepMatch  `json:"todo_step,omitempty"`
	Paths     []string        `json:"paths,omitempty"`
	Read      bool            `json:"read,omitempty"`
	Write     bool            `json:"write,omitempty"`
	Mutation  bool            `json:"mutation,omitempty"`
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
