//go:build !windows

package main

// monitorScreenRects is only implemented on Windows, where the saved window
// origin is validated against the live monitor layout before restore. Other
// platforms keep the size-only heuristic; nil means "no monitor rects".
func monitorScreenRects() []screenRect {
	return nil
}
