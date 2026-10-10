package usecap

import "reasonix/internal/contract/tool"

// routableRegistryTool is the registry hit a capability call may bind to. A
// withdrawn entry is a slot held open for a tool the server dropped, not a
// target: the live catalog decides, so a tool that comes back is callable.
func routableRegistryTool(reg *tool.Registry, name string) (tool.Tool, bool) {
	tl, ok := reg.Get(name)
	return tl, ok && !tool.IsWithdrawn(tl)
}
