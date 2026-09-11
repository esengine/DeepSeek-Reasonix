package extension

// ResumeDecision is the product-facing recovery answer for a generation.
// Irreversible receipts never produce CleanRollback=true.
type ResumeDecision struct {
	// AllowResume is always true when the process is healthy enough to continue
	// the session; external side effects may already have happened.
	AllowResume bool `json:"allowResume"`
	// CleanRollback is true only when no irreversible work completed and all
	// compensatable work finished compensation successfully.
	CleanRollback bool `json:"cleanRollback"`
	// HasIrreversible reports completed irreversible effects.
	HasIrreversible bool `json:"hasIrreversible"`
	// Notes are human-readable doctor/UI lines.
	Notes []string `json:"notes,omitempty"`
	// Blocking receipt IDs that prevent CleanRollback (not AllowResume).
	Blocking []string `json:"blocking,omitempty"`
}

// DecideResume allows resume but denies clean rollback for irreversible or
// uncompensated effects. Empty or fully compensated generations are clean;
// irreversible receipts are never rewritten to "rolled_back".
func DecideResume(store *ReceiptStore, gen uint64) ResumeDecision {
	if store == nil {
		return ResumeDecision{AllowResume: true, CleanRollback: true}
	}
	rec := store.AssessRecoverability(gen)
	return ResumeDecision{
		AllowResume:     true,
		CleanRollback:   rec.Clean,
		HasIrreversible: rec.HasIrreversible,
		Notes:           rec.Notes,
		Blocking:        rec.Blocking,
	}
}

// DecideResumeDefault uses the compatibility owner's ledger.
func DecideResumeDefault(gen uint64) ResumeDecision {
	return DecideResume(RuntimeOwnerOrDefault(nil).Receipts, gen)
}

// RecordProviderSubmit marks a provider request as already submitted
// (irreversible). Call after the sidecar accepts stream open.
func RecordProviderSubmit(generation uint64, streamID, owner string) {
	RuntimeOwnerOrDefault(nil).RecordProviderSubmit(generation, streamID, owner)
}

// RecordMessageSent marks a user-visible outbound message as sent.
func RecordMessageSent(generation uint64, messageID, owner string) {
	RuntimeOwnerOrDefault(nil).RecordMessageSent(generation, messageID, owner)
}
