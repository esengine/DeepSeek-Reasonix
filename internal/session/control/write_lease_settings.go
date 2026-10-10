package control

import "reasonix/internal/contract/config"

// WriteLeaseSetting is the write-lease mode as the user file holds it beside
// what this workspace will run with: a project file may set the same key, and a
// panel that showed only the user's answer would describe another run.
type WriteLeaseSetting struct {
	Mode      string `json:"mode"`
	Effective string `json:"effective"`
	Path      string `json:"path"`
}

// WriteLeaseSettings reads [agent] write_lease from the user file and from the
// merge in force for this workspace. Absent reads as "strict": every writer
// whose extent could overlap another's serializes, including the writers that
// declare no write_paths.
func (c *Controller) WriteLeaseSettings() WriteLeaseSetting {
	path := config.UserConfigPath()
	out := WriteLeaseSetting{
		Mode: config.LoadForEdit(path).Agent.WriteLeaseMode(),
		Path: path,
	}
	out.Effective = out.Mode
	if merged, err := config.LoadForRootReadOnly(c.WorkspaceRoot()); err == nil {
		out.Effective = merged.Agent.WriteLeaseMode()
	}
	return out
}

// SaveWriteLease persists the mode to the user file. The caller rebuilds: boot
// reads it while assembling the session runtime and the subagent scheduler, so a
// live runtime keeps the lease it was built with until it is replaced.
func (c *Controller) SaveWriteLease(mode string) error {
	unlock := config.LockUserConfigEdits()
	defer unlock()
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	cfg.Agent.WriteLease = config.NormalizeWriteLeaseMode(mode)
	return cfg.SaveTo(path)
}
