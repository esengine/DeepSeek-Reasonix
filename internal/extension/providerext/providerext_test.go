package providerext

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/provider"
)

// fakeClient implements ProviderClient with programmable behavior; streams are
// driven by the test calling the Resolver's router methods directly.
type fakeClient struct {
	pluginID     string
	crashed      atomic.Bool
	disconnected chan struct{}
	handshake    protocol.InitializeResult

	mu         sync.Mutex
	catalog    []protocol.ProviderDescriptor
	catalogErr error
	catalogFn  func(context.Context) ([]protocol.ProviderDescriptor, error)
	fetches    int
	openErr    error
	accept     bool
	opened     []protocol.StreamOpenParams
	cancels    []string
	cancelWake chan struct{}
}

func newFakeClient(pluginID string, providers ...protocol.ProviderDescriptor) *fakeClient {
	return &fakeClient{
		pluginID:     pluginID,
		disconnected: make(chan struct{}),
		handshake:    protocol.InitializeResult{Providers: providers},
		accept:       true,
		cancelWake:   make(chan struct{}, 16),
	}
}

func (f *fakeClient) PluginID() string                     { return f.pluginID }
func (f *fakeClient) Crashed() bool                        { return f.crashed.Load() }
func (f *fakeClient) Disconnected() <-chan struct{}        { return f.disconnected }
func (f *fakeClient) Handshake() protocol.InitializeResult { return f.handshake }

// kill simulates a mid-stream sidecar crash: the connection drops without any
// further stream notifications.
func (f *fakeClient) kill() {
	f.crashed.Store(true)
	close(f.disconnected)
}

func (f *fakeClient) ProviderCatalog(ctx context.Context) ([]protocol.ProviderDescriptor, error) {
	f.mu.Lock()
	f.fetches++
	fn := f.catalogFn
	catalog := append([]protocol.ProviderDescriptor(nil), f.catalog...)
	err := f.catalogErr
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return catalog, err
}

func (f *fakeClient) fetchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fetches
}

func (f *fakeClient) ProviderStreamOpen(_ context.Context, params protocol.StreamOpenParams) (protocol.StreamOpenResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = append(f.opened, params)
	if f.openErr != nil {
		return protocol.StreamOpenResult{}, f.openErr
	}
	return protocol.StreamOpenResult{Accepted: f.accept}, nil
}

func (f *fakeClient) ProviderStreamCancel(streamID string) {
	f.mu.Lock()
	f.cancels = append(f.cancels, streamID)
	f.mu.Unlock()
	f.cancelWake <- struct{}{}
}

func (f *fakeClient) openedParams(t *testing.T) protocol.StreamOpenParams {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.opened) != 1 {
		t.Fatalf("stream opens = %d, want 1", len(f.opened))
	}
	return f.opened[0]
}

func (f *fakeClient) waitCancel(t *testing.T, streamID string) {
	t.Helper()
	deadline := time.Now().Add(testBudget)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		if slices.Contains(f.cancels, streamID) {
			f.mu.Unlock()
			return
		}
		f.mu.Unlock()
		select {
		case <-f.cancelWake:
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatalf("stream cancel for %q never arrived", streamID)
}

// testBudget bounds every wait in these tests; the gap-timer test needs just
// over a second, so this stays comfortably above it.
const testBudget = 5 * time.Second

func testResolver(t *testing.T, base provider.Resolver, claims map[extension.Slot]extension.ContributionSource, clients ...ProviderClient) *Resolver {
	t.Helper()
	r, err := New(base, func() []ProviderClient { return clients }, claims)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func baseCatalog() *provider.StaticResolver {
	return &provider.StaticResolver{
		Descriptors: []provider.Descriptor{{Ref: "deepseek/deepseek-chat", DisplayName: "deepseek", Model: "deepseek-chat"}},
		Providers:   map[string]provider.Provider{"deepseek/deepseek-chat": staticProvider("deepseek")},
	}
}

type staticProvider string

func (s staticProvider) Name() string { return string(s) }
func (s staticProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("static provider does not stream")
}

func demoDescriptor() protocol.ProviderDescriptor {
	return protocol.ProviderDescriptor{
		Ref: "plugin/demo/fake/x", DisplayName: "Fake Demo", Model: "x",
		ContextWindow: 64_000, Tools: true, Reasoning: true,
	}
}

func TestCatalogMergesBaseAndSidecar(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	fc.catalog = []protocol.ProviderDescriptor{demoDescriptor()}
	r := testResolver(t, baseCatalog(), nil, fc)

	catalog := r.Catalog()
	if len(catalog) != 2 {
		t.Fatalf("catalog = %v, want base + sidecar entries", catalog)
	}
	if catalog[0].Ref != "deepseek/deepseek-chat" || catalog[1].Ref != "plugin/demo/fake/x" {
		t.Fatalf("catalog refs = %q, %q", catalog[0].Ref, catalog[1].Ref)
	}
	if catalog[1].DisplayName != "Fake Demo" || catalog[1].ContextWindow != 64_000 || !catalog[1].Tools || !catalog[1].Reasoning {
		t.Fatalf("sidecar descriptor did not convert: %+v", catalog[1])
	}
}

func TestCatalogSkipsEntriesOutsideNamespace(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	fc.catalog = []protocol.ProviderDescriptor{
		demoDescriptor(),
		{Ref: "plugin/other/fake/x", Model: "x"},
		{Ref: "plain/ref", Model: "ref"},
	}
	r := testResolver(t, baseCatalog(), nil, fc)

	catalog := r.Catalog()
	if len(catalog) != 2 || catalog[1].Ref != "plugin/demo/fake/x" {
		t.Fatalf("catalog = %v, want only the namespaced sidecar entry", catalog)
	}
}

func TestCatalogCachesPerClientAndDropsCrashed(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	fc.catalog = []protocol.ProviderDescriptor{demoDescriptor()}
	r := testResolver(t, baseCatalog(), nil, fc)

	if got := len(r.Catalog()); got != 2 {
		t.Fatalf("first catalog size = %d", got)
	}
	if got := len(r.Catalog()); got != 2 {
		t.Fatalf("second catalog size = %d", got)
	}
	if fetches := fc.fetchCount(); fetches != 1 {
		t.Fatalf("catalog fetches = %d, want 1 (cached per client)", fetches)
	}

	fc.kill()
	catalog := r.Catalog()
	if len(catalog) != 1 || catalog[0].Ref != "deepseek/deepseek-chat" {
		t.Fatalf("catalog after crash = %v, want base only", catalog)
	}
}

// TestCatalogCoalescesConcurrentFirstFetch forces every caller through the
// same cold-cache window. Exactly one sidecar RPC may run; followers must
// receive that call's result rather than racing duplicate dynamic catalogs
// into the cache with last-completion-wins behavior.
func TestCatalogCoalescesConcurrentFirstFetch(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce sync.Once
	fc.catalogFn = func(ctx context.Context) ([]protocol.ProviderDescriptor, error) {
		startOnce.Do(func() { close(started) })
		select {
		case <-release:
			return []protocol.ProviderDescriptor{demoDescriptor()}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	r := testResolver(t, baseCatalog(), nil, fc)

	const callers = 32
	results := make(chan []provider.Descriptor, callers)
	for range callers {
		go func() { results <- r.Catalog() }()
	}
	select {
	case <-started:
	case <-time.After(testBudget):
		t.Fatal("catalog fetch never started")
	}
	if got := fc.fetchCount(); got != 1 {
		t.Fatalf("catalog fetches while first call is blocked = %d, want 1", got)
	}
	close(release)
	for range callers {
		select {
		case catalog := <-results:
			if len(catalog) != 2 || catalog[1].Ref != demoDescriptor().Ref {
				t.Fatalf("catalog = %+v, want base plus the shared sidecar result", catalog)
			}
		case <-time.After(testBudget):
			t.Fatal("concurrent Catalog caller did not receive the shared result")
		}
	}
	if got := fc.fetchCount(); got != 1 {
		t.Fatalf("catalog fetches = %d, want exactly 1", got)
	}
}

func TestCatalogSkipsFailedFetch(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	fc.catalogErr = errors.New("sidecar unavailable")
	r := testResolver(t, baseCatalog(), nil, fc)

	catalog := r.Catalog()
	if len(catalog) != 1 {
		t.Fatalf("catalog = %v, want base only on fetch failure", catalog)
	}
	// A failed fetch is not cached: the next call retries.
	fc.catalogErr = nil
	fc.catalog = []protocol.ProviderDescriptor{demoDescriptor()}
	if got := len(r.Catalog()); got != 2 {
		t.Fatalf("catalog after recovery = %d, want 2", got)
	}
}

func TestConflictWithoutClaimFails(t *testing.T) {
	base := &provider.StaticResolver{
		Descriptors: []provider.Descriptor{{Ref: "plugin/demo/fake/x", DisplayName: "host copy"}},
	}
	fc := newFakeClient("demo", demoDescriptor())
	_, err := New(base, func() []ProviderClient { return []ProviderClient{fc} }, nil)
	if err == nil {
		t.Fatal("New succeeded with an unclaimed provider conflict")
	}
	var conflictErr *ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("error %v is not a ConflictError", err)
	}
	if len(conflictErr.Conflicts) != 1 {
		t.Fatalf("conflicts = %+v", conflictErr.Conflicts)
	}
	conflict := conflictErr.Conflicts[0]
	if conflict.Ref != "plugin/demo/fake/x" || conflict.PluginID != "demo" {
		t.Fatalf("conflict = %+v", conflict)
	}
	if conflict.Slot != extension.SlotProviderRef("plugin/demo/fake/x") {
		t.Fatalf("conflict slot = %q", conflict.Slot)
	}
	// The diagnostic names both sources so the user can act on it.
	msg := err.Error()
	if !strings.Contains(msg, `"demo"`) || !strings.Contains(msg, "plugin/demo/fake/x") || !strings.Contains(msg, "host provider catalog") {
		t.Fatalf("conflict message = %q", msg)
	}
}

func TestConflictClaimedByOtherPluginFails(t *testing.T) {
	base := &provider.StaticResolver{
		Descriptors: []provider.Descriptor{{Ref: "plugin/demo/fake/x"}},
	}
	fc := newFakeClient("demo", demoDescriptor())
	claims := map[extension.Slot]extension.ContributionSource{
		extension.SlotProviderRef("plugin/demo/fake/x"): {PluginID: "someone-else"},
	}
	_, err := New(base, func() []ProviderClient { return []ProviderClient{fc} }, claims)
	var conflictErr *ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("error %v is not a ConflictError", err)
	}
}

func TestConflictWithClaimSidecarReplacesBase(t *testing.T) {
	base := &provider.StaticResolver{
		Descriptors: []provider.Descriptor{
			{Ref: "plugin/demo/fake/x", DisplayName: "host copy", Model: "x"},
			{Ref: "deepseek/deepseek-chat", DisplayName: "deepseek"},
		},
	}
	fc := newFakeClient("demo", demoDescriptor())
	fc.catalog = []protocol.ProviderDescriptor{demoDescriptor()}
	claims := map[extension.Slot]extension.ContributionSource{
		extension.SlotProviderRef("plugin/demo/fake/x"): {PluginID: "demo"},
	}
	r := testResolver(t, base, claims, fc)

	catalog := r.Catalog()
	if len(catalog) != 2 {
		t.Fatalf("catalog = %v, want the untouched base entry plus the sidecar replacement", catalog)
	}
	byRef := map[string]provider.Descriptor{}
	for _, d := range catalog {
		byRef[d.Ref] = d
	}
	replaced, ok := byRef["plugin/demo/fake/x"]
	if !ok {
		t.Fatalf("catalog lost the contested ref: %v", catalog)
	}
	if replaced.DisplayName != "Fake Demo" {
		t.Fatalf("contested ref descriptor = %+v, want the sidecar's (claim winner)", replaced)
	}
	if _, ok := byRef["deepseek/deepseek-chat"]; !ok {
		t.Fatalf("catalog lost the uncontested base entry: %v", catalog)
	}
}

func TestResolveRoutesPluginRefToSidecar(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)

	p, err := r.Resolve(provider.Selection{Ref: "plugin/demo/fake/x"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	ext, ok := p.(*Provider)
	if !ok {
		t.Fatalf("Resolve returned %T, want *providerext.Provider", p)
	}
	if ext.ref != "plugin/demo/fake/x" || ext.client != fc {
		t.Fatalf("provider = %+v", ext)
	}
	if p.Name() != "plugin" {
		t.Fatalf("Name() = %q, want the ref's first segment", p.Name())
	}
}

func TestResolvePluginPrefixRefMatchesDeclaration(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)

	// Broker-style partial ref: the provider without its model segment.
	p, err := r.Resolve(provider.Selection{Ref: "plugin/demo/fake"})
	if err != nil {
		t.Fatalf("Resolve prefix: %v", err)
	}
	if p.(*Provider).ref != "plugin/demo/fake/x" {
		t.Fatalf("provider ref = %q", p.(*Provider).ref)
	}
}

func TestResolvePluginRefNotRunning(t *testing.T) {
	r := testResolver(t, baseCatalog(), nil) // no sidecars at all
	_, err := r.Resolve(provider.Selection{Ref: "plugin/demo/fake/x"})
	if err == nil || !strings.Contains(err.Error(), `"demo"`) {
		t.Fatalf("Resolve error = %v, want unknown-ref naming the plugin", err)
	}
}

func TestResolvePluginRefNotDeclared(t *testing.T) {
	fc := newFakeClient("demo") // declares no providers
	r := testResolver(t, baseCatalog(), nil, fc)
	_, err := r.Resolve(provider.Selection{Ref: "plugin/demo/fake/x"})
	if err == nil || !strings.Contains(err.Error(), "does not declare") {
		t.Fatalf("Resolve error = %v, want not-declared", err)
	}
}

func TestResolveNonPluginRefUsesBase(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, baseCatalog(), nil, fc)

	p, err := r.Resolve(provider.Selection{Ref: "deepseek/deepseek-chat"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, ok := p.(staticProvider); !ok {
		t.Fatalf("Resolve returned %T, want the base provider", p)
	}
}

func TestResolveTwoSegmentPluginRefUsesBase(t *testing.T) {
	// "plugin/x" is an ordinary two-segment ref, not the plugin namespace.
	base := &provider.StaticResolver{
		Descriptors: []provider.Descriptor{{Ref: "plugin/x"}},
		Providers:   map[string]provider.Provider{"plugin/x": staticProvider("base-plugin")},
	}
	r := testResolver(t, base, nil)
	p, err := r.Resolve(provider.Selection{Ref: "plugin/x"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, ok := p.(staticProvider); !ok {
		t.Fatalf("Resolve returned %T, want the base provider", p)
	}
}

func TestResolveNeverFallsBackForPluginRefs(t *testing.T) {
	// The base resolver would happily serve a prefix/suffix match for the
	// plugin-shaped ref; the merged resolver must not let a plugin ref reach it.
	base := &provider.StaticResolver{
		Descriptors: []provider.Descriptor{{Ref: "fake/x"}},
		Providers:   map[string]provider.Provider{"fake/x": staticProvider("fake")},
	}
	fc := newFakeClient("demo", demoDescriptor())
	r := testResolver(t, base, nil, fc)
	fc.kill()

	p, err := r.Resolve(provider.Selection{Ref: "plugin/demo/fake/x"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, ok := p.(*Provider); !ok {
		t.Fatalf("Resolve fell back to %T after the crash", p)
	}
	_, err = p.Stream(context.Background(), provider.Request{})
	if !provider.IsStreamInterrupted(err) {
		t.Fatalf("Stream error = %v, want fail-fast interruption", err)
	}
}

func TestNewWithNoSidecarProvidersBehavesLikeBase(t *testing.T) {
	r := testResolver(t, baseCatalog(), nil)
	catalog := r.Catalog()
	if len(catalog) != 1 || catalog[0].Ref != "deepseek/deepseek-chat" {
		t.Fatalf("catalog = %v", catalog)
	}
	if _, err := r.Resolve(provider.Selection{Ref: "deepseek/deepseek-chat"}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
}

func TestNewNilBaseTolerated(t *testing.T) {
	fc := newFakeClient("demo", demoDescriptor())
	fc.catalog = []protocol.ProviderDescriptor{demoDescriptor()}
	r := testResolver(t, nil, nil, fc)
	catalog := r.Catalog()
	if len(catalog) != 1 || catalog[0].Ref != "plugin/demo/fake/x" {
		t.Fatalf("catalog = %v", catalog)
	}
}
