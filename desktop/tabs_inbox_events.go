package main

import "reasonix/internal/sessioninbox"

// InboxChanged forwards Store revision notifications through the tab's
// ordered, non-blocking runtime emitter. A late callback from an old session is
// harmless because sessionPath lets the frontend reject the stale scope.
func (s *tabEventSink) InboxChanged(snap sessioninbox.InboxSnapshot) {
	if s == nil {
		return
	}
	tabID, _ := s.binding()
	s.emitRuntimeEvent("InboxChanged", inboxChangedView{
		TabID:       tabID,
		SessionPath: snap.SessionPath,
		Revision:    snap.Revision,
	})
}
