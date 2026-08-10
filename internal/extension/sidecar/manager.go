package sidecar

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/pluginpkg"
	"reasonix/internal/secrets"
)

const (
	// maxConcurrentPackageStarts bounds process creation while still preventing
	// one slow optional runtime from serially delaying every package after it.
	maxConcurrentPackageStarts = 4
	// packageStartupBudget is shared by every runtime in one generation. Without
	// a generation-level budget, N stalled optional runtimes could delay boot by
	// N times the per-client handshake timeout.
	packageStartupBudget = defaultHandshakeTimeout
)

type clientStarter func(context.Context, ClientOptions) (*Client, error)

type packageStartJob struct {
	item pluginpkg.InstalledPackage
	opts ClientOptions
}

type packageStartResult struct {
	client *Client
	err    error
}

// RequiredStartError reports a required runtime package that failed to start
// or hand shake. It fails the whole build; an optional package's failure is a
// warning instead. errors.As distinguishes the two at the boot call site.
type RequiredStartError struct {
	Plugin string
	Err    error
}

func (e *RequiredStartError) Error() string {
	return fmt.Sprintf("required extension runtime %q failed to start: %v", e.Plugin, e.Err)
}

func (e *RequiredStartError) Unwrap() error { return e.Err }

// LoadRuntimePackages returns the installed, ENABLED packages that declare a
// native runtime. This is the only enumeration the Manager ever launches from —
// see the package doc for the authorization invariant.
func LoadRuntimePackages(home string) ([]pluginpkg.InstalledPackage, []string) {
	installed, warnings := pluginpkg.LoadInstalled(home)
	var out []pluginpkg.InstalledPackage
	for _, item := range installed {
		if item.Package.Manifest.Runtime != nil {
			out = append(out, item)
		}
	}
	return out, warnings
}

// Manager owns every sidecar started for one runtime generation. It is the
// ONLY sidecar launch path, and its inputs are the pluginpkg installed state
// alone. It implements io.Closer so the kernel's RuntimeSet can retire it
// with its controller generation.
type Manager struct {
	mu      sync.Mutex
	clients map[string]*Client
	closed  bool
	// planAdopted records clients moved from a previous manager during
	// StartPackagesWithPlan (Unchanged). RollbackPlanStart reattaches only
	// these; newly started Added/Reloaded clients are closed by m.Close().
	planAdopted map[string]*Client
}

// StartPackages starts every installed runtime package (cold start). Prefer
// StartPackagesWithPlan when a RuntimePlan can adopt unchanged packages.
func StartPackages(ctx context.Context, home string, sessionCtx protocol.SessionContext, ui UIHandler) (*Manager, []string, error) {
	return StartPackagesWithPlan(ctx, home, sessionCtx, ui, nil, nil)
}

// startLoadedPackages starts a previously discovered, deterministically ordered
// package set. Handler binding stays serial; process startup and handshakes use
// a bounded worker pool and the caller's shared generation context. Results are
// consumed in package order so warnings and required-failure selection do not
// depend on goroutine completion order.
func startLoadedPackages(ctx context.Context, packages []pluginpkg.InstalledPackage, sessionCtx protocol.SessionContext, ui UIHandler, start clientStarter) (*Manager, []string, error) {
	m := &Manager{clients: make(map[string]*Client)}
	if len(packages) == 0 {
		return m, nil, nil
	}
	var binder UIBinder
	if b, ok := ui.(UIBinder); ok {
		binder = b
	}
	jobs := make([]packageStartJob, len(packages))
	for i, item := range packages {
		pluginID := item.Installed.Name
		clientUI := ui
		if binder != nil {
			clientUI = binder.HandlerFor(pluginID)
		}
		jobs[i] = packageStartJob{item: item, opts: ClientOptions{
			Package:   item.Package,
			Installed: item.Installed,
			Session:   sessionCtx,
			UI:        clientUI,
			OnCrash: func(err error) {
				slog.Warn("extension sidecar crashed", "plugin", pluginID, "err", secrets.RedactError(err))
				if binder != nil {
					binder.ClientCrashed(pluginID)
				}
			},
		}}
	}

	results := make([]packageStartResult, len(jobs))
	indices := make(chan int, len(jobs))
	for i := range jobs {
		indices <- i
	}
	close(indices)
	workers := min(maxConcurrentPackageStarts, len(jobs))
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for i := range indices {
				if err := ctx.Err(); err != nil {
					results[i].err = fmt.Errorf("extension generation startup stopped before launch: %w", err)
					continue
				}
				results[i].client, results[i].err = start(ctx, jobs[i].opts)
			}
		}()
	}
	wg.Wait()

	var warnings []string
	var requiredErr *RequiredStartError
	for i, result := range results {
		item := jobs[i].item
		pluginID := item.Installed.Name
		if result.err != nil {
			if item.Package.Manifest.Runtime.Required {
				if requiredErr == nil {
					requiredErr = &RequiredStartError{Plugin: pluginID, Err: result.err}
				}
			} else {
				warnings = append(warnings, fmt.Sprintf("%s: optional extension runtime failed to start: %v", pluginID, result.err))
			}
			continue
		}
		m.clients[pluginID] = result.client
	}
	if requiredErr != nil {
		_ = m.Close()
		return nil, warnings, requiredErr
	}
	return m, warnings, nil
}

// Client returns the client for one plugin ID, or nil.
func (m *Manager) Client(pluginID string) *Client {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.clients[pluginID]
}

// Clients returns every live client ordered by plugin ID.
func (m *Manager) Clients() []*Client {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Client, 0, len(m.clients))
	for _, client := range m.clients {
		out = append(out, client)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].pluginID < out[j].pluginID })
	return out
}

// Close shuts every sidecar down in parallel; each client's own budgets
// bound the total. It is idempotent.
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	clients := make([]*Client, 0, len(m.clients))
	for _, client := range m.clients {
		clients = append(clients, client)
	}
	m.clients = nil
	m.planAdopted = nil
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Add(1)
		go func(c *Client) {
			defer wg.Done()
			_ = c.Close()
		}(client)
	}
	wg.Wait()
	return nil
}

// Declaration-level kernel contribution payloads. They describe what a
// started sidecar declared; dispatch wiring arrives with stages 6-8.

// InterceptorDecl is the KindInterceptor payload for one manifest-declared
// interceptor point.
type InterceptorDecl struct {
	PluginID string
	Point    string
	Priority int
}

// StrategyDecl is the KindStrategy payload claiming one replacement slot. It
// implements the kernel's SlotClaimer so two runtimes claiming the same slot
// fail the build through ReplaceClaims.
type StrategyDecl struct {
	PluginID string
	Slots    []extension.Slot
}

// ReplacementSlots implements extension.SlotClaimer.
func (d StrategyDecl) ReplacementSlots() []extension.Slot {
	return append([]extension.Slot(nil), d.Slots...)
}

// ProviderDecl is the KindProvider payload for one handshake-declared
// extension-hosted provider.
type ProviderDecl struct {
	PluginID   string
	Descriptor protocol.ProviderDescriptor
}

// UIActionDecl is the KindUIAction payload for one handshake-declared action.
type UIActionDecl struct {
	PluginID string
	Decl     protocol.UIActionDecl
}

// Contributions renders every started client's declarations as kernel
// contributions: interceptor stubs per manifest intercept, strategy claims
// per manifest replaces, and provider / UI-action declarations from the
// validated handshake. Kernel-invalid IDs (whitespace, non-ref provider IDs)
// are skipped with a debug log rather than failing the whole snapshot.
func (m *Manager) Contributions() []extension.Contribution {
	var out []extension.Contribution
	for _, client := range m.Clients() {
		rt := client.rt
		source := extension.ContributionSource{
			Scope:    extension.ScopePlugin,
			PluginID: client.pluginID,
			Version:  client.version,
			Origin:   "extension-runtime",
		}
		for _, point := range rt.Intercepts {
			if !kernelID(point) {
				slog.Debug("sidecar: skipping interceptor point outside the kernel ID contract", "plugin", client.pluginID, "point", point)
				continue
			}
			out = append(out, extension.Contribution{
				Kind:     extension.KindInterceptor,
				ID:       point,
				Source:   source,
				Priority: rt.Priority,
				Payload:  InterceptorDecl{PluginID: client.pluginID, Point: point, Priority: rt.Priority},
			})
		}
		for _, slot := range rt.Replaces {
			if !kernelID(slot) {
				slog.Debug("sidecar: skipping replacement slot outside the kernel ID contract", "plugin", client.pluginID, "slot", slot)
				continue
			}
			out = append(out, extension.Contribution{
				Kind:     extension.KindStrategy,
				ID:       slot,
				Source:   source,
				Priority: rt.Priority,
				Payload:  StrategyDecl{PluginID: client.pluginID, Slots: []extension.Slot{extension.Slot(slot)}},
			})
		}
		result := client.Handshake()
		prefix := "plugin/" + client.pluginID + "/"
		for _, desc := range result.Providers {
			ref := strings.TrimPrefix(desc.Ref, prefix)
			if !extension.IsProviderRef(ref) {
				slog.Debug("sidecar: skipping provider ref outside the kernel ID contract", "plugin", client.pluginID, "ref", desc.Ref)
				continue
			}
			out = append(out, extension.Contribution{
				Kind:    extension.KindProvider,
				ID:      ref,
				Source:  source,
				Payload: ProviderDecl{PluginID: client.pluginID, Descriptor: desc},
			})
		}
		for _, decl := range result.UIActions {
			if !kernelID(decl.ActionID) {
				slog.Debug("sidecar: skipping UI action outside the kernel ID contract", "plugin", client.pluginID, "action", decl.ActionID)
				continue
			}
			out = append(out, extension.Contribution{
				Kind:    extension.KindUIAction,
				ID:      decl.ActionID,
				Source:  source,
				Payload: UIActionDecl{PluginID: client.pluginID, Decl: decl},
			})
		}
	}
	return out
}

// kernelID mirrors the kernel's generic ID hygiene (non-empty, no
// whitespace); boot's legacy assembly uses the same rule.
func kernelID(id string) bool {
	id = strings.TrimSpace(id)
	return id != "" && !strings.ContainsAny(id, " \t\n")
}
