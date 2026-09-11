package agent

import (
	"unicode/utf8"

	"reasonix/internal/provider"
)

const (
	readAutoRecoveryContextShareDivisor  = 4
	readAutoRecoveryContextReserveTokens = 1024
)

type readAutoRecoveryBudget struct {
	maxTokens      int
	windowTokens   int
	currentTokens  int
	headroomTokens int
	known          bool
}

// readAutoRecoveryBudgetFor derives a live budget without a product-level byte
// or token ceiling. Unknown context windows deliberately return no automatic
// budget so the host switches to the bounded search/read strategy instead of
// guessing how much context is safe.
func (a *Agent) readAutoRecoveryBudgetFor() readAutoRecoveryBudget {
	if a == nil {
		return readAutoRecoveryBudget{}
	}
	window := a.effectiveContextWindow()
	if window <= 0 {
		return readAutoRecoveryBudget{}
	}
	current := a.estimatedVisibleRequestTokens(a.modelVisibleMessages())
	headroom := max(0, a.compactTrigger()-current-readAutoRecoveryContextReserveTokens)
	return readAutoRecoveryBudget{
		maxTokens:      min(max(0, window/readAutoRecoveryContextShareDivisor), headroom),
		windowTokens:   window,
		currentTokens:  current,
		headroomTokens: headroom,
		known:          true,
	}
}

// estimatedReadResultTokens uses the larger of the calibrated provider-facing
// estimate and the cross-language conservative estimate. The latter prices CJK
// near one rune per token, avoiding the under-count produced by bytes/4 alone.
func (a *Agent) estimatedReadResultTokens(result string) int {
	conservative := estimateCrossLanguageReadTokens(result)
	if a == nil {
		return conservative
	}
	message := provider.Message{
		Role: provider.RoleTool, Name: "read_file", Content: result,
	}
	calibrated := a.estimatedPromptTokens([]provider.Message{message})
	message.Content = ""
	calibrated -= a.estimatedPromptTokens([]provider.Message{message})
	return max(conservative, calibrated)
}

func estimateCrossLanguageReadTokens(result string) int {
	if result == "" {
		return 0
	}
	cjkRunes := 0
	cjkBytes := 0
	for _, r := range result {
		if isCJKRune(r) {
			cjkRunes++
			cjkBytes += utf8.RuneLen(r)
		}
	}
	nonCJKBytes := len(result) - cjkBytes
	return (nonCJKBytes+3)/4 + cjkRunes
}
