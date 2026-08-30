package taskcontract

// Unsatisfied returns a copy of obligations that still lack a later proof.
func (c *Contract) Unsatisfied() []Obligation {
	if c == nil {
		return nil
	}
	var out []Obligation
	for _, o := range c.Obligations {
		if !c.obligationSatisfied(o) {
			out = append(out, cloneObligation(o))
		}
	}
	return out
}

// Stop evaluates the current obligations against host facts.
func (c *Contract) Stop(opts StopOptions) StopDisposition {
	if c == nil || len(c.Obligations) == 0 {
		return StopReady
	}
	var (
		hasAdvisory    bool
		hasRecoverable bool
		recoverTried   bool
		hasStrict      bool
		strictTried    bool
	)
	for _, o := range c.Obligations {
		if c.obligationSatisfied(o) {
			continue
		}
		switch o.Enforcement {
		case EnforcementAdvisory:
			hasAdvisory = true
		case EnforcementRecoverable:
			hasRecoverable = true
			if o.RecoveryAttempts > 0 {
				recoverTried = true
			}
		case EnforcementStrict:
			hasStrict = true
			if o.RecoveryAttempts > 0 {
				strictTried = true
			}
		}
	}
	if !hasRecoverable && !hasStrict {
		return StopReady
	}
	if hasStrict {
		if opts.PermissionDenied || opts.RecoveryLimit || (strictTried && opts.EnvUnavailable) {
			return StopBlocked
		}
		if opts.LoopGuard && strictTried {
			return StopBlocked
		}
		return StopContinue
	}
	if hasRecoverable {
		if recoverTried || opts.EnvUnavailable || opts.RecoveryLimit || opts.LoopGuard {
			return StopPartial
		}
		return StopContinue
	}
	if hasAdvisory {
		return StopReady
	}
	return StopReady
}

// NoteRecoveryAttempt increments the first unsatisfied recoverable or strict
// obligation so a later Stop can distinguish first miss from a spent retry.
func (c *Contract) NoteRecoveryAttempt() {
	if c == nil {
		return
	}
	for i := range c.Obligations {
		o := &c.Obligations[i]
		if c.obligationSatisfied(*o) || o.Enforcement == EnforcementAdvisory {
			continue
		}
		o.RecoveryAttempts++
		return
	}
}

// AdvisoryGaps returns unsatisfied advisory obligations for the final summary.
func (c *Contract) AdvisoryGaps() []Obligation {
	if c == nil {
		return nil
	}
	var out []Obligation
	for _, o := range c.Obligations {
		if o.Enforcement == EnforcementAdvisory && !c.obligationSatisfied(o) {
			out = append(out, cloneObligation(o))
		}
	}
	return out
}

// Empty reports a contract with no requirements, checks, or obligations.
func (c *Contract) Empty() bool {
	return c == nil || (len(c.Requirements) == 0 && len(c.Checks) == 0 && len(c.Obligations) == 0)
}
