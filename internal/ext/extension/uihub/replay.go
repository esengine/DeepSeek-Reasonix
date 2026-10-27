package uihub

import (
	"encoding/json"
	"errors"
	"sort"

	"reasonix/internal/contract/event"
)

const maxRememberedSurfaces = 64

var ErrSurfaceLimit = errors.New("surface.limit_exceeded")

type surfaceRecord struct {
	plugin, session string
	generation      uint64
	data            json.RawMessage
}

func (h *Hub) rememberSurface(p *event.ExtensionSurfacePayload) (bool, error) {
	var data json.RawMessage
	if p.Kind != event.ExtensionSurfaceNotification {
		var err error
		data, err = json.Marshal(p)
		if err != nil {
			return false, err
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if p.SessionID != h.sessionID || p.Generation != h.generation || h.crashed[p.PluginID] || h.owner.Gate.IsStale(p.Generation) {
		return false, nil
	}
	if err := h.admitFrameLocked(p); err != nil {
		return false, err
	}
	if p.Kind == event.ExtensionSurfaceNotification {
		return true, nil
	}
	key := p.PluginID + "\x00" + p.SurfaceID
	if _, exists := h.surfaces[key]; !exists && len(h.surfaces) >= maxRememberedSurfaces {
		return false, ErrSurfaceLimit
	}
	h.surfaces[key] = surfaceRecord{p.PluginID, p.SessionID, p.Generation, data}
	return true, nil
}

// Surfaces returns isolated copies; retired generations expose identity only.
func (h *Hub) Surfaces() []event.ExtensionSurfacePayload {
	h.mu.Lock()
	defer h.mu.Unlock()
	keys := make([]string, 0, len(h.surfaces))
	for k, p := range h.surfaces {
		if p.session == h.sessionID {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]event.ExtensionSurfacePayload, 0, len(keys))
	for _, k := range keys {
		record := h.surfaces[k]
		var p event.ExtensionSurfacePayload
		if json.Unmarshal(record.data, &p) != nil {
			continue
		}
		if record.generation != h.generation || h.crashed[record.plugin] || h.owner.Gate.IsStale(record.generation) {
			p = event.ExtensionSurfacePayload{Code: event.SurfaceExpired, PluginID: p.PluginID, SurfaceID: p.SurfaceID, SessionID: p.SessionID, Generation: p.Generation, Kind: p.Kind}
		}
		out = append(out, p)
	}
	return out
}

func (h *Hub) expireSurfacesLocked(plugin string) {
	for key, record := range h.surfaces {
		if plugin != "" && record.plugin != plugin {
			continue
		}
		var p event.ExtensionSurfacePayload
		if json.Unmarshal(record.data, &p) != nil {
			continue
		}
		p = event.ExtensionSurfacePayload{Code: event.SurfaceExpired, PluginID: p.PluginID, SurfaceID: p.SurfaceID, SessionID: p.SessionID, Generation: p.Generation, Kind: p.Kind}
		record.data, _ = json.Marshal(p)
		h.surfaces[key] = record
	}
}
