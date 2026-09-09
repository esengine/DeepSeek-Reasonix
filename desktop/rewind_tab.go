package main

import (
	"strings"

	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
)

// CommitRewindForTab executes prepare (if planID empty) then commit immediately.
func (a *App) CommitRewindForTab(tabID, planID string, turn int, scope string) RewindResultView {
	tab, ctrl := a.tabAndCtrlByID(tabID)
	if a.tabIsReadOnly(tab) {
		return RewindResultView{OK: false, Error: readOnlyChannelErr().Error()}
	}
	if ctrl == nil {
		return RewindResultView{OK: false, Error: "no controller"}
	}
	s := control.RewindBoth
	switch scope {
	case "code":
		s = control.RewindCode
	case "conversation":
		s = control.RewindConversation
	}
	if planID == "" {
		plan, err := ctrl.PrepareRewind(turn, s)
		if err != nil {
			return RewindResultView{OK: false, Error: err.Error()}
		}
		// Conversation-only is allowed when its boundary is valid. File scopes
		// never fall back to the legacy force-restore path.
		if s == control.RewindConversation {
			if !plan.CanConversation {
				return RewindResultView{OK: false, Error: nonEmptyStr(plan.DisabledReason, "conversation rewind unavailable")}
			}
		} else if !plan.CanFiles {
			return RewindResultView{OK: false, Error: nonEmptyStr(plan.DisabledReason, "file rewind unavailable"), Conflicts: conflictStrings(plan), Coverage: string(plan.Coverage)}
		}
		planID = plan.PlanID
	}
	_, inPlace := ctrl.SessionHead()
	var result checkpoint.RewindResult
	var err error
	if inPlace {
		result, err = ctrl.CommitRewindInPlace(planID)
	} else {
		result, err = ctrl.CommitRewind(planID)
	}
	view := rewindResultToView(result)
	if err != nil {
		view.OK = false
		if view.Error == "" {
			view.Error = err.Error()
		}
		return view
	}
	if view.OK && view.ConversationForked && strings.TrimSpace(view.Branch) != "" && tab != nil {
		if inPlace {
			meta := a.tabMetaAfterHeadSwitch(tab)
			view.TabID, view.Tab = meta.ID, &meta
		} else {
			view = a.attachForkedRewindTab(tab, view)
		}
	}
	return view
}

// UndoRewindForTab undoes the last successful rewind on the tab when available.
func (a *App) UndoRewindForTab(tabID, transactionID string) RewindResultView {
	tab, ctrl := a.tabAndCtrlByID(tabID)
	if a.tabIsReadOnly(tab) {
		return RewindResultView{OK: false, Error: readOnlyChannelErr().Error()}
	}
	if ctrl == nil {
		return RewindResultView{OK: false, Error: "no controller"}
	}
	before, _ := ctrl.SessionHead()
	result, err := ctrl.UndoRewind(transactionID)
	view := rewindResultToView(result)
	if err != nil {
		view.OK = false
		if view.Error == "" {
			view.Error = err.Error()
		}
	}
	if after, ok := ctrl.SessionHead(); ok && after.HeadID != before.HeadID && tab != nil {
		meta := a.tabMetaAfterHeadSwitch(tab)
		view.TabID, view.Tab = meta.ID, &meta
	}
	return view
}
