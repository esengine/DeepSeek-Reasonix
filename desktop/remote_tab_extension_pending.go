package main

import (
	"bytes"
	"encoding/json"
	"strings"
)

const remotePendingExtensionFormKey = "extension_surface:form"

// cacheRemotePendingExtensionForm retains the latest actionable form while a
// remote tab has no mounted frontend listener. Other extension publications
// remain ordinary stream history; the shared reducer only exposes one pending
// form at a time, so the newest form replaces the previous one here as well.
func (a *App) cacheRemotePendingExtensionForm(tabID string, gen uint64, frame json.RawMessage) bool {
	var probe struct {
		Extension *struct {
			PluginID  string          `json:"pluginId"`
			SurfaceID string          `json:"surfaceId"`
			Kind      string          `json:"kind"`
			Form      json.RawMessage `json:"form"`
		} `json:"extension"`
	}
	if json.Unmarshal(frame, &probe) != nil || probe.Extension == nil ||
		probe.Extension.Kind != "form" || len(probe.Extension.Form) == 0 || bytes.Equal(bytes.TrimSpace(probe.Extension.Form), []byte("null")) ||
		strings.TrimSpace(probe.Extension.PluginID) == "" || strings.TrimSpace(probe.Extension.SurfaceID) == "" {
		return false
	}
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.gen != gen {
		a.remoteTabMu.Unlock()
		return false
	}
	tab.runtime.revision++
	if tab.pendingEvents == nil {
		tab.pendingEvents = make(map[string]json.RawMessage)
	}
	tab.pendingEvents[remotePendingExtensionFormKey] = append(json.RawMessage(nil), frame...)
	tab.runtime.pendingPrompt = true
	tab.runtime.cancellable = true
	meta := remoteTabMetaLocked(tab)
	a.remoteTabMu.Unlock()
	a.emitRemoteEvent("remote-tab:updated", meta)
	return true
}

func (a *App) clearRemotePendingExtensionForm(tabID, pluginID, surfaceID string) {
	a.remoteTabMu.Lock()
	var meta TabMeta
	changed := false
	if tab := a.remoteTabs[tabID]; tab != nil {
		frame := tab.pendingEvents[remotePendingExtensionFormKey]
		var probe struct {
			Extension *struct {
				PluginID  string `json:"pluginId"`
				SurfaceID string `json:"surfaceId"`
			} `json:"extension"`
		}
		if json.Unmarshal(frame, &probe) == nil && probe.Extension != nil &&
			probe.Extension.PluginID == pluginID && probe.Extension.SurfaceID == surfaceID {
			tab.runtime.revision++
			delete(tab.pendingEvents, remotePendingExtensionFormKey)
			pending := len(tab.pendingEvents) > 0
			changed = tab.runtime.pendingPrompt != pending
			tab.runtime.pendingPrompt = pending
			meta = remoteTabMetaLocked(tab)
		}
	}
	a.remoteTabMu.Unlock()
	if changed {
		a.emitRemoteEvent("remote-tab:updated", meta)
	}
}
