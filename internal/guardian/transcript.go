package guardian

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"reasonix/internal/provider"
)

// TranscriptEntry is one simplified conversation entry for guardian review.
type TranscriptEntry struct {
	Kind string // "user" | "assistant" | "tool"
	Text string
}

// TranscriptCursor remembers which transcript entries have already been sent to
// the guardian session so subsequent reviews can send only the delta.
type TranscriptCursor struct {
	HistoryVersion int // agent session RewriteVersion at cursor time
	EntryCount     int // how many entries have already been sent
}

const (
	maxMessageEntryTokens = 2000  // per-entry cap for user/assistant
	maxToolEntryTokens    = 1000  // per-entry cap for tool call/result
	maxMessageTranscript  = 10000 // total token budget for user/assistant entries
	maxToolTranscript     = 10000 // total token budget for tool entries
	maxRecentEntries      = 40    // max non-user entries from the tail
)

// ExtractTranscript builds a compact transcript from the agent session messages
// suitable for guardian review. Returns entries in chronological order.
func ExtractTranscript(msgs []provider.Message) []TranscriptEntry {
	var entries []TranscriptEntry
	for _, m := range msgs {
		switch m.Role {
		case provider.RoleSystem:
			// skip — guardian gets its own system prompt
			continue
		case provider.RoleUser:
			if text := strings.TrimSpace(m.Content); text != "" {
				entries = append(entries, TranscriptEntry{Kind: "user", Text: text})
			}
		case provider.RoleAssistant:
			text := m.Content
			if text == "" && len(m.ToolCalls) > 0 {
				// assistant turn that only issued tool calls — include as "tool_calls"
				for _, tc := range m.ToolCalls {
					entries = append(entries, TranscriptEntry{
						Kind: "tool",
						Text: fmt.Sprintf("tool %s call: %s", tc.Name, firstRunesStr(tc.Arguments, 500)),
					})
				}
				continue
			}
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			entries = append(entries, TranscriptEntry{Kind: "assistant", Text: text})
		case provider.RoleTool:
			text := strings.TrimSpace(m.Content)
			if text == "" {
				continue
			}
			label := fmt.Sprintf("tool %s result", m.Name)
			entries = append(entries, TranscriptEntry{Kind: "tool", Text: label + ": " + text})
		}
	}
	return entries
}

// renderTranscript selects and formats entries for guardian prompt inclusion.
// Returns the rendered transcript lines and an omission-note (non-empty when some
// entries were dropped due to budget constraints).
func renderTranscript(entries []TranscriptEntry) ([]string, string) {
	if len(entries) == 0 {
		return []string{"<no retained transcript entries>"}, ""
	}

	// Pre-compute rendered text and estimated token counts for every entry.
	type rendered struct {
		text  string
		index int
		toks  int
	}
	var all []rendered
	for i, e := range entries {
		tokCap := maxMessageEntryTokens
		if e.Kind == "tool" {
			tokCap = maxToolEntryTokens
		}
		text, _ := truncateText(e.Text, tokCap)
		line := fmt.Sprintf("[%d] %s: %s", i+1, e.Kind, text)
		toks := estimateTokens(line)
		all = append(all, rendered{text: line, index: i, toks: toks})
	}

	// Select entries with user-anchored, tool-separated budgets.
	included := make([]bool, len(entries))
	msgToks := 0
	toolToks := 0

	// Find user entry indices.
	var userIdx []int
	for i, e := range entries {
		if e.Kind == "user" {
			userIdx = append(userIdx, i)
		}
	}

	// Always keep the first user entry (anchor).
	if len(userIdx) > 0 && userIdx[0] < len(all) {
		first := userIdx[0]
		included[first] = true
		msgToks += all[first].toks
	}

	// Always keep the last user entry (anchor), if different.
	if len(userIdx) > 1 && userIdx[len(userIdx)-1] != userIdx[0] {
		last := userIdx[len(userIdx)-1]
		if last < len(all) && !included[last] && msgToks+all[last].toks <= maxMessageTranscript {
			included[last] = true
			msgToks += all[last].toks
		}
	}

	// Fill remaining message budget with user entries from newest to oldest.
	for i := len(userIdx) - 1; i >= 0; i-- {
		idx := userIdx[i]
		if idx >= len(all) || included[idx] {
			continue
		}
		if msgToks+all[idx].toks > maxMessageTranscript {
			continue
		}
		included[idx] = true
		msgToks += all[idx].toks
	}

	// Add recent non-user entries from newest to oldest.
	recent := 0
	for i := len(entries) - 1; i >= 0 && recent < maxRecentEntries; i-- {
		if included[i] || entries[i].Kind == "user" {
			continue
		}
		add := all[i].toks
		if entries[i].Kind == "tool" {
			if toolToks+add > maxToolTranscript {
				continue
			}
			toolToks += add
		} else {
			if msgToks+add > maxMessageTranscript {
				continue
			}
			msgToks += add
		}
		included[i] = true
		recent++
	}

	// Build the result.
	var lines []string
	for i, r := range all {
		if included[i] {
			lines = append(lines, r.text)
		}
	}
	omitted := false
	for _, b := range included {
		if !b {
			omitted = true
			break
		}
	}
	if omitted {
		return lines, "Some conversation entries were omitted."
	}
	return lines, ""
}

// FormatTranscript returns a complete guardian transcript prompt block.
func FormatTranscript(entries []TranscriptEntry) string {
	lines, omission := renderTranscript(entries)
	var b strings.Builder
	b.WriteString(">>> TRANSCRIPT START\n")
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString(">>> TRANSCRIPT END\n")
	if omission != "" {
		b.WriteByte('\n')
		b.WriteString(omission)
		b.WriteByte('\n')
	}
	return b.String()
}

// truncateText trims text to roughly tokCap tokens, keeping head and tail.
// Returns the truncated text and whether truncation occurred.
func truncateText(content string, tokCap int) (string, bool) {
	if content == "" {
		return content, false
	}
	est := estimateTokens(content)
	if est <= tokCap {
		return content, false
	}
	// Simple truncation: keep head + tail with marker.
	maxBytes := tokCap * 4 // rough byte estimate
	if len(content) <= maxBytes {
		return content, false
	}
	marker := "<truncated>"
	avail := maxBytes - len(marker)
	if avail <= 0 {
		return marker, true
	}
	head := avail / 2
	tail := avail - head

	// Convert to runes for safe boundary alignment.
	runes := []rune(content)
	// Estimate how many runes fit in head/tail bytes (conservative: assume
	// max 4 bytes per rune).
	headRunes := head / 4
	if headRunes > len(runes) {
		headRunes = len(runes)
	}
	tailRunes := tail / 4
	if tailRunes > len(runes)-headRunes {
		tailRunes = len(runes) - headRunes
	}
	if tailRunes < 0 {
		tailRunes = 0
	}
	return string(runes[:headRunes]) + marker + string(runes[len(runes)-tailRunes:]), true
}

// estimateTokens gives a rough token count for display purposes (not API-accurate).
func estimateTokens(s string) int {
	bytes := len(s)
	runes := utf8.RuneCountInString(s)
	byBytes := (bytes + 3) / 4 // ~4 chars per token for English
	if runes > byBytes {
		return runes
	}
	return byBytes
}
