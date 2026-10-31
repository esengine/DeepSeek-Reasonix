package boot

import (
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/browser"
	"reasonix/internal/platform/computer"
	"reasonix/internal/tools/builtin"
)

// bindMachineTools binds the tools that reach past the workspace into this
// machine: its browser, and its applications when the host provides a driver. They
// replace the unbound built-ins, so they are bound after those are registered.
func bindMachineTools(reg *tool.Registry, cfg *config.Config, root, profile string, reused *browser.Session, driver *computer.Driver) *browser.Session {
	if driver != nil {
		for _, t := range builtin.ComputerTools(computer.NewSession(driver)) {
			reg.Add(t)
		}
	} else {
		// No helper means the two tools could only answer `computer.unavailable`,
		// refused the way `browserWithoutEngine` refuses below. A tool the model
		// can see and cannot use is a turn spent discovering that.
		suspendComputerTools(reg)
	}
	return bindBrowser(reg, cfg, root, profile, reused)
}

// suspendComputerTools takes the computer-use tools out of the registry and
// keeps them out, for the reason `suspendBrowserTools` does: `addBuiltins` has
// already seeded the unbound copies, and a later `Add` — an MCP handshake, a
// preset that binds its own set — would put them back.
func suspendComputerTools(reg *tool.Registry) {
	for _, t := range builtin.ComputerTools(nil) {
		reg.SuspendPrefix(t.Name())
	}
}
