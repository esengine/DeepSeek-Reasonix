package provider

// Records a tool result carries for this host alone. Provider serializers never
// emit them: projection.go strips them at the boundary and keeps them in the
// stored projection the next compaction reads.

// ToolFailure says a result did not succeed, and why when the host refused.
// Shell calls also carry ToolExecution; every other tool has only this.
type ToolFailure struct {
	RefusalCode string `json:"refusalCode,omitempty"`
	Blocked     bool   `json:"blocked,omitempty"`
}

// ToolExecution is host-local shell metadata mirrored from tool.ShellExecution.
// Provider serializers must never emit this object on the wire.
type ToolExecution struct {
	Kind           string `json:"kind,omitempty"`
	Shell          string `json:"shell,omitempty"`
	ShellVersion   string `json:"shellVersion,omitempty"`
	Platform       string `json:"platform,omitempty"`
	SupportsAndAnd bool   `json:"supportsAndAnd"`
	State          string `json:"state,omitempty"`
	FailurePhase   string `json:"failurePhase,omitempty"`
	ExitCode       *int   `json:"exitCode,omitempty"`
	OutputTail     string `json:"outputTail,omitempty"`
	MutationRisk   string `json:"mutationRisk,omitempty"`
	Verification   string `json:"verification,omitempty"`
	DurationMs     int64  `json:"durationMs,omitempty"`
}
