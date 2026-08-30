package main

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/remote/bootstrap"
)

// CheckRemotePlatform verifies the connected host runs a supported OS
// (Linux/macOS). The connect wizard calls it before directory browsing, so
// unsupported hosts fail early with one message.
func (a *App) CheckRemotePlatform(hostID string) error {
	rt, err := a.remoteRT()
	if err != nil {
		return err
	}
	return rt.CheckPlatform(a.bootContext(), hostID)
}

// CheckPlatform applies the same ParseUname gate EnsureServe uses immediately
// after connection instead of failing later during serve bootstrap.
func (m *desktopRemoteManager) CheckPlatform(ctx context.Context, hostID string) error {
	mh := m.managed(hostID)
	if mh == nil || mh.client == nil {
		return fmt.Errorf("host %q is not connected", hostID)
	}
	opCtx, cancel := managedOperationContext(ctx, mh)
	defer cancel()
	res, err := mh.client.Exec(opCtx, "uname -sm")
	if err != nil || strings.TrimSpace(string(res.Stdout)) == "" {
		return fmt.Errorf("remote host platform check failed: cannot detect OS (supported: Linux, macOS)")
	}
	if _, _, parseErr := bootstrap.ParseUname(string(res.Stdout)); parseErr != nil {
		return fmt.Errorf("remote host platform check failed: %w", parseErr)
	}
	return nil
}
