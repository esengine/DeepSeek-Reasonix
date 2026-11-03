package memory

import (
	"math"
	"strings"
	"unicode/utf8"
)

const (
	ImportanceLow    = 1
	ImportanceNormal = 2
	ImportanceHigh   = 3

	maxGalaxyRunes = 40
)

func NormalizeGalaxy(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxGalaxyRunes {
		s = string([]rune(s)[:maxGalaxyRunes])
	}
	return s
}

func NormalizeImportance(n int) int {
	if n < ImportanceLow || n > ImportanceHigh {
		return 0
	}
	return n
}

func DefaultImportance(t Type) int {
	switch NormalizeType(string(t)) {
	case TypeConstraint:
		return ImportanceHigh
	case TypeReference:
		return ImportanceLow
	}
	return ImportanceNormal
}

func (m Memory) ResolvedImportance() int {
	if n := NormalizeImportance(m.Importance); n != 0 {
		return n
	}
	return DefaultImportance(m.Type)
}

func (m Memory) NoDecay() bool {
	if NormalizeType(string(m.Type)) == TypeConstraint || ResolveActivation(m) == ActivationPinned {
		return true
	}
	return NormalizeVolatility(string(m.Volatility)) == VolatilityEvergreen
}

func InitialStrengthDays(m Memory) float64 {
	switch NormalizeVolatility(string(m.Volatility)) {
	case VolatilityStable:
		return 120
	case VolatilityVolatile:
		return 14
	}
	switch m.ResolvedImportance() {
	case ImportanceLow:
		return 7
	case ImportanceHigh:
		return 120
	}
	return 30
}

func (m Memory) StrengthDays() float64 {
	if m.Strength > 0 && !math.IsNaN(m.Strength) && !math.IsInf(m.Strength, 0) {
		return m.Strength
	}
	return InitialStrengthDays(m)
}
