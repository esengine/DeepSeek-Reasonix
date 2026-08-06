package tool

import (
	"context"
	"encoding/json"
)

// ShellExecution is local host metadata for one shell invocation. It is never
// part of the provider-visible tool schema or request bytes; ModelMessages and
// provider serializers must strip it before a model request leaves the host.
//
// Kind is always "shell" for shell invocations so UIs can distinguish this
// optional payload from other future execution kinds without guessing.
type ShellExecution struct {
	Kind         string `json:"kind"`
	Shell        string `json:"shell,omitempty"`        // bash | git-bash | powershell | pwsh
	ShellVersion string `json:"shellVersion,omitempty"` // 5.1 | 7+ (PowerShell only)
	Platform     string `json:"platform,omitempty"`     // windows | darwin | linux
	// SupportsAndAnd is explicit even when false so UIs can show PowerShell 5.1
	// chaining limits without treating omission as "unknown".
	SupportsAndAnd bool   `json:"supportsAndAnd"`
	State          string `json:"state,omitempty"`        // running | completed | failed | timed_out | cancelled | background_started | not_run
	FailurePhase   string `json:"failurePhase,omitempty"` // preflight | authorization | dependency | launch | execution | timeout | cancellation
	// ExitCode is set only when a child process started and produced an exit
	// status. Zero is a valid successful code (*int keeps 0 distinct from unset).
	ExitCode *int `json:"exitCode,omitempty"`
	// OutputTail is the bounded tail of combined stdout+stderr, set only for a
	// run that did not succeed. Both streams share one pipe so model-visible
	// interleaving stays in child-write order, which rules out a stderr-only
	// tail. At most 16 KiB; never a shell executable absolute path.
	OutputTail   string `json:"outputTail,omitempty"`
	MutationRisk string `json:"mutationRisk,omitempty"` // none | not_started | may_have_completed | may_be_partial | unknown
	Verification string `json:"verification,omitempty"` // not_verification | not_run | passed | failed
	DurationMs   int64  `json:"durationMs,omitempty"`
}

// Shell execution state values.
const (
	ShellStateRunning           = "running"
	ShellStateCompleted         = "completed"
	ShellStateFailed            = "failed"
	ShellStateTimedOut          = "timed_out"
	ShellStateCancelled         = "cancelled"
	ShellStateBackgroundStarted = "background_started"
	ShellStateNotRun            = "not_run"
)

// Shell failure phase values.
const (
	ShellPhasePreflight     = "preflight"
	ShellPhaseAuthorization = "authorization"
	ShellPhaseDependency    = "dependency"
	ShellPhaseLaunch        = "launch"
	ShellPhaseExecution     = "execution"
	ShellPhaseTimeout       = "timeout"
	ShellPhaseCancellation  = "cancellation"
)

// Shell mutation risk values.
const (
	ShellMutationNone             = "none"
	ShellMutationNotStarted       = "not_started"
	ShellMutationMayHaveCompleted = "may_have_completed"
	ShellMutationMayBePartial     = "may_be_partial"
	ShellMutationUnknown          = "unknown"
)

// Shell verification values.
const (
	ShellVerificationNotVerification = "not_verification"
	ShellVerificationNotRun          = "not_run"
	ShellVerificationPassed          = "passed"
	ShellVerificationFailed          = "failed"
)

// Shell name values for ShellExecution.Shell.
const (
	ShellNameBash       = "bash"
	ShellNameGitBash    = "git-bash"
	ShellNamePowerShell = "powershell"
	ShellNamePwsh       = "pwsh"
)

// PowerShell version labels.
const (
	ShellVersionPS51 = "5.1"
	ShellVersionPS7  = "7+"
)

// OutputTailMaxBytes bounds the output tail retained on ShellExecution.
const OutputTailMaxBytes = 16 << 10

// DetailedResult is the structured outcome of a DetailedExecutor call.
// Output remains the model-visible text; Execution is host/UI metadata only.
type DetailedResult struct {
	Output    string
	Images    []string
	Execution *ShellExecution
}

// DetailedExecutor is an optional Tool capability that returns structured
// execution metadata alongside the model-visible result text. Tools that do
// not implement it continue to use ImageTool/Tool.Execute.
type DetailedExecutor interface {
	// ExecutionDescriptor returns a descriptor for the would-be execution
	// before the process starts (shell identity, platform, chaining support).
	// It must not launch a process. Args may be empty or invalid — return a
	// best-effort descriptor from the bound shell configuration.
	ExecutionDescriptor(args json.RawMessage) *ShellExecution
	// ExecuteDetailed runs the tool and returns structured metadata. On
	// policy/preflight blocks, Execution must still be populated (state=not_run).
	ExecuteDetailed(ctx context.Context, args json.RawMessage) (DetailedResult, error)
}

// CloneShellExecution returns a deep copy suitable for attaching to events or
// session messages without sharing mutable pointers (e.g. ExitCode).
func CloneShellExecution(in *ShellExecution) *ShellExecution {
	if in == nil {
		return nil
	}
	out := *in
	if in.ExitCode != nil {
		code := *in.ExitCode
		out.ExitCode = &code
	}
	return &out
}

// IntPtr returns a pointer to v for ShellExecution.ExitCode.
func IntPtr(v int) *int { return &v }
