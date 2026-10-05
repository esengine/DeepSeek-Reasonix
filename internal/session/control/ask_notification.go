package control

import (
	"fmt"
	"strings"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/event"
)

// askNotificationPromptRunes caps the prompt a Notification hook forwards. The
// text leaves the session for whatever channel the hook feeds, and for an MCP
// elicitation it is the server's words, not the model's.
const askNotificationPromptRunes = 120

func askNotificationText(questions []event.AskQuestion, origin *event.AskOrigin) string {
	prompt := ""
	if len(questions) > 0 {
		prompt = questions[0].Prompt
	}
	if origin != nil && origin.URL != "" {
		prompt = "Complete an external interaction"
	} else if origin != nil && origin.Kind == event.AskOriginMCP && strings.TrimSpace(origin.Message) != "" {
		prompt = origin.Message
	}
	prompt = approvalTruncate(approvalCompactText(prompt), askNotificationPromptRunes)
	if origin != nil && origin.Kind == event.AskOriginMCP && origin.Source != "" {
		return fmt.Sprintf(i18n.M.AnswerNeededFromFmt, origin.Source, prompt)
	}
	return fmt.Sprintf(i18n.M.AnswerNeededFmt, prompt)
}
