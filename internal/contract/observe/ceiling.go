package observe

import "reasonix/internal/contract/tool"

// Admits reports whether a tool may be held by a run under this posture: it
// says it is read-only and it declares a reach that only reads files or hands
// the turn back. The reach is checked first and alone decides for a tool that
// declares none, so a lazy tool's own methods are never consulted for it.
func Admits(t tool.Tool) bool {
	if t == nil {
		return false
	}
	switch tool.ReachOf(t) {
	case tool.ReachLocalRead, tool.ReachHostControl:
		return t.ReadOnly()
	default:
		return false
	}
}
