package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/mcplaunch"
	"reasonix/internal/tool"
)

type destructiveLazyTarget struct {
	name  string
	calls int
}

type mutableLazyTarget struct {
	name  string
	calls int
}

func (t *mutableLazyTarget) Name() string            { return t.name }
func (t *mutableLazyTarget) Description() string     { return "writer test target" }
func (t *mutableLazyTarget) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *mutableLazyTarget) ReadOnly() bool          { return false }
func (t *mutableLazyTarget) Execute(context.Context, json.RawMessage) (string, error) {
	t.calls++
	return "executed", nil
}

func (t *destructiveLazyTarget) Name() string             { return t.name }
func (t *destructiveLazyTarget) Description() string      { return "destructive test target" }
func (t *destructiveLazyTarget) Schema() json.RawMessage  { return json.RawMessage(`{"type":"object"}`) }
func (t *destructiveLazyTarget) ReadOnly() bool           { return true }
func (t *destructiveLazyTarget) MCPDestructiveHint() bool { return true }
func (t *destructiveLazyTarget) Execute(context.Context, json.RawMessage) (string, error) {
	t.calls++
	return "executed", nil
}

// helperSpec returns a Spec that re-invokes this test binary as a minimal MCP
// stdio server (see TestHelperProcess in plugin_test.go). Reused across every
// lazy_test case so the helper-process contract — "echo: <msg>" responder with
// tools/list exposing echo and zed — stays the single source of truth.
func helperSpec() Spec {
	return Spec{
		Name:    "mock",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env:     map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
	}
}

// writeMockCache primes the on-disk cache for spec with the two tools the
// helper subprocess exposes (echo, zed). We mirror the real schemas so a
// cache-hit lazyTool surfaces the same Schema() bytes that a freshly handshaked
// remoteTool would — the test for "model sees real schema before any Execute"
// depends on this equivalence.
func writeMockCache(t *testing.T, spec Spec) {
	t.Helper()
	cs := CachedSchema{
		CacheKey:     SchemaCacheKey(spec),
		Capabilities: map[string]bool{"prompts": false, "resources": false},
		Tools: []CachedTool{
			{
				Name:        "echo",
				Description: "Echo back the message.",
				Schema:      json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}},"required":["z","msg"]}`),
			},
			{
				Name:        "zed",
				Description: "Sorted after echo.",
				Schema:      json.RawMessage(`{"type":"object"}`),
			},
		},
	}
	if err := SaveCachedSchema(spec.Name, cs); err != nil {
		t.Fatalf("SaveCachedSchema: %v", err)
	}
}

// waitForServer polls host.ServerNames() until name appears or timeout
// elapses. The lazy path spawns via a goroutine, so tests need a bounded poll
// rather than a fixed sleep — five seconds covers a slow CI subprocess fork
// while still aborting clearly on a real hang.
func waitForServer(t *testing.T, host *Host, name string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, n := range host.ServerNames() {
			if n == name {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server %q never appeared in host.ServerNames() within %v (got %v)", name, timeout, host.ServerNames())
}

func waitForCachedSchema(t *testing.T, spec Spec, timeout time.Duration) *CachedSchema {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cs, ok := LoadCachedSchema(spec.Name, SchemaCacheKey(spec)); ok {
			return cs
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cached schema for %q never appeared within %v", spec.Name, timeout)
	return nil
}

func TestHostCloseWaitsForLazyBackgroundWrite(t *testing.T) {
	host := NewHost()
	started := make(chan struct{})
	release := make(chan struct{})
	host.queueBackgroundWrite(func() {
		close(started)
		<-release
	})
	<-started

	closed := make(chan struct{})
	go func() {
		host.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Host.Close returned before the lazy background write finished")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Host.Close did not return after the lazy background write finished")
	}
}

// TestLazyCacheHitSyncSpawn drives the cache-hit branch end-to-end: cache is
// pre-populated, the model can see real schemas before any spawn, and the
// first Execute synchronously handshakes, swaps the placeholder for the real
// *remoteTool, and forwards through in one turn. This is the "warm start"
// payoff — lazy plugins should be indistinguishable from eager once they have
// a cache.
func TestLazyCacheHitSyncSpawn(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()
	writeMockCache(t, spec)

	cs, ok := LoadCachedSchema(spec.Name, SchemaCacheKey(spec))
	if !ok {
		t.Fatal("LoadCachedSchema: miss right after save (sanity)")
	}

	host := NewHost()
	defer host.Close()
	reg := tool.NewRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tools := LazyToolset(spec, cs, host, reg, ctx, false)
	if len(tools) != 2 {
		t.Fatalf("LazyToolset returned %d tools, want 2 (echo + zed)", len(tools))
	}
	for _, lt := range tools {
		reg.Add(lt)
	}

	// Before any Execute: registry exposes real cached schemas (not the empty
	// {"type":"object"} stub). The model relies on this to call the tool with
	// real args on the very first turn.
	echoBefore, ok := reg.Get("mcp__mock__echo")
	if !ok {
		t.Fatal("registry missing mcp__mock__echo after LazyToolset")
	}
	if _, isLazy := echoBefore.(*lazyTool); !isLazy {
		t.Fatalf("pre-Execute echo should be a *lazyTool, got %T", echoBefore)
	}
	gotSchema := string(echoBefore.Schema())
	if !strings.Contains(gotSchema, `"msg"`) || !strings.Contains(gotSchema, `"required"`) {
		t.Fatalf("cached schema not surfaced through lazyTool.Schema(): %s", gotSchema)
	}

	// First Execute: cache-hit path runs the handshake synchronously and
	// forwards to the real tool — the user sees "echo: hi" in this same turn.
	out, err := echoBefore.Execute(ctx, json.RawMessage(`{"msg":"hi"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "echo: hi" {
		t.Fatalf("Execute result = %q, want %q", out, "echo: hi")
	}

	// The spawn actually happened — host now lists the mock server.
	names := host.ServerNames()
	if len(names) != 1 || names[0] != "mock" {
		t.Fatalf("host.ServerNames() = %v, want [mock]", names)
	}

	// After Execute, the registry entry must STILL be the placeholder: cache-hit
	// placeholders are pinned for the whole session so the request's tools
	// array stays byte-identical even when the live handshake differs from the
	// cache (see trySwap). Execution keeps forwarding to the real tool through
	// the shared spawn state.
	echoAfter, _ := reg.Get("mcp__mock__echo")
	if _, isLazy := echoAfter.(*lazyTool); !isLazy {
		t.Fatalf("post-Execute echo should remain the pinned *lazyTool, got %T", echoAfter)
	}
	if got := string(echoAfter.Schema()); got != gotSchema {
		t.Fatalf("registry schema bytes changed across the handshake:\nbefore: %s\nafter:  %s", gotSchema, got)
	}
	// Second call goes straight through the ready state to the real tool.
	out2, err := echoAfter.Execute(ctx, json.RawMessage(`{"msg":"again"}`))
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if out2 != "echo: again" {
		t.Fatalf("second Execute result = %q, want %q", out2, "echo: again")
	}
}

func TestLazyCacheHitReusesExistingSharedHostClient(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()
	writeMockCache(t, spec)

	cs, ok := LoadCachedSchema(spec.Name, SchemaCacheKey(spec))
	if !ok {
		t.Fatal("LoadCachedSchema: miss right after save (sanity)")
	}

	host := NewHost()
	defer host.Close()
	reg := tool.NewRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := host.Add(ctx, spec); err != nil {
		t.Fatalf("preconnect shared host: %v", err)
	}

	tools := LazyToolset(spec, cs, host, reg, ctx, false)
	for _, lt := range tools {
		reg.Add(lt)
	}
	echoBefore, ok := reg.Get("mcp__mock__echo")
	if !ok {
		t.Fatal("registry missing mcp__mock__echo after LazyToolset")
	}

	out, err := echoBefore.Execute(ctx, json.RawMessage(`{"msg":"hi"}`))
	if err != nil {
		t.Fatalf("Execute against existing shared host client: %v", err)
	}
	if out != "echo: hi" {
		t.Fatalf("Execute result = %q, want %q", out, "echo: hi")
	}
	if got := host.ServerNames(); len(got) != 1 || got[0] != "mock" {
		t.Fatalf("shared host should still have exactly one mock server, got %v", got)
	}
}

func TestLazyRemoveCancelsInFlightGenerationWithoutResurrection(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()
	spec.Env["GO_WANT_HELPER_INIT_MS"] = "500"
	host := NewHost()
	defer host.Close()
	reg := tool.NewRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, placeholder := range LazyToolset(spec, nil, host, reg, ctx, true) {
		reg.Add(placeholder)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		host.spawningMu.Lock()
		spawning := len(host.spawning) > 0
		host.spawningMu.Unlock()
		if spawning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lazy spawn never entered the in-flight state")
		}
		time.Sleep(5 * time.Millisecond)
	}

	prefix, found := host.Remove(spec.Name)
	if !found {
		t.Fatal("Host.Remove did not cancel the in-flight lazy generation")
	}
	reg.RemovePrefix(prefix)
	done := make(chan struct{})
	go func() {
		host.deferredWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled lazy generation did not finish")
	}
	if host.HasClient(spec.Name) || len(host.ServerNames()) != 0 {
		t.Fatalf("removed lazy server was resurrected: %v", host.ServerNames())
	}
	if _, ok := reg.Get(ToolPrefix(spec.Name) + "connect"); ok {
		t.Fatal("removed lazy placeholder was re-registered")
	}
	if _, ok := LoadCachedSchema(spec.Name, SchemaCacheKey(spec)); ok {
		t.Fatal("cancelled lazy generation wrote a new schema cache")
	}
	tools, err := host.Add(ctx, spec)
	if err != nil {
		t.Fatalf("re-add after cancelled generation: %v", err)
	}
	if len(tools) == 0 || !host.HasClient(spec.Name) {
		t.Fatalf("new generation did not connect after removal: tools=%d clients=%v", len(tools), host.ServerNames())
	}
}

func TestAddWithLifecycleCoalescesConcurrentSameServer(t *testing.T) {
	spec := helperSpec()
	spec.Env["GO_WANT_HELPER_INIT_MS"] = "200"

	host := NewHost()
	defer host.Close()
	lifeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := make(chan struct{})
	errs := make([]error, 2)
	toolCounts := make([]int, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			callCtx, cancelCall := context.WithTimeout(lifeCtx, 5*time.Second)
			defer cancelCall()
			tools, err := host.AddWithLifecycle(lifeCtx, callCtx, spec)
			errs[i] = err
			toolCounts[i] = len(tools)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("AddWithLifecycle call %d failed: %v (all errors: %v)", i, err, errs)
		}
		if toolCounts[i] != 2 {
			t.Fatalf("AddWithLifecycle call %d returned %d tools, want 2", i, toolCounts[i])
		}
	}
	if got := host.ServerNames(); len(got) != 1 || got[0] != "mock" {
		t.Fatalf("host should contain exactly one connected server, got %v", got)
	}
}

func TestLazyCacheHitStartupTimeoutCanRetry(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()
	spec.Env["GO_WANT_HELPER_INIT_MS"] = fmt.Sprint(int(defaultStartTimeout/time.Millisecond) + 200)
	writeMockCache(t, spec)

	cs, ok := LoadCachedSchema(spec.Name, SchemaCacheKey(spec))
	if !ok {
		t.Fatal("LoadCachedSchema: miss right after save (sanity)")
	}

	host := NewHost()
	defer host.Close()
	reg := tool.NewRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tools := LazyToolset(spec, cs, host, reg, ctx, false)
	for _, lt := range tools {
		reg.Add(lt)
	}
	echo, ok := reg.Get("mcp__mock__echo")
	if !ok {
		t.Fatal("registry missing mcp__mock__echo after LazyToolset")
	}
	lazyEcho, ok := echo.(*lazyTool)
	if !ok {
		t.Fatalf("pre-Execute echo should be a *lazyTool, got %T", echo)
	}

	if _, err := echo.Execute(ctx, json.RawMessage(`{"msg":"slow"}`)); err == nil || !strings.Contains(err.Error(), "startup timed out") {
		t.Fatalf("first Execute error = %v, want startup timed out", err)
	}

	lazyEcho.shared.spec.Env["GO_WANT_HELPER_INIT_MS"] = "0"
	out, err := echo.Execute(ctx, json.RawMessage(`{"msg":"retry"}`))
	if err != nil {
		t.Fatalf("second Execute after timeout should retry: %v", err)
	}
	if out != "echo: retry" {
		t.Fatalf("Execute result = %q, want %q", out, "echo: retry")
	}
}

func TestLazyToolsetInheritsInstalledServerReaderAuthorization(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()
	spec.LaunchManager = mcplaunch.NewManager(filepath.Join(t.TempDir(), mcplaunch.StateFilename), t.TempDir())
	spec.Authorized = true
	if err := SaveCachedSchema(spec.Name, CachedSchema{
		CacheKey: SchemaCacheKey(spec),
		Tools: []CachedTool{{
			Name: "echo", Description: "Echo back the message.",
			Schema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`), ReadOnly: true,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	cs, ok := LoadCachedSchema(spec.Name, SchemaCacheKey(spec))
	if !ok {
		t.Fatal("LoadCachedSchema: miss right after save")
	}

	host := NewHost()
	defer host.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tools := LazyToolset(spec, cs, host, tool.NewRegistry(), ctx, false)
	var echo tool.Tool
	for _, candidate := range tools {
		if candidate.Name() == "mcp__mock__echo" {
			echo = candidate
			break
		}
	}
	if echo == nil || !echo.ReadOnly() {
		t.Fatalf("installed cached reader missing or not read-only: %T", echo)
	}
	if authority, ok := echo.(tool.MCPServerAuthorization); !ok || !authority.MCPServerAuthorized() {
		t.Fatalf("lazy installed reader did not inherit authorization: %T", echo)
	}
}

// TestLazyCacheMissAsyncSpawn drives the cache-miss branch: with no cache, a
// single "connect" placeholder shows up; first Execute returns a retry hint and
// kicks the spawn async; once that spawn finishes, the registry swaps to the
// real tools under their real names, and the connect stub is dropped. This is
// the "model warm-up" contract — the model must not see stale schemas, so we
// refuse to forward the first call and instead ask for one more turn.
func TestLazyCacheMissAsyncSpawn(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()

	host := NewHost()
	defer host.Close()
	reg := tool.NewRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tools := LazyToolset(spec, nil, host, reg, ctx, false)
	if len(tools) != 1 {
		t.Fatalf("cache-miss LazyToolset must return 1 connect stub, got %d", len(tools))
	}
	for _, lt := range tools {
		reg.Add(lt)
	}

	connect, ok := reg.Get("mcp__mock__connect")
	if !ok {
		t.Fatalf("registry missing mcp__mock__connect; names=%v", reg.Names())
	}

	// First Execute must NOT forward — schema is unknown, so the model would
	// be feeding garbage. It returns a retry hint and triggers spawn async.
	_, err := connect.Execute(ctx, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("first Execute on cache-miss placeholder should error with a retry hint")
	}
	msg := err.Error()
	if !strings.Contains(msg, "initializing") && !strings.Contains(msg, "next turn") {
		t.Fatalf("first-Execute error %q should mention 'initializing' or 'next turn'", msg)
	}

	// Wait for the async spawn to complete (host.Add happens on the run()
	// goroutine kicked by Execute). The goroutine swaps the registry itself, so
	// the next model request sees the real schemas without another placeholder
	// Execute call.
	waitForServer(t, host, "mock", 5*time.Second)

	if _, found := reg.Get("mcp__mock__connect"); found {
		t.Errorf("connect stub should be removed after swap, names=%v", reg.Names())
	}
	if _, found := reg.Get("mcp__mock__echo"); !found {
		t.Errorf("real mcp__mock__echo missing after swap, names=%v", reg.Names())
	}
	if _, found := reg.Get("mcp__mock__zed"); !found {
		t.Errorf("real mcp__mock__zed missing after swap, names=%v", reg.Names())
	}
}

func TestLazySwapDoesNotRaceRegistrySchemas(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()
	spec.Env["GO_WANT_HELPER_INIT_MS"] = "50"
	writeMockCache(t, spec)
	cs, _ := LoadCachedSchema(spec.Name, SchemaCacheKey(spec))

	host := NewHost()
	defer host.Close()
	reg := tool.NewRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tools := LazyToolset(spec, cs, host, reg, ctx, false)
	for _, lt := range tools {
		reg.Add(lt)
	}
	echo, _ := reg.Get("mcp__mock__echo")
	if echo == nil {
		t.Fatal("missing mcp__mock__echo placeholder")
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				_ = reg.Schemas()
			}
		}
	}()

	out, err := echo.Execute(ctx, json.RawMessage(`{"msg":"race"}`))
	close(done)
	wg.Wait()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "echo: race" {
		t.Fatalf("Execute result = %q, want %q", out, "echo: race")
	}
}

// TestLazyBackgroundKick covers the background-tier path: kick=true plus a
// cache hit means the spawn races boot, finishes before the model calls, and
// the first Execute hits the "already-ready, swap on the way through" branch.
// The model never sees a placeholder schema-wise either, since the cache
// fed Schema() before kick even started.
func TestLazyBackgroundKick(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()
	writeMockCache(t, spec)
	cs, _ := LoadCachedSchema(spec.Name, SchemaCacheKey(spec))

	host := NewHost()
	defer host.Close()
	reg := tool.NewRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tools := LazyToolset(spec, cs, host, reg, ctx, true) // kick=true
	if len(tools) != 2 {
		t.Fatalf("LazyToolset(kick=true) returned %d tools, want 2", len(tools))
	}
	for _, lt := range tools {
		reg.Add(lt)
	}

	// Wait for the background spawn to complete — proof that kick fired off
	// the handshake without us calling Execute.
	waitForServer(t, host, "mock", 5*time.Second)

	// Now Execute: the state is already spawnReady, so this should swap +
	// forward in one shot without a second Add call. The result must still be
	// correct.
	echo, _ := reg.Get("mcp__mock__echo")
	out, err := echo.Execute(ctx, json.RawMessage(`{"msg":"bg"}`))
	if err != nil {
		t.Fatalf("Execute after background ready: %v", err)
	}
	if out != "echo: bg" {
		t.Fatalf("Execute result = %q, want %q", out, "echo: bg")
	}

	// One spawn, not two — kick + Execute must collapse onto the same run.
	if names := host.ServerNames(); len(names) != 1 {
		t.Fatalf("host.ServerNames() = %v, want exactly one 'mock'", names)
	}
}

func TestLazyBackgroundCacheMissPersistsSchemaAndCompletesAdvertisedConnect(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()

	host := NewHost()
	defer host.Close()
	reg := tool.NewRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tools := LazyToolset(spec, nil, host, reg, ctx, true) // cache miss + background kick
	if len(tools) != 1 {
		t.Fatalf("cache-miss LazyToolset returned %d tools, want one connect placeholder", len(tools))
	}
	connect, ok := tools[0].(*lazyTool)
	if !ok {
		t.Fatalf("cache-miss placeholder type = %T, want *lazyTool", tools[0])
	}
	for _, lt := range tools {
		reg.Add(lt)
	}

	waitForServer(t, host, "mock", 5*time.Second)
	cs := waitForCachedSchema(t, spec, 5*time.Second)
	if len(cs.Tools) != 2 {
		t.Fatalf("cached schema has %d tools, want 2", len(cs.Tools))
	}
	got := map[string]bool{}
	for _, ct := range cs.Tools {
		got[ct.Name] = true
	}
	if !got["echo"] || !got["zed"] {
		t.Fatalf("cached tools = %v, want echo and zed", got)
	}
	if _, found := reg.Get(connect.Name()); found {
		t.Fatalf("connect placeholder remained provider-visible after discovery; names=%v", reg.Names())
	}
	if out, err := connect.Execute(ctx, json.RawMessage(`{}`)); err != nil || !strings.Contains(out, "real tools are now available") {
		t.Fatalf("already-advertised connect after discovery = (%q, %v), want controlled connected result", out, err)
	}
}

func TestLazyBackgroundCloseCancelsInFlightKick(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()
	spec.Name = "slow"
	spec.Env["GO_WANT_HELPER_INIT_MS"] = "5000"
	writeMockCache(t, spec)
	cs, _ := LoadCachedSchema(spec.Name, SchemaCacheKey(spec))

	host := NewHost()
	reg := tool.NewRegistry()

	tools := LazyToolset(spec, cs, host, reg, context.Background(), true)
	for _, lt := range tools {
		reg.Add(lt)
	}

	done := make(chan struct{})
	go func() {
		host.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Host.Close did not cancel the in-flight background lazy spawn")
	}

	if names := host.ServerNames(); len(names) != 0 {
		t.Fatalf("closed host retained connected servers: %v", names)
	}
}

// TestLazyConcurrentExecuteOnlyOneSpawn pins the de-duplication contract: 10
// goroutines racing through Execute on the same lazyTool may only trigger ONE
// spawn (and therefore one connected mock server on the host). The state
// machine's mu+state gate is what makes this true; this test would catch a
// regression where someone moved the state transition outside the lock or
// swapped to a TOCTOU check.
//
// Note: by design (see lazy.go), only the winner of the race forwards
// synchronously; the losers observe spawnInFlight and return a "retry next
// turn" hint rather than blocking. We assert that contract too: at least one
// goroutine got "echo: r<i>", and the racers that didn't win got the
// initializing hint — never a spurious error and never a stale or partial
// result. After all goroutines complete, a fresh Execute hits spawnReady and
// forwards normally.
func TestLazyConcurrentExecuteOnlyOneSpawn(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()
	writeMockCache(t, spec)
	cs, _ := LoadCachedSchema(spec.Name, SchemaCacheKey(spec))

	host := NewHost()
	defer host.Close()
	reg := tool.NewRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tools := LazyToolset(spec, cs, host, reg, ctx, false)
	for _, lt := range tools {
		reg.Add(lt)
	}
	echo, _ := reg.Get("mcp__mock__echo")

	const goroutines = 10
	var wg sync.WaitGroup
	results := make([]string, goroutines)
	errs := make([]error, goroutines)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			out, err := echo.Execute(ctx, json.RawMessage(fmt.Sprintf(`{"msg":"r%d"}`, i)))
			results[i], errs[i] = out, err
		}(i)
	}
	wg.Wait()

	// Every result must be either the real "echo: rN" output or the explicit
	// initializing hint — nothing else. At least one goroutine (the racing
	// winner) must succeed, otherwise the state machine deadlocked the win.
	winners := 0
	for i, err := range errs {
		want := fmt.Sprintf("echo: r%d", i)
		switch {
		case err == nil && results[i] == want:
			winners++
		case err != nil && strings.Contains(err.Error(), "initializing"):
			// expected loser
		default:
			t.Errorf("goroutine %d: result=%q err=%v — must be either %q or an 'initializing' hint", i, results[i], err, want)
		}
	}
	if winners == 0 {
		t.Fatal("no goroutine succeeded — at least the race winner must forward through")
	}

	// Exactly one Client landed on the host: the mu+state gate kept the 9
	// losers off the spawn path. This is the headline invariant of the lazy
	// design — racing the first call must not fork-bomb the subprocess.
	mockCount := 0
	for _, n := range host.ServerNames() {
		if n == "mock" {
			mockCount++
		}
	}
	if mockCount != 1 {
		t.Fatalf("expected 1 'mock' server after concurrent Execute, got %d (names=%v)", mockCount, host.ServerNames())
	}

	// A follow-up Execute (now in spawnReady) goes through cleanly: the
	// "retry on next turn" hint was honest, not a permanent error.
	out, err := echo.Execute(ctx, json.RawMessage(`{"msg":"after"}`))
	if err != nil {
		t.Fatalf("post-race Execute: %v", err)
	}
	if out != "echo: after" {
		t.Fatalf("post-race Execute = %q, want %q", out, "echo: after")
	}
}

// TestLazyHandshakeFailureSurfaced covers the spawnFailed sticky branch: a
// bogus command can't start, the first Execute returns an error that mentions
// "failed to start", and a second Execute returns the SAME error (the state
// machine doesn't retry — we don't want to fork a doomed subprocess every
// turn until the user fixes config).
func TestLazyHandshakeFailureSurfaced(t *testing.T) {
	redirectCache(t)
	// Bogus command: process exec will fail outright.
	spec := Spec{Name: "missing", Command: "reasonix-nonexistent-binary-for-lazy-test"}

	// Hand-craft a cache so the cache-HIT branch runs (synchronous spawn,
	// failure surfaces directly to the first caller rather than via a retry
	// hint). The CacheKey must match — otherwise LoadCachedSchema would miss
	// and we'd be exercising the async path.
	cs := &CachedSchema{
		CacheKey:     SchemaCacheKey(spec),
		Capabilities: map[string]bool{},
		Tools: []CachedTool{{
			Name:        "doit",
			Description: "noop",
			Schema:      json.RawMessage(`{"type":"object"}`),
		}},
	}

	host := NewHost()
	defer host.Close()
	reg := tool.NewRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tools := LazyToolset(spec, cs, host, reg, ctx, false)
	if len(tools) != 1 {
		t.Fatalf("LazyToolset returned %d tools, want 1 (doit)", len(tools))
	}
	for _, lt := range tools {
		reg.Add(lt)
	}
	doit, _ := reg.Get("mcp__missing__doit")

	_, err1 := doit.Execute(ctx, json.RawMessage(`{}`))
	if err1 == nil {
		t.Fatal("Execute on a bogus command should error")
	}
	if !strings.Contains(err1.Error(), "failed to start") {
		t.Fatalf("error %q should mention 'failed to start'", err1.Error())
	}

	// Second call: same error, no retry. spawnFailed is sticky on purpose —
	// the operator must fix config and restart, not have us fork-bomb on
	// every turn.
	_, err2 := doit.Execute(ctx, json.RawMessage(`{}`))
	if err2 == nil {
		t.Fatal("second Execute after spawnFailed should still error")
	}
	if !strings.Contains(err2.Error(), "failed to start") {
		t.Fatalf("second error %q should still mention 'failed to start' (state machine must stay in spawnFailed)", err2.Error())
	}
}

// TestLazyToolsetCacheHitSchemaVisible is the model-facing visibility test:
// immediately after LazyToolset returns and BEFORE any Execute, lazyTool.Schema()
// must equal the canonicalized cached schema. The whole point of the cache is
// that the model sees real schemas at turn-start; if Schema() returned the
// "{}" stub here, the model would call with empty args and the cache-hit
// path would never get a useful first call.
func TestLazyToolsetCacheHitSchemaVisible(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()

	rawSchema := json.RawMessage(`{"properties":{"msg":{"type":"string"}},"type":"object","required":["msg"]}`)
	cs := &CachedSchema{
		CacheKey:     SchemaCacheKey(spec),
		Capabilities: map[string]bool{},
		Tools: []CachedTool{{
			Name:        "echo",
			Description: "Echo back.",
			Schema:      rawSchema,
		}},
	}

	host := NewHost()
	defer host.Close()
	reg := tool.NewRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tools := LazyToolset(spec, cs, host, reg, ctx, false)
	if len(tools) != 1 {
		t.Fatalf("LazyToolset returned %d tools, want 1", len(tools))
	}
	got := string(tools[0].Schema())
	want := string(canonicalizeSchema(rawSchema))
	if got != want {
		t.Fatalf("lazyTool.Schema() = %s,\nwant canonicalized cached schema = %s", got, want)
	}

	// And we never spawned: Schema() must be free, otherwise the cache
	// optimisation is moot.
	if names := host.ServerNames(); len(names) != 0 {
		t.Fatalf("Schema() must not spawn; host.ServerNames() = %v", names)
	}
}

// registrySchemaBytes marshals the registry's full tool schemas — the exact
// surface that feeds the provider request's tools array.
func registrySchemaBytes(t *testing.T, reg *tool.Registry) string {
	t.Helper()
	b, err := json.Marshal(reg.Schemas())
	if err != nil {
		t.Fatalf("marshal schemas: %v", err)
	}
	return string(b)
}

// TestLazyCacheHitPinsToolBytesAcrossDivergentHandshake is the session
// byte-stability guard: the cached snapshot deliberately DIFFERS from what the
// live handshake will report (stale description/schema, and it omits one tool
// the live server exposes). After the background spawn completes, the
// registry's schema bytes must be identical to what the model saw at boot —
// the divergence surfaces in the refreshed disk cache (next session), never
// mid-session in the tools array.
func TestLazyCacheHitPinsToolBytesAcrossDivergentHandshake(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()
	stale := CachedSchema{
		CacheKey:     SchemaCacheKey(spec),
		Capabilities: map[string]bool{},
		Tools: []CachedTool{{
			Name:        "echo",
			Description: "STALE description from a previous session.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`),
			// live handshake also exposes "zed" — absent here on purpose.
		}},
	}
	if err := SaveCachedSchema(spec.Name, stale); err != nil {
		t.Fatalf("SaveCachedSchema: %v", err)
	}
	cs, ok := LoadCachedSchema(spec.Name, SchemaCacheKey(spec))
	if !ok {
		t.Fatal("LoadCachedSchema miss after save")
	}

	host := NewHost()
	defer host.Close()
	reg := tool.NewRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for _, lt := range LazyToolset(spec, cs, host, reg, ctx, true) {
		reg.Add(lt)
	}
	bootBytes := registrySchemaBytes(t, reg)

	// Let the background handshake finish and give trySwap every chance to run.
	waitForServer(t, host, "mock", 5*time.Second)
	echo, _ := reg.Get("mcp__mock__echo")
	if out, err := echo.Execute(ctx, json.RawMessage(`{"msg":"pin"}`)); err != nil || out != "echo: pin" {
		t.Fatalf("Execute after schema drift = %q, %v; want live execution", out, err)
	}

	if got := registrySchemaBytes(t, reg); got != bootBytes {
		t.Fatalf("tools array bytes changed mid-session after a divergent handshake:\nboot: %s\nnow:  %s", bootBytes, got)
	}
	if _, found := reg.Get("mcp__mock__zed"); found {
		t.Fatal("live-only tool joined the registry mid-session; it must wait for the next session")
	}

	// The refreshed cache carries the live truth for the NEXT session. The
	// stale cache this test wrote is itself loadable, so poll until the
	// refresh actually lands (the background save races Execute's return on
	// slow machines) rather than accepting the first loadable snapshot.
	deadline := time.Now().Add(5 * time.Second)
	for {
		refreshed, ok := LoadCachedSchema(spec.Name, SchemaCacheKey(spec))
		if ok {
			names := map[string]bool{}
			for _, ct := range refreshed.Tools {
				names[ct.Name] = true
			}
			if names["echo"] && names["zed"] {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("refreshed cache tools = %v, want live set {echo, zed}", refreshed.Tools)
			}
		} else if time.Now().After(deadline) {
			t.Fatal("cached schema never became loadable")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLazyToolPromotesLiveDestructiveHintBeforeExecution(t *testing.T) {
	const name = "mcp__srv__wipe"
	target := &destructiveLazyTarget{name: name}
	shared := &lazySpawn{
		spec:    Spec{Name: "srv"},
		state:   spawnReady,
		real:    map[string]tool.Tool{name: target},
		swapped: true,
	}
	lazy := &lazyTool{
		shared:   shared,
		name:     name,
		rawName:  "wipe",
		readOnly: true,
		hasCache: true,
	}

	if out, err := lazy.Execute(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "retry") || out != "" {
		t.Fatalf("first Execute = (%q,%v), want retry before destructive execution", out, err)
	}
	if target.calls != 0 || !lazy.MCPDestructiveHint() {
		t.Fatalf("after promotion calls=%d destructive=%v, want 0/true", target.calls, lazy.MCPDestructiveHint())
	}

	out, err := lazy.Execute(context.Background(), nil)
	if err != nil || out != "executed" || target.calls != 1 {
		t.Fatalf("second Execute = (%q,%v), calls=%d, want execution after metadata refresh retry", out, err, target.calls)
	}
}

func TestLazyToolDemotesStaleReaderBeforeExecution(t *testing.T) {
	const name = "mcp__srv__mutate"
	target := &mutableLazyTarget{name: name}
	shared := &lazySpawn{
		spec:    Spec{Name: "srv"},
		state:   spawnReady,
		real:    map[string]tool.Tool{name: target},
		swapped: true,
	}
	lazy := &lazyTool{
		shared: shared, name: name, rawName: "mutate", readOnly: true, hasCache: true,
	}

	if out, err := lazy.Execute(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "Plan/read-only safety boundary") || out != "" {
		t.Fatalf("first Execute = (%q,%v), want retry before writer execution", out, err)
	}
	if target.calls != 0 || lazy.ReadOnly() {
		t.Fatalf("after demotion calls=%d readOnly=%v, want 0/false", target.calls, lazy.ReadOnly())
	}

	out, err := lazy.Execute(context.Background(), nil)
	if err != nil || out != "executed" || target.calls != 1 {
		t.Fatalf("second Execute = (%q,%v), calls=%d", out, err, target.calls)
	}
}

// TestLazyEmptyCachedToolsFallsBackToConnectStub: a snapshot with zero tools
// presents nothing the model could call, so it must take the cache-miss stub
// path instead of letting live tools join the registry mid-session unnamed.
func TestLazyEmptyCachedToolsFallsBackToConnectStub(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()
	cs := &CachedSchema{CacheKey: SchemaCacheKey(spec), Tools: nil}

	host := NewHost()
	defer host.Close()
	reg := tool.NewRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tools := LazyToolset(spec, cs, host, reg, ctx, false)
	if len(tools) != 1 || tools[0].Name() != "mcp__mock__connect" {
		t.Fatalf("empty-cache toolset = %v, want single connect stub", tools)
	}
}

// TestAddWithLifecycleSurvivesHandshakeCtxCancel proves the on-demand proxy
// pattern: connect with a short handshake budget, cancel it immediately after
// connect, and the stdio child must stay alive (its lifetime is lifeCtx) so
// the tool call that triggered the connect can still execute.
func TestAddWithLifecycleSurvivesHandshakeCtxCancel(t *testing.T) {
	spec := helperSpec()
	host := NewHost()
	defer host.Close()

	lifeCtx, cancelLife := context.WithCancel(context.Background())
	defer cancelLife()
	handshakeCtx, cancelHandshake := context.WithTimeout(context.Background(), 5*time.Second)
	tools, err := host.AddWithLifecycle(lifeCtx, handshakeCtx, spec)
	cancelHandshake() // the proxy's deferred cancel fires right after connect
	if err != nil {
		t.Fatalf("AddWithLifecycle: %v", err)
	}
	var echo tool.Tool
	for _, tl := range tools {
		if strings.HasSuffix(tl.Name(), "__echo") {
			echo = tl
		}
	}
	if echo == nil {
		t.Fatalf("no echo tool in %d tools", len(tools))
	}
	out, err := echo.Execute(context.Background(), json.RawMessage(`{"msg":"hi"}`))
	if err != nil {
		t.Fatalf("Execute after handshake ctx cancel: %v — the child died with the handshake context", err)
	}
	if out != "echo: hi" {
		t.Fatalf("Execute result = %q, want %q", out, "echo: hi")
	}
}
