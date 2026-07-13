package main

import (
	"log/slog"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

// globalCacheGuardian 全局缓存守护者（在 startup 中设置）
var globalCacheGuardian *CachePrefixGuardian

func systemPromptFrom(messages []provider.Message) string {
	for _, m := range messages {
		if m.Role == provider.RoleSystem {
			return m.Content
		}
	}
	return ""
}

// logSystemPromptSwap leaves a trace whenever a resume/rebind replaces a
// conversation's persisted system prompt with different bytes: that swap
// invalidates the whole conversation's provider prefix cache (misses bill at
// 10x hits) and persists the rewrite. With probe snapshots keeping composition
// deterministic, this should fire only on genuine config changes — if it shows
// up in field logs without one, a new nondeterminism source crept into the
// prompt assembly.
func logSystemPromptSwap(persisted, fresh, path string) {
	if persisted == "" || fresh == "" || persisted == fresh {
		return
	}
	slog.Warn("desktop: resume swapped a differing system prompt; conversation prefix cache will miss",
		"path", path, "persisted_len", len(persisted), "fresh_len", len(fresh))
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
	persisted := systemPromptFrom(messages)
	if persisted == "" {
		return session
	}
	// OPT-12+: 使用缓存守护者检查是否可以避免 swap
	// 如果差异只是空白符或行尾格式，使用 persisted 保持缓存命中
	if globalCacheGuardian != nil && !globalCacheGuardian.GuardSystemPromptSwap("", persisted, system) {
		logSystemPromptSwap(persisted, system, "")
	} else if persisted != system {
		// 守护者认为可以避免，但仍记录 info 级日志
		slog.Info("cache guardian: preserving persisted system prompt to maintain cache hit",
			"persisted_len", len(persisted), "fresh_len", len(system))
		return session // 保持 persisted，不 swap
	}
	return session.CloneWithMessages(withFreshSystemPrompt(messages, system))
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
		fresh := systemPromptFrom(ctrl.History())
		logSystemPromptSwap(systemPromptFrom(messages), fresh, path)
		next := withFreshSystemPrompt(messages, fresh)
		if path != "" {
			if loaded, err := agent.LoadSession(path); err == nil && loaded != nil {
				if resumed, ok := loaded.CloneWithMessagesIfCompatible(next); ok {
					ctrl.Resume(resumed, path)
					return
				}
			}
		}
		ctrl.Resume(agent.NewSession("").CloneWithMessages(next), path)
		return
	}
	if path != "" {
		ctrl.SetSessionPath(path)
	}
}
