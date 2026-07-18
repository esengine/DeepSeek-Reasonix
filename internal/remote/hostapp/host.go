// Package hostapp is the production composition root for a Reasonix Remote
// Host process. It joins the durable catalog, the shared boot.Build runtime
// factory, the transport-neutral daemon, and a platform service endpoint
// without placing Unix or systemd policy inside Host core.
package hostapp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"

	"reasonix/internal/config"
	"reasonix/internal/nilutil"
	"reasonix/internal/remote/catalog"
	"reasonix/internal/remote/daemon"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/runtimefactory"
	"reasonix/internal/remote/service"
	"reasonix/internal/sandbox"
)

// Options keeps Host identity, persistence roots, and the boot builder
// injectable. Production callers should leave ControllerBuilder nil so every
// Session is constructed by the same boot.Build composition root as Local.
type Options struct {
	BuildID protocol.BuildID

	HostEpoch    protocol.HostEpoch
	NewHostEpoch func() (protocol.HostEpoch, error)
	HostInfo     *protocol.HostInfo
	Capabilities *protocol.Capabilities
	// CapabilityProvider is the authoritative Host configuration projection
	// used when Capabilities is not explicitly injected. It must not infer
	// availability from a non-nil Controller placeholder.
	CapabilityProvider func(context.Context) (protocol.Capabilities, error)

	CatalogOptions    catalog.Options
	ProfileResolver   catalog.ProfileResolver
	ControllerBuilder runtimefactory.Builder
	Stderr            io.Writer

	DaemonOptions daemon.Options
}

// App owns exactly one daemon Host epoch and all resources rooted beneath it.
// An attach transport never owns or closes this object.
type App struct {
	hostEpoch protocol.HostEpoch
	catalog   *catalog.Catalog
	server    *daemon.Server
	closeOnce sync.Once
}

func New(ctx context.Context, options Options) (*App, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := options.BuildID.Validate(); err != nil {
		return nil, fmt.Errorf("Remote Host Build ID: %w", err)
	}

	hostEpoch := options.HostEpoch
	if strings.TrimSpace(string(hostEpoch)) == "" {
		generator := options.NewHostEpoch
		if generator == nil {
			generator = newHostEpoch
		}
		var err error
		hostEpoch, err = generator()
		if err != nil {
			return nil, fmt.Errorf("generate Remote Host epoch: %w", err)
		}
		if strings.TrimSpace(string(hostEpoch)) == "" {
			return nil, errors.New("generated Remote Host epoch is empty")
		}
	}

	resolver := options.ProfileResolver
	if nilutil.IsNil(resolver) {
		resolver = options.CatalogOptions.ProfileResolver
	}
	if nilutil.IsNil(resolver) {
		return nil, errors.New("Remote Host profile resolver is required")
	}
	catalogOptions := options.CatalogOptions
	catalogOptions.ProfileResolver = resolver
	catalogValue, err := catalog.New(hostEpoch, catalogOptions)
	if err != nil {
		return nil, fmt.Errorf("open Remote catalog: %w", err)
	}

	factory, err := runtimefactory.New(runtimefactory.Options{
		Resolver: catalogValue,
		Builder:  options.ControllerBuilder,
		Stderr:   options.Stderr,
	})
	if err != nil {
		return nil, fmt.Errorf("assemble Remote runtime factory: %w", err)
	}

	hostInfo := defaultHostInfo(options.Stderr)
	if options.HostInfo != nil {
		hostInfo = *options.HostInfo
	}
	capabilities := protocol.Capabilities{}
	if options.Capabilities != nil {
		capabilities = *options.Capabilities
	} else {
		provider := options.CapabilityProvider
		if provider == nil {
			provider = configuredCapabilities
		}
		capabilities, err = provider(ctx)
		if err != nil {
			return nil, fmt.Errorf("resolve Remote Host capabilities: %w", err)
		}
	}

	daemonOptions := options.DaemonOptions
	// Identity and ownership dependencies are fixed by this composition root;
	// callers may inject only daemon clocks/ID generators and diagnostics.
	daemonOptions.BuildID = options.BuildID
	daemonOptions.HostEpoch = hostEpoch
	daemonOptions.HostInfo = hostInfo
	daemonOptions.Capabilities = capabilities
	daemonOptions.Catalog = catalogValue
	daemonOptions.ControllerFactory = factory
	daemonOptions.Metadata = func(ctx context.Context, target protocol.RuntimeTarget) (protocol.SessionMetaSnapshot, error) {
		metadata, err := catalogValue.Metadata(ctx, target)
		if err != nil {
			return protocol.SessionMetaSnapshot{}, err
		}
		return protocol.SessionMetaSnapshot{
			TopicID: metadata.TopicID, Title: metadata.Title, ResolvedProfile: metadata.ResolvedProfile,
		}, nil
	}
	server, err := daemon.New(ctx, daemonOptions)
	if err != nil {
		return nil, fmt.Errorf("assemble Remote daemon: %w", err)
	}
	return &App{hostEpoch: hostEpoch, catalog: catalogValue, server: server}, nil
}

func configuredCapabilities(context.Context) (protocol.Capabilities, error) {
	// Safe Mode intentionally omits Memory discovery and writes even though
	// boot.Build still installs a non-nil empty Set as a recovery placeholder.
	// AutoResearch remains a built-in Controller capability and has no Host
	// configuration disable switch in V1.
	return protocol.FrozenCapabilities(!config.SafeModeRequested(), true), nil
}

func newHostEpoch() (protocol.HostEpoch, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return protocol.HostEpoch("host_" + hex.EncodeToString(value[:])), nil
}

func defaultHostInfo(warn io.Writer) protocol.HostInfo {
	backend := "none"
	if sandbox.Available() {
		switch runtime.GOOS {
		case "linux":
			backend = "bubblewrap"
		case "darwin":
			backend = "seatbelt"
		case "windows":
			backend = "appcontainer"
		default:
			backend = "platform"
		}
	}
	return protocol.HostInfo{
		OS: runtime.GOOS, Arch: runtime.GOARCH,
		ShellKind: sandbox.ResolveShell("", "", warn).Kind.String(), SandboxBackend: backend,
	}
}

func (a *App) HostEpoch() protocol.HostEpoch {
	if a == nil {
		return ""
	}
	return a.hostEpoch
}

func (a *App) Catalog() *catalog.Catalog {
	if a == nil {
		return nil
	}
	return a.catalog
}

func (a *App) Server() *daemon.Server {
	if a == nil {
		return nil
	}
	return a.server
}

// Serve binds the selected local endpoint and blocks until the daemon exits or
// ctx is cancelled. It never installs, starts, upgrades, or repairs a service.
func (a *App) Serve(ctx context.Context, endpoint *service.Endpoint) error {
	if a == nil || a.server == nil {
		return errors.New("Remote Host app is not initialized")
	}
	return service.RunServer(ctx, endpoint, a.server)
}

func (a *App) Close() {
	if a == nil {
		return
	}
	a.closeOnce.Do(func() {
		if a.server != nil {
			a.server.Close()
		}
	})
}
