package boot

// ResolveWorkspaceRoot reports the project root Build resolves for the same
// value. A host needs the answer before the controller exists, to derive the
// workspace-scoped paths it passes back in as options.
func ResolveWorkspaceRoot(explicit string) string { return resolveWorkspaceRoot(explicit) }
