package evidence

import "encoding/json"

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
	TodoStep  *TodoStepMatch  `json:"todo_step,omitempty"`
	Paths     []string        `json:"paths,omitempty"`
	Read      bool            `json:"read,omitempty"`
	Write     bool            `json:"write,omitempty"`
	Mutation  bool            `json:"mutation,omitempty"`
	Todos     []TodoItem      `json:"todos,omitempty"`
	// OutputBytes is the host-observed length of the tool's (redacted, trimmed)
	// output. Content-evidence checks require it to be non-zero so a command
	// that printed nothing (head -n 0, >/dev/null) can never count as reading.
	OutputBytes int `json:"output_bytes,omitempty"`
	// ExitCode is the status the child process actually returned. Success only
	// says the tool call itself completed, so a failing test run the tool
	// reported cleanly stays distinguishable here. Zero differs from unset.
	ExitCode *int `json:"exit_code,omitempty"`
	// Verification is the host's classification of a shell call: one of the
	// Verification* values. Empty means the host never classified this receipt.
	Verification string `json:"verification,omitempty"`
}

// Verification classifications mirror tool.ShellVerification*, duplicated so
// this package keeps importing nothing from the tool layer.
const (
	VerificationNotVerification = "not_verification"
	VerificationNotRun          = "not_run"
	VerificationPassed          = "passed"
	VerificationFailed          = "failed"
)
