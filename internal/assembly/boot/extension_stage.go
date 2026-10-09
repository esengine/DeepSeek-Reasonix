package boot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"

	"reasonix/internal/base/netclient"
	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/extension/dispatch"
	"reasonix/internal/ext/extension/protocol"
	"reasonix/internal/ext/extension/sidecar"
	"reasonix/internal/ext/extension/uihub"
	"reasonix/internal/session/control"
)

// extensionStage is one build's extension generation: the sidecars started
// before model resolution, and the UI hub they call through. The controller
// does not exist yet, so the hub reaches it through ctrl once published.
type extensionStage struct {
	generation  uint64
	sessionID   string
	root        string
	warn        func(string)
	sidecarDown func(string, extension.InterceptorPoint)
	hub         *uihub.Hub
	mgr         *sidecar.Manager
	ctrl        atomic.Pointer[control.Controller]
	ready       chan struct{} // closed when ctrl is published
	failed      chan struct{} // closed when the build fails before assembly owns mgr
}

// startExtensions starts the installed v2 runtime packages once, before model
// resolution, so a plugin-namespaced model ref resolves on the first boot.
// sink must already be final: the hub's emit path captures it.
func startExtensions(ctx context.Context, opts Options, roots config.Roots, root string, owner *extension.RuntimeOwner, sink event.Sink) (*extensionStage, error) {
	ext := &extensionStage{
		generation: nextRuntimeGeneration(),
		root:       root,
		ready:      make(chan struct{}),
		failed:     make(chan struct{}),
	}
	// A fresh controller has no session path; the handshake only needs a stable identity.
	ext.sessionID = fmt.Sprintf("boot-%d", ext.generation)
	ext.warn = func(msg string) {
		redacted := secrets.RedactCredentials(msg)
		slog.Warn("boot: extension runtime: "+redacted, "root", root)
		report(sink, event.Event{Level: event.LevelWarn, Text: redacted})
	}
	ext.sidecarDown = func(pluginID string, point extension.InterceptorPoint) {
		slog.Warn("boot: extension skipped: no live sidecar", "extension", pluginID, "point", point, "root", root)
		report(sink, extensionSkippedEvent(pluginID, string(point)))
	}
	ext.hub = uihub.New(uihub.Options{
		SessionID:  ext.sessionID,
		Generation: ext.generation,
		Owner:      owner,
		Emit: func(ev event.Event) {
			if c := ext.ctrl.Load(); c != nil {
				c.EmitExtensionEvent(ev)
				return
			}
			sink.Emit(ev)
		},
		Request: func(reqCtx context.Context, req uihub.HubRequest) (map[string]any, bool, error) {
			return gateExtensionUIRequest(reqCtx, ext.ctrl.Load, ext.ready, ext.failed,
				func(c *control.Controller) (map[string]any, bool, error) {
					return uihub.AskRequestFunc(c.Ask)(reqCtx, req)
				})
		},
		Warn: func(msg string) {
			slog.Warn("boot: extension UI hub: "+msg, "root", root)
		},
	})
	home := roots.Home()
	if opts.Observe != nil {
		// Extension sidecars are programs the run would start; the posture holds none.
		home = ""
	}
	mgr, err := preflightExtensionRuntimes(ctx, home, ext.boot(), opts.Extensions, planForPreflight(opts, ext.generation))
	if err != nil {
		return nil, fmt.Errorf("boot: %w", err)
	}
	ext.mgr = mgr
	return ext, nil
}

func (ext *extensionStage) session() protocol.SessionContext {
	return protocol.SessionContext{SessionID: ext.sessionID, WorkspaceRoot: ext.root, Generation: ext.generation}
}

func (ext *extensionStage) boot() extensionBoot {
	return extensionBoot{session: ext.session(), ui: ext.hub, onWarning: ext.warn, onSidecarDown: ext.sidecarDown}
}

// publish hands the hub the controller; from here on host/ui/* traffic rides it.
func (ext *extensionStage) publish(ctrl *control.Controller) {
	ext.ctrl.Store(ctrl)
	close(ext.ready)
}

// providerStage is the build's provider resolution: base is config-backed or
// caller-owned, effective adds sidecar providers, and extension is non-nil
// only when a sidecar declared one.
type providerStage struct {
	base, effective, extension provider.Resolver
}

// resolveProviders folds sidecar-declared providers into the base catalog.
// A conflict the plugin has no claim for is fatal: booting without a
// declared provider would silently change what the session is.
func resolveProviders(opts Options, cfg *config.Config, proxySpec netclient.ProxySpec, mgr *sidecar.Manager, owner *extension.RuntimeOwner) (providerStage, error) {
	ps := providerStage{base: opts.ProviderResolver, effective: opts.ProviderResolver}
	if ps.base == nil {
		ps.base = NewLocalProviderResolver(cfg, proxySpec)
	}
	if mgr == nil || !declaresProviders(mgr) {
		return ps, nil
	}
	claims, err := resolveReplacementClaims(mgr.Contributions())
	if err != nil {
		return ps, fmt.Errorf("boot: %w", err)
	}
	merged, err := mergeSidecarProviders(ps.base, mgr, claims, owner)
	if err != nil {
		return ps, fmt.Errorf("boot: %w", err)
	}
	ps.effective, ps.extension = merged, merged
	return ps, nil
}

func declaresProviders(mgr *sidecar.Manager) bool {
	for _, client := range mgr.Clients() {
		if len(client.Handshake().Providers) > 0 {
			return true
		}
	}
	return false
}

// extensionContractBroken reports an assembly failure the build may not
// degrade past: a slot two runtimes claim, or a required strategy that failed.
// Booting without the installed contract would silently change the session.
func extensionContractBroken(err error) bool {
	var requiredErr *sidecar.RequiredStartError
	var slotErr *extension.SlotConflictError
	var blockErr *dispatch.BlockError
	var failureErr *dispatch.FailureError
	var violationErr *dispatch.ViolationError
	return errors.As(err, &requiredErr) || errors.As(err, &slotErr) ||
		errors.As(err, &blockErr) || errors.As(err, &failureErr) || errors.As(err, &violationErr)
}
