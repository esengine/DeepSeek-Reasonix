package sidecar

import "reasonix/internal/ext/pluginpkg"

// FrameBinder is a UI binder that serves the frame documents a plugin's
// package ships; it is told which package each plugin runs from.
type FrameBinder interface {
	DeclareFrames(pluginID string, pkg pluginpkg.Package)
}

// frameDrawer is a per-plugin UI handler whose host accepts frame surfaces.
type frameDrawer interface{ DrawsFrames() bool }

func bindFrames(binder UIBinder, pluginID string, pkg pluginpkg.Package) {
	if fb, ok := binder.(FrameBinder); ok {
		fb.DeclareFrames(pluginID, pkg)
	}
}

func frameIDs(frames []pluginpkg.FrameRef) []string {
	if len(frames) == 0 {
		return nil
	}
	out := make([]string, len(frames))
	for i, f := range frames {
		out[i] = f.ID
	}
	return out
}
