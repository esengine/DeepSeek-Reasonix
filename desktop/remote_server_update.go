package main

// UpdateRemoteServer stops the workspace's remote Serve and reinstalls it
// at this desktop's exact release, then re-attaches the parked tabs. The
// frontend confirms first: in-flight turns are interrupted by the stop.
func (a *App) UpdateRemoteServer(hostID, workspace string) error {
	op := a.beginRemoteWindowHostOperation(hostID)
	return op.run(func(current func() bool) error {
		rt, err := a.remoteRT()
		if err != nil {
			return err
		}
		parked := a.parkRemoteTabsForServer(hostID, workspace, "serve_down", "Remote server updating.")
		reattach := func() {
			if !rt.HostConnected(hostID) {
				// A superseding disconnect or removal owns the final state;
				// reattaching would reconnect a host the user closed.
				return
			}
			for _, tabID := range parked {
				a.emitRemoteTabState(tabID, "connecting", "")
				a.goRemoteTabSafe("remoteTabServe", func() { a.bootstrapRemoteTab(tabID, hostID, workspace) })
			}
		}
		if _, _, err := rt.UpdateServer(a.bootContext(), hostID, workspace); err != nil {
			reattach()
			return err
		}
		if !current() {
			// A window open or disconnect superseded this update and owns the
			// final state, but the parked tabs still belong to this call.
			reattach()
			return nil
		}
		// The replacement serve binds a fresh loopback port; a web window
		// still showing this workspace would point at the dead old tunnel.
		if a.remoteWindowWorkspace(hostID) == workspace {
			a.closeRemoteWindowForHost(hostID)
		}
		reattach()
		return nil
	})
}
