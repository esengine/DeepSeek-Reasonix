package agent

import "context"

// CodeVerifier is the optional inference-time code verification hook
// ([agent.cosplay] auto_on_mutation). Defined as an interface in the agent
// package so agent does not import cosplay: domain boundaries stay clean and
// PR #7791 can drop the cosplay package without touching the agent kernel.
// cosplay.AutoVerifier implements it; boot wires the concrete value.
type CodeVerifier interface {
	// MaybeVerify kicks off async verification of a mutated file and reports
	// the outcome through onResult.
	MaybeVerify(ctx context.Context, path string, onResult func(summary string))
	// HasWork reports whether verification of the path is pending/queued.
	HasWork(path string) bool
}
