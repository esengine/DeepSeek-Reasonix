package main

// registerProjectRoot indexes workspaceRoot, realigns open tabs to its
// canonical spelling, and discovers existing sessions once per process.
func (a *App) registerProjectRoot(workspaceRoot string) {
	_ = addProject(workspaceRoot, "")
	a.syncTabWorkspaceRootSpellings()
	root := normalizeProjectRoot(workspaceRoot)
	if root == "" {
		return
	}
	registrationKey := projectRootKey(root)
	if _, loaded := a.catalogRegisteredProjectRoots.LoadOrStore(registrationKey, struct{}{}); loaded {
		return
	}
	if !a.requestSessionCatalogReconcile(desktopSessionDir(root)) {
		a.catalogRegisteredProjectRoots.Delete(registrationKey)
	}
}
