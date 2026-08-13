package historycatalog

import (
	"strings"

	"reasonix/internal/provider"
	"reasonix/internal/retrieval"
)

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
				appendDoc(i, part, string(msg.Role), "tool_input", call.Name, call.Name+" "+call.Arguments)
			}
		case provider.RoleTool:
			// Index both tool_error and tool_output so explicit kind filters stay
			// honest. Default search kinds still exclude tool_output.
			text := msg.Name + " " + msg.Content
			lower := strings.ToLower(strings.TrimSpace(msg.Content))
			if strings.HasPrefix(lower, "error:") || strings.HasPrefix(lower, "blocked:") || strings.Contains(lower, "permission denied") {
				appendDoc(i, 0, string(msg.Role), "tool_error", msg.Name, text)
			}
			appendDoc(i, 0, string(msg.Role), "tool_output", msg.Name, text)
		}
	}
	return out
}
