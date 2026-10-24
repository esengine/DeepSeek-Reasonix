package tui

import (
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
	"slices"
)

func (t *Transcript) foldWorkspaceWait(ev eventwire.Event, level string) bool {
	opening := ev.Code == event.NoticeCodeWorkspaceLease
	if !opening && !closesWorkspaceWait(ev.Code) {
		return false
	}
	id := workspaceWaitID(ev.WorkspaceLease)
	row := Item{Kind: ItemNotice, Level: level, Code: ev.Code, Text: ev.Text, Detail: ev.Detail, WorkspaceLease: ev.WorkspaceLease}
	for i, previous := range slices.Backward(t.Items) {
		if previous.Kind != ItemNotice || workspaceWaitID(previous.WorkspaceLease) != id {
			continue
		}
		if id != "" && closesWorkspaceWait(previous.Code) {
			return true
		}
		if previous.Code == event.NoticeCodeWorkspaceLease && (id != "" || !opening) {
			row.ID = previous.ID
			t.Items[i] = row
			return true
		}
	}
	row.ID = t.id()
	t.Items = append(t.Items, row)
	return true
}

func closesWorkspaceWait(code string) bool {
	return code == event.NoticeCodeWorkspaceLeaseResumed || code == event.NoticeCodeWorkspaceLeaseAbandoned
}

func workspaceWaitID(w *eventwire.WorkspaceLease) string {
	if w == nil {
		return ""
	}
	return w.TransactionID
}
