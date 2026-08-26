package main

func (a *App) StopRemoteServer(hostID, workspace string) error {
	op := a.beginRemoteWindowHostOperation(hostID)
	return op.run(func(func() bool) error {
		rt, err := a.remoteRT()
		if err != nil {
			return err
		}
		parked := a.parkRemoteTabsForServer(hostID, workspace, "serve_down", "Remote server stopped.")
		if err := rt.StopServer(hostID, workspace); err != nil {
			for _, tabID := range parked {
				a.emitRemoteTabState(tabID, "connecting", "")
				a.goSafe("remoteTabServe", func() { a.bootstrapRemoteTab(tabID, hostID, workspace) })
			}
			return err
		}
		// Stopping the service also tears down that workspace's loopback
		// tunnel, so close the host's web window only when it is showing this
		// workspace.
		if a.remoteWindowWorkspace(hostID) == workspace {
			a.closeRemoteWindowForHost(hostID)
		}
		return nil
	})
}
