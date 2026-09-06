package main

import (
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func historyLocalOnlyRows(m provider.Message) ([]HistoryMessage, bool) {
	if !m.LocalOnly {
		return nil, false
	}
	if len(m.ProtocolRecovery) > 0 {
		if r, ok := provider.DecodeProtocolRecovery(m.ProtocolRecovery); ok && r.State == "pending" {
			return []HistoryMessage{{Role: "notice", Code: "protocol_recovery", Level: "info", Pending: true, ProtocolRecovery: &provider.ProtocolRecoveryAction{ID: r.ID}}}, true
		}
		return nil, true
	}
	if recovery := m.FinalReadinessRecovery; recovery != nil && recovery.Pending {
		return []HistoryMessage{{
			Role: "notice", Code: event.NoticeCodeFinalReadiness, Level: "info", Pending: true,
			Content: "Task is not complete; continue the remaining work or checks.",
			Readiness: &event.FinalReadiness{
				Attempts: 1,
				Missing:  append([]string(nil), recovery.Missing...),
			},
		}}, true
	}
	return historySteerRows(m.Content, true)
}
