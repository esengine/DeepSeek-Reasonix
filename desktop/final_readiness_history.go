package main

import (
	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func historyLocalOnlyRows(m provider.Message) ([]HistoryMessage, bool) {
	if !m.LocalOnly {
		return nil, false
	}
	if recovery := m.FinalReadinessRecovery; recovery != nil && recovery.Pending {
		return []HistoryMessage{{
			Role: "notice", Code: event.NoticeCodeFinalReadiness, Level: "info", Pending: true,
			Content: "Task status needs one more check; continue the remaining work.",
			Readiness: &event.FinalReadiness{
				Attempts: 1,
				Missing:  append([]string(nil), recovery.Missing...),
			},
		}}, true
	}
	return historySteerRows(agent.UserMessageText(m), true)
}
