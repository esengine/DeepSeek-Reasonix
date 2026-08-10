package main

import (
	"reasonix/internal/boot"
	"reasonix/internal/control"
)

func rebuildTabRuntime(a *App, tab *WorkspaceTab, old *control.Controller, opts boot.Options) (*boot.BuildResult, error) {
	var res *boot.BuildResult
	var err error
	previous := a.tabBuildResultForController(tab, old)
	if previous != nil {
		res, err = boot.RebuildFrom(a.bootContext(), previous, opts)
	} else {
		res, err = boot.Rebuild(a.bootContext(), old, opts)
	}
	if err != nil {
		return nil, err
	}
	a.setTabLastBuildResult(tab, res)
	return res, nil
}

func (a *App) tabBuildResultForController(tab *WorkspaceTab, ctrl *control.Controller) *boot.BuildResult {
	if a == nil || tab == nil || ctrl == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if tab.ID != "" && a.tabs[tab.ID] != tab {
		return nil
	}
	res := tab.lastBuildResult
	if res == nil || res.Controller != ctrl || tab.Ctrl != ctrl {
		return nil
	}
	return res
}

func (a *App) setTabLastBuildResult(tab *WorkspaceTab, res *boot.BuildResult) {
	if a == nil || tab == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if tab.ID != "" && a.tabs[tab.ID] != tab {
		return
	}
	tab.lastBuildResult = res
}

// RuntimeDoctorReport is the desktop/Wails view of extension runtime diagnostics.
type RuntimeDoctorReport struct {
	Text                  string `json:"text"`
	PublishedGen          uint64 `json:"publishedGeneration"`
	AllowResume           bool   `json:"allowResume"`
	CleanRollback         bool   `json:"cleanRollback"`
	HasIrreversible       bool   `json:"hasIrreversible"`
	NoOpRebuilds          uint64 `json:"noOpRebuilds"`
	FullRebuilds          uint64 `json:"fullRebuilds"`
	SubgraphRebuilds      uint64 `json:"subgraphRebuilds"`
	StaleDrops            uint64 `json:"staleDrops"`
	AdmissionRejected     uint64 `json:"admissionRejected"`
	RuntimeOwnerFallbacks uint64 `json:"runtimeOwnerFallbacks"`
}

// RuntimeDoctor returns process-wide + active-tab extension runtime diagnostics
// for the settings/status panel (mirrors `reasonix doctor runtime`).
func (a *App) RuntimeDoctor() RuntimeDoctorReport {
	var res *boot.BuildResult
	if a != nil {
		// Snapshot the active tab and build result under the same lock: Wails can
		// query diagnostics while another goroutine finishes a runtime rebuild.
		a.mu.RLock()
		if tab := a.activeTabLocked(); tab != nil {
			candidate := tab.lastBuildResult
			if candidate != nil && candidate.Controller == tab.Ctrl {
				res = candidate
			}
		}
		a.mu.RUnlock()
	}
	report := boot.CollectRuntimeDoctor(res)
	return RuntimeDoctorReport{
		Text:                  boot.RenderRuntimeDoctorText(report),
		PublishedGen:          report.PublishedGen,
		AllowResume:           report.Resume.AllowResume,
		CleanRollback:         report.Resume.CleanRollback,
		HasIrreversible:       report.Resume.HasIrreversible,
		NoOpRebuilds:          report.Metrics.NoOpRebuilds,
		FullRebuilds:          report.Metrics.FullRebuilds,
		SubgraphRebuilds:      report.Metrics.SubgraphRebuilds,
		StaleDrops:            report.Metrics.StaleDrops,
		AdmissionRejected:     report.Metrics.AdmissionRejected,
		RuntimeOwnerFallbacks: report.RuntimeOwnerFallbacks,
	}
}
