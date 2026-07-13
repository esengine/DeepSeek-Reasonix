package agent

import (
	"strings"
	"sync"

	"reasonix/internal/provider"
)

// SerializerStats holds statistics about token-efficient serialization
// operations performed by TokenEfficientSerializer.
type SerializerStats struct {
	TotalSerialized int
	TokensSaved     int
}

// maxSerializeContentLen is the length, in characters, above which an
// individual message's content is truncated during serialization.
const maxSerializeContentLen = 1000

// truncatedContentLen is the number of characters kept from the head of an
// over-long message before appending the ellipsis marker.
const truncatedContentLen = 500

// TokenEfficientSerializer reduces token usage by serializing conversation
// messages into a compact, single-line representation: roles are abbreviated
// to single letters and content is trimmed and truncated, all joined by a
// pipe separator. The process is reversible via DeserializeMessages.
type TokenEfficientSerializer struct {
	mu                sync.RWMutex
	totalSerialized   int
	tokensSaved       int
	roleAbbreviations map[provider.Role]string
}

// NewTokenEfficientSerializer creates a new TokenEfficientSerializer with
// the standard role-to-abbreviation mapping initialized:
//
//	provider.RoleSystem    → "S"
//	provider.RoleUser      → "U"
//	provider.RoleAssistant → "A"
//	provider.RoleTool      → "T"
func NewTokenEfficientSerializer() *TokenEfficientSerializer {
	return &TokenEfficientSerializer{
		roleAbbreviations: map[provider.Role]string{
			provider.RoleSystem:    "S",
			provider.RoleUser:      "U",
			provider.RoleAssistant: "A",
			provider.RoleTool:      "T",
		},
	}
}

// SerializeMessages serializes the provided messages into a compact, pipe-
// separated string of the form "S:system content|U:user content|A:assistant
// content". Each message's content is trimmed of leading/trailing whitespace
// and, when it exceeds maxSerializeContentLen characters, truncated to
// truncatedContentLen characters plus "...". Cumulative statistics
// (TotalSerialized and TokensSaved) are updated to reflect the savings.
func (s *TokenEfficientSerializer) SerializeMessages(messages []provider.Message) string {
	parts := make([]string, 0, len(messages))
	origTokens := 0

	for _, msg := range messages {
		origTokens += s.EstimateSerializedTokens(msg.Content)

		abbr := s.roleAbbreviations[msg.Role]
		if abbr == "" {
			abbr = string(msg.Role)
		}

		content := strings.TrimSpace(msg.Content)
		if len(content) > maxSerializeContentLen {
			content = content[:truncatedContentLen] + "..."
		}

		parts = append(parts, abbr+":"+content)
	}

	result := strings.Join(parts, "|")

	newTokens := s.EstimateSerializedTokens(result)
	saved := origTokens - newTokens
	if saved < 0 {
		saved = 0
	}

	s.mu.Lock()
	s.totalSerialized++
	s.tokensSaved += saved
	s.mu.Unlock()

	return result
}

// EstimateSerializedTokens returns a rough estimate of the token count for
// the given text using the len(text)/4 heuristic.
func (s *TokenEfficientSerializer) EstimateSerializedTokens(text string) int {
	return len(text) / 4
}

// DeserializeMessages reverses SerializeMessages, parsing a compact,
// pipe-separated string back into a slice of provider.Message. Each segment
// is expected to be of the form "ROLE_ABBR:content" (split on the first
// colon so content may itself contain colons). Unknown abbreviations fall
// back to the user role. An empty input yields nil.
func (s *TokenEfficientSerializer) DeserializeMessages(serialized string) []provider.Message {
	if serialized == "" {
		return nil
	}

	abbrToRole := make(map[string]provider.Role, len(s.roleAbbreviations))
	for role, abbr := range s.roleAbbreviations {
		abbrToRole[abbr] = role
	}

	segments := strings.Split(serialized, "|")
	messages := make([]provider.Message, 0, len(segments))

	for _, seg := range segments {
		if seg == "" {
			continue
		}
		abbr, content, found := strings.Cut(seg, ":")
		role := provider.RoleUser
		if found {
			if r, ok := abbrToRole[abbr]; ok {
				role = r
			}
		} else {
			// No colon: treat the whole segment as content under the user role.
			content = abbr
		}
		messages = append(messages, provider.Message{
			Role:    role,
			Content: content,
		})
	}

	return messages
}

// CompactContent collapses excessive whitespace within content: runs of
// spaces and tabs are reduced to a single space, consecutive blank lines are
// removed, and the result is trimmed of leading/trailing whitespace.
func (s *TokenEfficientSerializer) CompactContent(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = collapseSerializeSpaces(line)
	}

	var builder strings.Builder
	builder.Grow(len(content))
	blankRun := 0

	for i, line := range lines {
		isBlank := strings.TrimSpace(line) == ""
		if isBlank {
			blankRun++
			if blankRun > 1 {
				continue
			}
		} else {
			blankRun = 0
		}

		builder.WriteString(line)
		if i < len(lines)-1 {
			builder.WriteByte('\n')
		}
	}

	return strings.TrimSpace(builder.String())
}

// GetStats returns a snapshot of the serializer's cumulative statistics.
func (s *TokenEfficientSerializer) GetStats() SerializerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return SerializerStats{
		TotalSerialized: s.totalSerialized,
		TokensSaved:     s.tokensSaved,
	}
}

// Reset clears all accumulated statistics, returning the serializer to its
// initial state.
func (s *TokenEfficientSerializer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totalSerialized = 0
	s.tokensSaved = 0
}

// collapseSerializeSpaces reduces every run of spaces and tabs in s to a
// single space. Other whitespace (such as newlines) is left untouched; the
// caller handles blank-line collapsing separately.
func collapseSerializeSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if !inSpace {
				b.WriteByte(' ')
				inSpace = true
			}
			continue
		}
		inSpace = false
		b.WriteRune(r)
	}
	return b.String()
}
