package sessiontool

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

// ---- search_sessions tool ---------------------------------------------------

type searchSessionsTool struct {
	sessionDir string
}

// NewSearchSessionsTool creates a tool that searches across saved sessions.
func NewSearchSessionsTool(sessionDir string) *searchSessionsTool {
	return &searchSessionsTool{sessionDir: sessionDir}
}

func (t *searchSessionsTool) Name() string   { return "search_sessions" }
func (t *searchSessionsTool) ReadOnly() bool { return true }

func (t *searchSessionsTool) Description() string {
	return "Search saved AI conversation sessions by keyword. Returns matching session filenames, timestamps, and relevant message excerpts. Use read_session to view a full session. Searches user messages and assistant answers but not tool results."
}

func (t *searchSessionsTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "Search keyword or phrase (case-insensitive). Use specific technical terms for best results."
    },
    "max_results": {
      "type": "integer",
      "description": "Maximum number of matching sessions to return (default 10, max 50)."
    },
    "model": {
      "type": "string",
      "description": "Optional model name filter (e.g. \"deepseek-chat\"). Only search sessions using this model."
    }
  },
  "required": ["query"]
}`)
}

func (t *searchSessionsTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Query      string `json:"query"`
		MaxResults *int   `json:"max_results"`
		Model      string `json:"model"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("search_sessions: invalid args: %w", err)
	}
	if params.Query == "" {
		return "", fmt.Errorf("search_sessions: 'query' argument is required")
	}

	query := strings.ToLower(params.Query)

	maxResults := 10
	if params.MaxResults != nil {
		if *params.MaxResults > 50 {
			maxResults = 50
		} else if *params.MaxResults > 0 {
			maxResults = *params.MaxResults
		}
	}

	ordered, err := agent.ListSessionOrder(t.sessionDir)
	if err != nil {
		return "", fmt.Errorf("search_sessions: %w", err)
	}

	var b strings.Builder
	results := 0

	for _, s := range ordered {
		if results >= maxResults {
			break
		}

		// Filter by model if specified
		if params.Model != "" {
			model, _ := agent.LoadSessionModel(s.Path)
			if !strings.EqualFold(model, params.Model) {
				continue
			}
		}

		ses, err := agent.LoadSession(s.Path)
		if err != nil {
			continue // skip unreadable sessions gracefully
		}
		if agent.IsCleanupPending(s.Path) {
			continue
		}

		msgs := ses.Snapshot()

		match := searchSession(msgs, query)
		if match == "" {
			continue
		}

		results++
		if results == 1 {
			b.WriteString(fmt.Sprintf("# Search results for %q\n\n", params.Query))
		}
		fmt.Fprintf(&b, "### %d. `%s` (%s)\n\n%s\n\n",
			results, filepath.Base(s.Path), s.LastActivityAt.Format("2006-01-02 15:04"), match)
	}

	if results == 0 {
		return fmt.Sprintf("No sessions matched %q.", params.Query), nil
	}
	return b.String(), nil
}

// searchSession scans messages for a query and returns matched excerpts.
func searchSession(msgs []provider.Message, query string) string {
	var excerpts []string

	for _, m := range msgs {
		switch m.Role {
		case provider.RoleUser:
			if content := matchContent(m.Content, query); content != "" {
				excerpts = append(excerpts, "User: "+content)
			}
		case provider.RoleAssistant:
			if m.Content != "" {
				if content := matchContent(m.Content, query); content != "" {
					excerpts = append(excerpts, "Assistant: "+content)
				}
			}
		}
		// Tool results are excluded from search (privacy, matching read_session default)
	}

	if len(excerpts) == 0 {
		return ""
	}
	if len(excerpts) > 3 {
		excerpts = excerpts[:3]
	}
	return strings.Join(excerpts, "\n")
}

// matchContent checks if text contains the query and returns a truncated excerpt.
func matchContent(text, query string) string {
	query = strings.ToLower(query)
	if query == "" {
		return ""
	}
	// Use lowercased text for case-insensitive matching.
	lower := strings.ToLower(text)
	byteIdx := strings.Index(lower, query)
	if byteIdx < 0 {
		return ""
	}

	// Convert byte offset to rune position for correct slicing with multi-byte chars.
	runes := []rune(text)
	matchRune := utf8.RuneCountInString(lower[:byteIdx])

	const windowRunes = 180 // total runes for the excerpt (~60 before + ~120 after)
	start := 0
	if matchRune > int(windowRunes/3) {
		start = matchRune - int(windowRunes/3)
		// Back up to a word boundary.
		for start > 0 && runes[start] != ' ' {
			start--
		}
	}
	end := matchRune + len([]rune(query)) + int(windowRunes*2/3)
	if end > len(runes) {
		end = len(runes)
	}

	excerpt := string(runes[start:end])
	excerpt = strings.ReplaceAll(excerpt, "\n", " ")
	excerpt = strings.TrimSpace(truncateRunes(excerpt, 200))

	if start > 0 {
		excerpt = "..." + excerpt
	}
	if end < len(runes) {
		excerpt = excerpt + "..."
	}
	return excerpt
}
