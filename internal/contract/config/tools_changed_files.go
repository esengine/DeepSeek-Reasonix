package config

// ChangedFilesProtected reports whether write_file refuses to overwrite a file
// that changed after the agent last read or wrote it. Unset means on: the
// check costs nothing until a file has actually changed underneath the agent.
func (t ToolsConfig) ChangedFilesProtected() bool {
	return t.ProtectChangedFiles == nil || *t.ProtectChangedFiles
}
