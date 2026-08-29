package main

import (
	"fmt"
	"strings"
	"time"
)

// AcceptDeliveryToTab clears an awaiting_delivery ("待完成验证") tab activity
// state without starting a model turn. This gives the user an explicit,
// model-free way to dismiss the "待完成验证" label when they consider the task
// acceptable. (Issue #9036 — Phase 1 minimal + Phase 2 lightweight trace.)
//
// Phase 2 lightweight: after clearing, emit a "delivery:accepted" runtime
// event (tabId + timestamp) as a desktop-visible, traceable record. We do NOT
// write a durable evidence-ledger receipt or change FinalReadiness semantics —
// that deeper integration is intentionally deferred to keep the fix low-risk.
func (a *App) AcceptDeliveryToTab(tabID string) error {
	if strings.TrimSpace(tabID) == "" {
		return fmt.Errorf("tab id is required")
	}
	if a.clearTabActivityStatusIf(tabID, topicStatusAwaitingDelivery) {
		a.emitProjectTreeRuntimeChangedWithLegacy()
		a.emitRuntimeEvent("delivery:accepted", map[string]string{
			"tabId":      tabID,
			"acceptedAt": time.Now().Format(time.RFC3339Nano),
		})
	}
	return nil
}

// clearTabActivityStatusIf resets a tab's activity status to "" when it
// matches the wanted value. It is idempotent and safe to call for tabs in any
// state; it only changes awaiting_delivery tabs.
func (a *App) clearTabActivityStatusIf(tabID, want string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	tab := a.tabByEventSinkIDLocked(tabID)
	if tab == nil {
		return false
	}
	if want != "" && tab.ActivityStatus != want {
		return false
	}
	if tab.ActivityStatus == "" {
		return false
	}
	tab.ActivityStatus = ""
	return true
}
