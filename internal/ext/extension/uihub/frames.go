package uihub

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/event"
	"reasonix/internal/ext/extension/protocol"
	"reasonix/internal/ext/pluginpkg"
)

// maxLiveFrames bounds the frame documents one session keeps on screen.
const maxLiveFrames = 6

var (
	ErrFrameLimit = errors.New("ui.frame_limit")
	ErrFrameStale = errors.New("ui.frame_stale")
)

// DeclareFrames records the package a plugin's frame documents are read from.
// It is the package this generation runs, which for a session pinned to a
// composition is a retained copy rather than the installed one.
func (h *Hub) DeclareFrames(pluginID string, pkg pluginpkg.Package) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(pkg.Manifest.Frames) == 0 {
		delete(h.frames, pluginID)
		return
	}
	h.frames[pluginID] = pkg
}

// DrawsFrames tells the sidecar this host accepts frame surfaces; a frontend
// that cannot draw one shows its fallback.
func (binding) DrawsFrames() bool { return true }

// FrameDocument returns one declared frame document of a live plugin in the
// current generation.
func (h *Hub) FrameDocument(pluginID, frameID string, generation uint64) ([]byte, error) {
	h.mu.Lock()
	pkg, declared := h.frames[pluginID]
	stale := generation != h.generation || h.owner.Gate.IsStale(generation) || h.crashed[pluginID] || !h.known[pluginID]
	h.mu.Unlock()
	if stale {
		return nil, ErrFrameStale
	}
	if !declared {
		return nil, fmt.Errorf("%w: plugin %q", pluginpkg.ErrFrameNotDeclared, pluginID)
	}
	return pkg.FrameDocument(frameID)
}

// admitFrameLocked shares the surface write's lock, including kind transitions.
func (h *Hub) admitFrameLocked(p *event.ExtensionSurfacePayload) error {
	if p.Frame == nil {
		return nil
	}
	pkg, ok := h.frames[p.PluginID]
	if _, declared := pkg.Frame(p.Frame.Frame); !ok || !declared {
		return &protocol.ProtocolError{
			Reason:  protocol.ErrCapabilityNotDeclared,
			Message: "frame " + p.Frame.Frame + " is not declared by plugin " + p.PluginID,
		}
	}
	key := p.PluginID + "\x00" + p.SurfaceID
	live := 0
	for id, record := range h.surfaces {
		var kind struct{ Kind, Code string }
		if id != key && record.session == h.sessionID && record.generation == h.generation && json.Unmarshal(record.data, &kind) == nil && kind.Kind == event.ExtensionSurfaceFrame && kind.Code == "" {
			live++
		}
	}
	if live >= maxLiveFrames {
		return ErrFrameLimit
	}
	return nil
}

func frameView(payload protocol.UIFramePayload) (*event.ExtensionFrameView, error) {
	props, err := redactJSONStrings(payload.Props)
	if err != nil {
		return nil, err
	}
	return &event.ExtensionFrameView{
		Frame:    payload.Frame,
		Slot:     payload.Slot,
		Title:    secrets.RedactCredentials(payload.Title),
		Fallback: secrets.RedactCredentials(payload.Fallback),
		Props:    props,
	}, nil
}

// redactJSONStrings redacts every string value in a JSON document; keys stay,
// because they are how the document reads its own data.
func redactJSONStrings(raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return json.Marshal(redactValue(v))
}

func redactValue(v any) any {
	switch t := v.(type) {
	case string:
		return secrets.RedactCredentials(t)
	case []any:
		for i := range t {
			t[i] = redactValue(t[i])
		}
	case map[string]any:
		for k := range t {
			t[k] = redactValue(t[k])
		}
	}
	return v
}
