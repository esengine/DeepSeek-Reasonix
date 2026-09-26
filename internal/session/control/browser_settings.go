package control

import "reasonix/internal/contract/config"

// BrowserToolsSettings is the built-in browser switch as the user file holds it
// beside what this workspace will run with: a project file may set the same key,
// and an editor that showed only the user's answer would describe another run.
type BrowserToolsSettings struct {
	Enabled   bool   `json:"enabled"`
	Effective bool   `json:"effective"`
	Path      string `json:"path"`
}

// BrowserToolsSettings reads [tools] browser_tools from the user file and from
// the merge in force for this workspace.
func (c *Controller) BrowserToolsSettings() BrowserToolsSettings {
	path := config.UserConfigPath()
	out := BrowserToolsSettings{
		Enabled: config.LoadForEdit(path).Tools.BrowserToolsEnabled(),
		Path:    path,
	}
	out.Effective = out.Enabled
	if merged, err := config.LoadForRootReadOnly(c.WorkspaceRoot()); err == nil {
		out.Effective = merged.Tools.BrowserToolsEnabled()
	}
	return out
}

// SaveBrowserToolsSettings persists the switch to the user file. The caller
// rebuilds: boot binds the browser tools while assembling, so a live runtime
// keeps the tool list it was built with until it is replaced.
func (c *Controller) SaveBrowserToolsSettings(enabled bool) error {
	unlock := config.LockUserConfigEdits()
	defer unlock()
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	cfg.Tools.BrowserTools = &enabled
	return cfg.SaveTo(path)
}
