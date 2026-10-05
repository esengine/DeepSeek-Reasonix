// verification_drift.go — the declaration a Goal was accepted under, beside the
// one this process loaded.
package event

import "reasonix/internal/base/nilutil"

// VerificationContractDrift is one comparison between the contract a resumed
// Goal was frozen under and the declaration the current process read. It
// observes and decides nothing. It does not say the frozen contract should be
// kept, that the current declaration supersedes it, or that the Goal owes
// anything: which of the two governs is a decision no observation may make.
type VerificationContractDrift struct {
	ScopeID string
	Epoch   uint64
	// Frozen and Current are canonical criterion identities — the project's own
	// declared commands, the same text a readiness reason already shows.
	Frozen  []string
	Current []string
	Drift   bool
}

// VerificationContractDriftSink is an optional sink capability.
type VerificationContractDriftSink interface {
	RecordVerificationContractDrift(VerificationContractDrift)
}

// RecordVerificationContractDrift forwards one comparison to sinks that opt in.
func RecordVerificationContractDrift(s Sink, d VerificationContractDrift) {
	if nilutil.IsNil(s) {
		return
	}
	if ds, ok := s.(VerificationContractDriftSink); ok {
		ds.RecordVerificationContractDrift(d)
	}
}
