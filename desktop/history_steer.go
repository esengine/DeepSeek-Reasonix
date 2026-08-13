package main

import (
	"reasonix/internal/agent"
	"reasonix/internal/event"
)

func historySteerRows(content string, unapplied bool) ([]HistoryMessage, bool) {
	text, handled := agent.ReplaySteerText(content)
	if !handled {
		return nil, false
	}
	if text == "" {
		return nil, true
	}
	if unapplied {
		return []HistoryMessage{{
			Role:    "notice",
			Content: agent.UnappliedSteerNotice(text),
			Code:    event.NoticeCodeUnappliedSteer,
			Level:   "warn",
		}}, true
	}
	return []HistoryMessage{{Role: "notice", Content: "↪ " + text}}, true
}
