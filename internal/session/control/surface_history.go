package control

import (
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/surfacespec"
	"reasonix/internal/ext/extension/uihub"
)

func (c *Controller) NativeSurfaces() bool {
	if c == nil {
		return false
	}
	registry := c.mcp.registry()
	if registry == nil {
		return false
	}
	_, enabled := registry.Get(surfacespec.ToolName)
	return enabled
}

func (c *Controller) ExtensionSurfaces() []event.ExtensionSurfacePayload {
	if h := c.extensionUIHub(); h != nil {
		return h.Surfaces()
	}
	return nil
}

// ExtensionFrameDocument reads one frame document a live plugin of the current
// generation declares. Without an extension runtime nothing is live.
func (c *Controller) ExtensionFrameDocument(pluginID, frameID string, generation uint64) ([]byte, error) {
	if h := c.extensionUIHub(); h != nil {
		return h.FrameDocument(pluginID, frameID, generation)
	}
	return nil, uihub.ErrFrameStale
}
