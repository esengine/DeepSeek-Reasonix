package control

import "reasonix/internal/contract/config"

// OpaqueWriterSerialization is the write-serialization switch as the user file
// holds it beside what this workspace will run with: a project file may set the
// same key, and an editor that showed only the user's answer would describe
// another run.
type OpaqueWriterSerialization struct {
	Enabled   bool   `json:"enabled"`
	Effective bool   `json:"effective"`
	Path      string `json:"path"`
}

// OpaqueWriterSerializationSettings reads [agent] serialize_opaque_writers from
// the user file and from the merge in force for this workspace. An absent key
// reads as enabled, which is the upstream behaviour: only writers that cannot
// declare write paths (bash, MCP) take the whole-workspace lock.
func (c *Controller) OpaqueWriterSerializationSettings() OpaqueWriterSerialization {
	path := config.UserConfigPath()
	out := OpaqueWriterSerialization{
		Enabled: config.LoadForEdit(path).Agent.SerializeOpaqueWriters,
		Path:    path,
	}
	out.Effective = out.Enabled
	if merged, err := config.LoadForRootReadOnly(c.WorkspaceRoot()); err == nil {
		out.Effective = merged.Agent.SerializeOpaqueWriters
	}
	return out
}

// SaveOpaqueWriterSerialization persists the switch to the user file. The caller
// rebuilds: boot reads it while assembling the session runtime and the subagent
// scheduler, so a live runtime keeps the lease it was built with until it is
// replaced.
func (c *Controller) SaveOpaqueWriterSerialization(enabled bool) error {
	unlock := config.LockUserConfigEdits()
	defer unlock()
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	cfg.Agent.SerializeOpaqueWriters = enabled
	return cfg.SaveTo(path)
}
