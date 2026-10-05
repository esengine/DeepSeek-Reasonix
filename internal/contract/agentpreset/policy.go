package agentpreset

// VerificationLevel is how much post-mutation checking the host requires.
type VerificationLevel uint8

const (
	// VerifyNone requires no host verification commands.
	VerifyNone VerificationLevel = iota
	// VerifyTargeted runs the cheapest relevant checks and diff review.
	VerifyTargeted
	// VerifyFull requires every acceptance check for the current epoch.
	VerifyFull
)

// VerificationPolicy is post-mutation verification intensity.
type VerificationPolicy struct {
	Level VerificationLevel
}

// PresetPolicy is the compiled, immutable strategy for one AgentPreset.
type PresetPolicy struct {
	Preset             AgentPreset
	VerificationPolicy VerificationPolicy
}

// PolicyOf returns the compiled strategy for preset. Unknown values use Balanced.
// Review intensity is deliberately absent: what a change owes is read off the
// mutation receipts it produced, which no role setting can know in advance.
func PolicyOf(preset AgentPreset) PresetPolicy {
	if Normalize(string(preset)) == Delivery {
		return PresetPolicy{Preset: Delivery, VerificationPolicy: VerificationPolicy{Level: VerifyFull}}
	}
	return PresetPolicy{Preset: Balanced, VerificationPolicy: VerificationPolicy{Level: VerifyTargeted}}
}
