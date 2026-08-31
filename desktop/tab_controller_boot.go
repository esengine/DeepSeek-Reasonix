package main

import (
	"context"
	"errors"

	"reasonix/internal/boot"
	"reasonix/internal/control"
)

var errTabControllerExtensionsChanged = errors.New("desktop: controller extensions changed during build")

// buildTabControllerBoot is a thin wrapper around boot.Build so the large
// controller assembly path can stay under function-size / complexity budgets.
func (a *App) buildTabControllerBoot(ctx context.Context, opts boot.Options) (control.SessionAPI, error) {
	return boot.Build(ctx, opts)
}

// buildTabControllerBootFenced keeps optimistic builds concurrent with each
// other but excludes live MCP mutation. The generation check happens after the
// gate so a build that loaded stale configuration never launches extensions.
func (a *App) buildTabControllerBootFenced(ctx context.Context, generation uint64, opts boot.Options) (control.SessionAPI, error) {
	a.extensionBuildMu.RLock()
	defer a.extensionBuildMu.RUnlock()
	if a.currentExtensionGeneration() != generation {
		return nil, errTabControllerExtensionsChanged
	}
	return a.buildTabControllerBoot(ctx, opts)
}

// lockTabControllerPublication makes the generation check part of admission.
// An MCP writer bumps the generation before releasing runtimeAdmissionMu, so a
// build cannot pass an early check and publish stale registries afterward.
func (a *App) lockTabControllerPublication(generation uint64) (func(), bool) {
	a.runtimeAdmissionMu.RLock()
	if a.currentExtensionGeneration() != generation {
		a.runtimeAdmissionMu.RUnlock()
		return nil, false
	}
	return a.runtimeAdmissionMu.RUnlock, true
}

func (a *App) handleTabControllerBootError(
	tab *WorkspaceTab,
	registration *sharedHostMCPRegistration,
	rootKey string,
	buildGeneration uint64,
	wailsCtx context.Context,
	err error,
) bool {
	if err == nil {
		return false
	}
	registration.rollback()
	if errors.Is(err, errTabControllerExtensionsChanged) {
		a.abandonSupersededBuild(tab, nil, rootKey, "")
		a.scheduleDeferredStartupBuild(tab.ID)
		return true
	}
	a.mu.Lock()
	if a.tabBuildSupersededLocked(tab, buildGeneration) {
		a.mu.Unlock()
		a.abandonSupersededBuild(tab, nil, rootKey, "")
		return true
	}
	leaseHeld, save := a.markTabStartupFailureLocked(tab, err, keepStartupRestore)
	hostKey := takeTabSharedHostKey(tab)
	tab.releaseSessionLease()
	a.mu.Unlock()
	a.writeTabsSaveRequest(save)
	if hostKey != "" {
		a.releaseSharedHost(hostKey)
	}
	if leaseHeld {
		a.scheduleDeferredStartupBuild(tab.ID)
	}
	a.emitReady(wailsCtx, tab.ID)
	return true
}
