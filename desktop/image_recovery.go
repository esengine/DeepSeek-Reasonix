package main

import (
	"errors"

	"reasonix/internal/control"
	"reasonix/internal/provider"
)

func (a *App) ResolveImageRecovery(id string, selected []provider.ImageIdentity) error {
	return a.ResolveImageRecoveryForTab("", id, selected)
}

func (a *App) ResolveImageRecoveryForTab(tabID, id string, selected []provider.ImageIdentity) error {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return errors.New("tab is not ready")
	}
	recovery, ok := ctrl.(control.ImageRecoveryControl)
	if !ok {
		return control.ErrImageRecoveryUnavailable
	}
	return recovery.ResolveImageRecovery(a.bootContext(), id, selected)
}
