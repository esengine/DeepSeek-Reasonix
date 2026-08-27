package serve

import (
	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

func historyMessageContent(message provider.Message) string {
	if message.Role == provider.RoleUser {
		return agent.UserMessageText(message)
	}
	return message.Content
}
