package main

import (
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

func systemPromptFrom(messages []provider.Message) string {
	for _, m := range messages {
		if m.Role == provider.RoleSystem {
			return m.Content
		}
	}
	return ""
}

func withFreshSystemPrompt(messages []provider.Message, system string) []provider.Message {
	if strings.TrimSpace(system) == "" {
		return messages
	}
	out := append([]provider.Message(nil), messages...)
	for i := range out {
		if out[i].Role == provider.RoleSystem {
			out[i].Content = system
			out[i].ReasoningContent = ""
			out[i].ReasoningSignature = ""
			out[i].ToolCalls = nil
			out[i].ToolCallID = ""
			out[i].Name = ""
			return out
		}
	}
	return append([]provider.Message{{Role: provider.RoleSystem, Content: system}}, out...)
}

func sessionWithFreshSystemPrompt(session *agent.Session, system string) *agent.Session {
	if session == nil {
		return nil
	}
	messages := session.Snapshot()
	if systemPromptFrom(messages) == "" {
		return session
	}
	return &agent.Session{Messages: withFreshSystemPrompt(messages, system)}
}

func resumeWithFreshSystemPrompt(ctrl interface {
	History() []provider.Message
	Resume(*agent.Session, string)
	SetSessionPath(string)
}, messages []provider.Message, path string) {
	if ctrl == nil {
		return
	}
	if len(messages) > 0 {
		ctrl.Resume(&agent.Session{Messages: withFreshSystemPrompt(messages, systemPromptFrom(ctrl.History()))}, path)
		return
	}
	if path != "" {
		ctrl.SetSessionPath(path)
	}
}
