package hostaudit

// OutcomeSample decomposes one tool round's receipts by outcome: information
// gathered (Exploration), verification attempts run, and verification-command
// state transitions (Objective fail→pass, Regression pass→fail). Counts are
// unit-weighted; policy weighting is an offline concern.
type OutcomeSample struct {
	Round        int
	Exploration  int
	Verification int
	Objective    int
	Regression   int
	Churn        int
	// LegacyGain is the live novelty scorer's verdict on the same receipts, so
	// offline analysis can compare the two policies without replaying.
	LegacyGain int
	// Discriminating counts observations able to falsify the working
	// hypothesis: verification commands, or commands exercising a mutated
	// file — deliberately broader than delivery verification (repro scripts).
	Discriminating int
	// DebtAge counts consecutive rounds carrying an unverified mutation with
	// no discriminating observation; 0 while no verification debt is open.
	DebtAge int
	// BlindMutations counts mutations since the last discriminating observation.
	BlindMutations int
	// Stall counts this round's checks that ran, failed, and had already
	// failed: the third transition beside Objective and Regression, and the one
	// the live scorer prices as motion instead of as standing still.
	Stall int
	// StallAge counts consecutive rounds one check has stayed failed, and
	// StallMutations the change that landed against it without moving it.
	StallAge       int
	StallMutations int
}
