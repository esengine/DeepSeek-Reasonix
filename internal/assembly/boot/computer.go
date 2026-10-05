package boot

import (
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/browser"
	"reasonix/internal/platform/computer"
	"reasonix/internal/tools/builtin"
)

// computerHelper is the native helper of a host that can operate this
// machine's applications, or nil in every other host.
var computerHelper *computer.Helper

// SetComputerHelper gives every assembly in this process the computer-use
// tools, carried out by the helper executable at path. The host decides: the
// helper's permissions belong to the application that started it.
func SetComputerHelper(path string) {
	if path == "" {
		computerHelper = nil
		return
	}
	computerHelper = computer.NewHelper(path)
}

// bindMachineTools binds the tools that reach past the workspace into this
// machine: its browser, and its applications when the host has a helper. They
// replace the unbound built-ins, so they are bound after those are registered.
func bindMachineTools(reg *tool.Registry, cfg *config.Config, root string, reused *browser.Session) *browser.Session {
	if computerHelper != nil {
		for _, t := range builtin.ComputerTools(computer.NewSession(computerHelper)) {
			reg.Add(t)
		}
	}
	return bindBrowser(reg, cfg, root, reused)
}
