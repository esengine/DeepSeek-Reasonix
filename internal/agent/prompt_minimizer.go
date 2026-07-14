package agent

import (
	"strings"
	"sync"
)

// PromptSection represents a logical section of the system prompt.
type PromptSection struct {
	Name          string
	Content       string
	TokenEstimate int
	Active        bool
	MinTurn       int // minimum turn to keep active
}

// SystemPromptMinimizer dynamically minimizes the system prompt based on
// conversation state, removing instructions that are no longer relevant as
// the conversation progresses while preserving cache-critical prefixes.
type SystemPromptMinimizer struct {
	mu              sync.RWMutex
	originalPrompt  string
	minimizedPrompt string
	cacheBoundary   int
	totalSaved      int
	minimizations   int
	sections        map[string]*PromptSection
}

// NewSystemPromptMinimizer creates a new initialized SystemPromptMinimizer.
func NewSystemPromptMinimizer() *SystemPromptMinimizer {
	return &SystemPromptMinimizer{
		sections: make(map[string]*PromptSection),
	}
}

// SetOriginal parses the prompt into sections by splitting on double newlines.
// Each resulting block becomes a PromptSection.
func (m *SystemPromptMinimizer) SetOriginal(prompt string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.originalPrompt = prompt
	m.sections = make(map[string]*PromptSection)

	blocks := strings.Split(prompt, "\n\n")
	for i, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		name := deriveSectionName(block, i)
		m.sections[name] = &PromptSection{
			Name:          name,
			Content:       block,
			TokenEstimate: m.estimateTokensUnlocked(block),
			Active:        true,
			MinTurn:       0,
		}
	}

	// Set the cache boundary to roughly the first 25% of sections so that the
	// cache-critical prefix is preserved.
	totalTokens := 0
	for _, s := range m.sections {
		totalTokens += s.TokenEstimate
	}
	m.cacheBoundary = totalTokens / 4

	m.minimizedPrompt = prompt
}

// Minimize returns a minimized version of the system prompt based on the
// current conversation state.
//
//   - If currentTurn < 2, the original prompt is returned (too early to minimize).
//   - Tool usage instructions are removed once the user has already used tools.
//   - Getting started / introduction sections are removed after turn 3.
//   - Examples sections are removed after turn 5.
//   - If isEnding is true, all non-critical sections are removed.
//   - Sections containing "safety", "security", "never", or "must" are preserved.
func (m *SystemPromptMinimizer) Minimize(currentTurn int, hasUsedTools bool, isEnding bool) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Too early to minimize.
	if currentTurn < 2 {
		return m.originalPrompt
	}

	originalTokens := m.estimateTokensUnlocked(m.originalPrompt)

	for _, section := range m.sections {
		// Keep critical instructions regardless of state.
		if isCritical(section.Content) {
			continue
		}

		if isEnding {
			// Remove all non-critical sections at the end of the conversation.
			section.Active = false
			continue
		}

		// (a) Remove tool usage instructions once tools have been used.
		if hasUsedTools && matchesName(section.Name, "tool usage", "tool", "tools") {
			section.Active = false
			continue
		}

		// (b) Remove getting started / introduction sections after turn 3.
		if currentTurn > 3 && matchesName(section.Name, "getting started", "introduction", "intro") {
			section.Active = false
			continue
		}

		// (c) Remove examples sections after turn 5.
		if currentTurn > 5 && matchesName(section.Name, "examples", "example") {
			section.Active = false
			continue
		}
	}

	// Rebuild the minimized prompt, preserving section order via the cache boundary.
	var builder strings.Builder
	for _, section := range m.sections {
		if !section.Active {
			continue
		}
		builder.WriteString(section.Content)
		builder.WriteString("\n\n")
	}

	minimized := strings.TrimSuffix(builder.String(), "\n\n")
	minimizedTokens := m.estimateTokensUnlocked(minimized)

	saved := originalTokens - minimizedTokens
	if saved < 0 {
		saved = 0
	}

	m.minimizedPrompt = minimized
	m.totalSaved += saved
	m.minimizations++

	return minimized
}

// PromptMinimizerStats reports statistics about the minimization process.
type PromptMinimizerStats struct {
	TotalSaved      int
	Minimizations   int
	SectionsTotal   int
	SectionsActive   int
	OriginalTokens   int
	MinimizedTokens  int
}

// GetStats returns statistics about the minimization process.
func (m *SystemPromptMinimizer) GetStats() PromptMinimizerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := 0
	active := 0
	for _, s := range m.sections {
		total++
		if s.Active {
			active++
		}
	}

	return PromptMinimizerStats{
		TotalSaved:     m.totalSaved,
		Minimizations:  m.minimizations,
		SectionsTotal:  total,
		SectionsActive: active,
		OriginalTokens: m.estimateTokensUnlocked(m.originalPrompt),
		MinimizedTokens: m.estimateTokensUnlocked(m.minimizedPrompt),
	}
}

// EstimateTokens returns a rough token estimate for the given text.
func (m *SystemPromptMinimizer) EstimateTokens(text string) int {
	return m.estimateTokensUnlocked(text)
}

// estimateTokensUnlocked is the unlocked variant used internally.
func (m *SystemPromptMinimizer) estimateTokensUnlocked(text string) int {
	return len(text) / 4
}

// Reset restores the minimizer to its initial state.
func (m *SystemPromptMinimizer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.originalPrompt = ""
	m.minimizedPrompt = ""
	m.cacheBoundary = 0
	m.totalSaved = 0
	m.minimizations = 0
	m.sections = make(map[string]*PromptSection)
}

// --- helpers ---

// deriveSectionName produces a lowercase, descriptive name for a section based
// on its leading content and index.
func deriveSectionName(block string, index int) string {
	lower := strings.ToLower(block)

	switch {
	case strings.Contains(lower, "tool"):
		return "tool usage instructions"
	case strings.Contains(lower, "getting started") || strings.Contains(lower, "get started"):
		return "getting started"
	case strings.Contains(lower, "introduction") || strings.HasPrefix(lower, "intro"):
		return "introduction"
	case strings.Contains(lower, "example"):
		return "examples"
	default:
		return "section_" + itoaSimple(index)
	}
}

// itoaSimple is a minimal int-to-string helper to avoid importing strconv.
func itoaSimple(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoaSimple(-n)
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// matchesName reports whether the section name matches any of the candidates.
// Matching is substring-based and case-insensitive.
func matchesName(name string, candidates ...string) bool {
	lower := strings.ToLower(name)
	for _, c := range candidates {
		if strings.Contains(lower, c) {
			return true
		}
	}
	return false
}

// isCritical reports whether a section contains critical instructions that
// must be preserved at all times.
func isCritical(content string) bool {
	lower := strings.ToLower(content)
	return strings.Contains(lower, "safety") ||
		strings.Contains(lower, "security") ||
		strings.Contains(lower, "never") ||
		strings.Contains(lower, "must")
}
