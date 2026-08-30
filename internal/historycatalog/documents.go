package historycatalog

import (
	"strings"
	"unicode/utf8"

	"reasonix/internal/provider"
	"reasonix/internal/retrieval"
)

const (
	toolTextMaxBytes  = 8 * 1024
	toolTextHeadBytes = 6 * 1024
	toolTextTailBytes = 2 * 1024
)

// toolTextTruncationMarker makes elided middle bytes recognizable in search
// hits, so truncation is not mistaken for source content.
const toolTextTruncationMarker = "\n…[truncated]…\n"

// truncateToolText bounds one tool payload's indexed text (#8717: tool output
// is the index size driver). The tail survives because tool errors and
// summaries typically sit at the end of the output.
func truncateToolText(text string) string {
	if len(text) <= toolTextMaxBytes {
		return text
	}
	head := text[:toolTextHeadBytes]
	for len(head) > 0 && !utf8.ValidString(head) {
		head = head[:len(head)-1]
	}
	tail := text[len(text)-toolTextTailBytes:]
	for len(tail) > 0 && !utf8.ValidString(tail) {
		tail = tail[1:]
	}
	return head + toolTextTruncationMarker + tail
}

type indexedDocument struct {
	message int
	part    int
	role    string
	kind    string
	tool    string
	terms   string
	count   int
}

func documents(messages []provider.Message) []indexedDocument {
	out := []indexedDocument{}
	appendDoc := func(message, part int, role, kind, tool, text string) {
		terms := retrieval.Tokens(strings.TrimSpace(text))
		if len(terms) == 0 {
			return
		}
		out = append(out, indexedDocument{message: message, part: part, role: role, kind: kind, tool: tool, terms: strings.Join(terms, " "), count: len(terms)})
	}
	for i, msg := range messages {
		switch msg.Role {
		case provider.RoleUser:
			appendDoc(i, 0, string(msg.Role), "user_text", "", msg.Content)
		case provider.RoleAssistant:
			appendDoc(i, 0, string(msg.Role), "assistant_text", "", msg.Content)
			for part, call := range msg.ToolCalls {
				appendDoc(i, part, string(msg.Role), "tool_input", call.Name, truncateToolText(call.Name+" "+call.Arguments))
			}
		case provider.RoleTool:
			// Index both tool_error and tool_output so explicit kind filters stay
			// honest. Default search kinds still exclude tool_output.
			text := truncateToolText(msg.Name + " " + msg.Content)
			lower := strings.ToLower(strings.TrimSpace(msg.Content))
			if strings.HasPrefix(lower, "error:") || strings.HasPrefix(lower, "blocked:") || strings.Contains(lower, "permission denied") {
				appendDoc(i, 0, string(msg.Role), "tool_error", msg.Name, text)
			}
			appendDoc(i, 0, string(msg.Role), "tool_output", msg.Name, text)
		}
	}
	return out
}
