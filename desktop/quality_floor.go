package main

import (
	"strings"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/worktree"
)

// Session-scoped quality floor: SetQualityFloorForTab is the single write
// path; derivedQualityFloor is the read model where facts may outrank the
// recorded choice, mirroring the fact-driven contract.

// SetQualityFloor applies the floor to the active tab.
func (a *App) SetQualityFloor(floor string) error {
	return a.SetQualityFloorForTab("", floor)
}

// SetQualityFloorForTab updates the tab's floor and pushes it to the
// controller between turns. Failures return error so the Wails Promise
// rejects; an unknown value never reaches the controller.
func (a *App) SetQualityFloorForTab(tabID, floor string) error {
	normalized, err := control.NormalizeQualityFloor(floor)
	if err != nil {
		return err
	}
	tab := a.tabByID(tabID)
	if tab == nil {
		return a.workspaceNotReadyErr(nil)
	}
	tab.turnStartMu.Lock()
	defer tab.turnStartMu.Unlock()
	a.mu.Lock()
	if a.tabs[tab.ID] != tab {
		a.mu.Unlock()
		return a.workspaceNotReadyErr(nil)
	}
	tab.qualityFloor = normalized
	ctrl := tab.Ctrl
	tabIDForSave := tab.ID
	a.mu.Unlock()
	if ctrl != nil {
		if err := ctrl.SetQualityFloor(normalized); err != nil {
			return err
		}
	}
	a.mu.Lock()
	if a.tabs[tabIDForSave] == tab {
		a.saveTabsLocked()
	}
	a.mu.Unlock()
	return nil
}

// derivedFloor is the effective floor plus whether facts — not the user's
// choice — put the session at the delivery level. isolated carries the
// worktree predicate so callers that need both do the path math once.
type derivedFloor struct {
	floor    string
	inferred bool
	isolated bool
}

// ctrlQualityFloor reads a controller's floor. An empty answer means the
// controller has no opinion yet and the recorded tab value wins.
func ctrlQualityFloor(ctrl control.SessionAPI) (string, bool) {
	if ctrl == nil {
		return "", false
	}
	floor := ctrl.QualityFloor()
	return floor, floor != ""
}

// derivedQualityFloor resolves the effective floor for display: an explicit
// delivery choice wins; otherwise an isolated-worktree tab or an active
// delivery-gated session infers delivery. Standard is the default.
func derivedQualityFloor(tab *WorkspaceTab) derivedFloor {
	if tab == nil {
		return derivedFloor{floor: control.QualityFloorStandard}
	}
	isolated := worktree.IsManagedPath(tab.WorkspaceRoot, config.DeliveryWorktreeDir())
	if strings.TrimSpace(tab.qualityFloor) == control.QualityFloorDelivery {
		return derivedFloor{floor: control.QualityFloorDelivery, isolated: isolated}
	}
	if isolated {
		return derivedFloor{floor: control.QualityFloorDelivery, inferred: true, isolated: true}
	}
	if floor, ok := ctrlQualityFloor(tab.Ctrl); ok && floor == control.QualityFloorDelivery {
		return derivedFloor{floor: control.QualityFloorDelivery, inferred: true}
	}
	return derivedFloor{floor: control.QualityFloorStandard}
}

// tabQualityFloor seeds a new tab's recorded floor. Isolated worktrees are
// left empty — derivedQualityFloor infers delivery for them at read time —
// so the UI keeps the "(inferred)" distinction until the user chooses.
// Other tabs inherit the sibling tab's explicit delivery choice.
func tabQualityFloor(workspaceRoot string, siblingExplicit string) string {
	if worktree.IsManagedPath(workspaceRoot, config.DeliveryWorktreeDir()) {
		return ""
	}
	if strings.TrimSpace(siblingExplicit) == control.QualityFloorDelivery {
		return control.QualityFloorDelivery
	}
	return control.QualityFloorStandard
}

// applyTabQualityFloorToController pushes the recorded floor (or the
// worktree-inferred one) onto a controller before it takes over a session.
func applyTabQualityFloorToController(ctrl control.SessionAPI, floor string) {
	if ctrl == nil {
		return
	}
	if strings.TrimSpace(floor) == "" {
		return
	}
	if _, err := control.NormalizeQualityFloor(floor); err != nil {
		return
	}
	_ = ctrl.SetQualityFloor(floor)
}

// currentTabTokenMode returns the dual-write compat label derived from the
// tab's quality floor: delivery writes "delivery", standard writes "full".
func currentTabTokenMode(tab *WorkspaceTab) string {
	return tokenModeForFloor(derivedQualityFloor(tab).floor)
}

// currentTabAgentPreset returns the role label derived from the quality floor.
func currentTabAgentPreset(tab *WorkspaceTab) string {
	return agentPresetForFloor(derivedQualityFloor(tab).floor)
}

// tokenModeForFloor and agentPresetForFloor map an already-derived floor onto
// the compat labels, so callers holding a derivedFloor skip the path math.
func tokenModeForFloor(floor string) string {
	if floor == control.QualityFloorDelivery {
		return boot.TokenModeDelivery
	}
	return boot.TokenModeFull
}

func agentPresetForFloor(floor string) string {
	if floor == control.QualityFloorDelivery {
		return boot.AgentPresetDelivery
	}
	return boot.AgentPresetStandard
}

// qualityFloorSafe reads the recorded floor; nil-safe for lookups.
func (t *WorkspaceTab) qualityFloorSafe() string {
	if t == nil {
		return ""
	}
	return t.qualityFloor
}

// firstCtrlFloor prefers the controller's live floor, falling back to the
// recorded value when the controller cannot answer.
func firstCtrlFloor(ctrl control.SessionAPI, fallback string) string {
	if floor, ok := ctrlQualityFloor(ctrl); ok {
		return floor
	}
	return fallback
}
