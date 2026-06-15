package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/tool"
)

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
		SpecHash:     SpecFingerprint(spec),
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

	cs, ok := LoadCachedSchema(spec.Name, SpecFingerprint(spec))
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

	// After Execute, the registry entry must be the real *remoteTool, not the
	// placeholder. Subsequent calls bypass the state machine and Execute
	// directly. Compare via type name so this test doesn't need to import the
	// unexported type by name.
	echoAfter, _ := reg.Get("mcp__mock__echo")
	if got := fmt.Sprintf("%T", echoAfter); !strings.Contains(got, "remoteTool") {
		t.Fatalf("post-Execute echo should be a remoteTool, got %s", got)
	}
}

func TestLazyToolsetAppliesSpecReadOnlyOverrideToCachedTools(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()
	cachedSpec := spec
	writeMockCache(t, cachedSpec)
	spec.ReadOnlyToolNames = map[string]bool{"echo": true}

	cs, ok := LoadCachedSchema(spec.Name, SpecFingerprint(spec))
	if !ok {
		t.Fatal("LoadCachedSchema: miss right after save (sanity)")
	}

	host := NewHost()
	defer host.Close()
	reg := tool.NewRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tools := LazyToolset(spec, cs, host, reg, ctx, false)
	byName := map[string]tool.Tool{}
	for _, tl := range tools {
		byName[tl.Name()] = tl
	}
	echo := byName["mcp__mock__echo"]
	if echo == nil {
		t.Fatalf("mcp__mock__echo missing from %v", byName)
	}
	if !echo.ReadOnly() {
		t.Fatal("lazy cached echo should use the spec read-only override")
	}
	zed := byName["mcp__mock__zed"]
	if zed == nil {
		t.Fatalf("mcp__mock__zed missing from %v", byName)
	}
	if zed.ReadOnly() {
		t.Fatal("lazy cached zed should keep cached non-read-only status")
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
	cs, _ := LoadCachedSchema(spec.Name, SpecFingerprint(spec))

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
	cs, _ := LoadCachedSchema(spec.Name, SpecFingerprint(spec))

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

func TestLazyBackgroundCloseCancelsInFlightKick(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()
	spec.Name = "slow"
	spec.Env["GO_WANT_HELPER_INIT_MS"] = "5000"
	writeMockCache(t, spec)
	cs, _ := LoadCachedSchema(spec.Name, SpecFingerprint(spec))

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
	cs, _ := LoadCachedSchema(spec.Name, SpecFingerprint(spec))

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
	// hint). The SpecHash must match — otherwise LoadCachedSchema would miss
	// and we'd be exercising the async path.
	cs := &CachedSchema{
		SpecHash:     SpecFingerprint(spec),
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
		SpecHash:     SpecFingerprint(spec),
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
