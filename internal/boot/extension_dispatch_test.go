package boot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
)

// Stage 6b1 dispatch wiring tests: the boot builds the dispatcher, runs the
// system_prompt.build strategy before the snapshot freezes, and broadcasts
// the final prompt to observers — all without letting dispatch results flow
// back into the frozen snapshot.

func TestBootSystemPromptStrategyReplacementLandsInSnapshot(t *testing.T) {
	res := bootWithFakePlugin(t, "prompt-owner", map[string]any{
		"replaces": []string{"system_prompt"},
		"env":      map[string]string{bootFakeEnvReplacePrompt: "EXTENSION PROMPT"},
	})
	if res.Dispatcher == nil {
		t.Fatal("BuildRuntime returned no dispatcher with a live sidecar")
	}
	if got := res.Snapshot.SystemPrompt(); got != "EXTENSION PROMPT" {
		t.Fatalf("snapshot system prompt = %q, want the strategy replacement %q", got, "EXTENSION PROMPT")
	}
	// Stage 6b2 handoff: the snapshot (and its cache hash) records the
	// strategy's final prompt, and the live executor session was swapped to
	// the same prompt before the build returned — the session's system
	// message is exactly what the agent will send on its first request.
	if got := systemMessage(res.Controller.History()); got != "EXTENSION PROMPT" {
		t.Fatalf("controller system message = %.80q, want the strategy-replaced prompt", got)
	}
}

// TestBootObservationOnlyKeepsSessionPrompt guards the swap condition: a
// build whose strategy did NOT replace the prompt must leave the executor
// session on the host-composed prompt (byte-identical pre-dispatch path).
func TestBootObservationOnlyKeepsSessionPrompt(t *testing.T) {
	res := bootWithFakePlugin(t, "observer", map[string]any{
		"intercepts": []string{"input.receive"},
	})
	if res.Dispatcher == nil {
		t.Fatal("BuildRuntime returned no dispatcher with a live sidecar")
	}
	if got := systemMessage(res.Controller.History()); !strings.Contains(got, "BASE SYSTEM PROMPT") {
		t.Fatalf("controller system message = %.80q, want the host-composed prompt", got)
	}
}

func TestBootSystemPromptStrategyAttribution(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	home := config.ReasonixHomeDir()

	// Baseline: no sidecars — no dispatcher, and the golden-path fingerprint.
	plain, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	t.Cleanup(plain.Controller.Close)
	if plain.Dispatcher != nil || plain.Extensions != nil {
		t.Fatal("no-plugin build produced a dispatcher or manager")
	}
	baseHash := plain.Snapshot.CacheHash()

	// An observation-only sidecar (continue at input.receive) must not
	// perturb the provider-visible fingerprint at all.
	installBootFakePlugin(t, home, "observer", map[string]any{
		"intercepts": []string{"input.receive"},
	})
	observed, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("BuildRuntime with observer: %v", err)
	}
	t.Cleanup(observed.Controller.Close)
	if observed.Dispatcher == nil {
		t.Fatal("observer build returned no dispatcher")
	}
	if got := observed.Snapshot.CacheHash(); got != baseHash {
		t.Fatalf("observation-only build changed the cache hash: %s vs %s", got, baseHash)
	}

	// A strategy replacement lands IN the hash: two builds whose owner
	// returns different prompts produce different hashes, and both differ
	// from the host-composed baseline. That is attribution — the fingerprint
	// covers what the session was actually built with.
	installBootFakePlugin(t, home, "strategist", map[string]any{
		"replaces": []string{"system_prompt"},
		"env":      map[string]string{bootFakeEnvReplacePrompt: "PROMPT A"},
	})
	buildA, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("BuildRuntime with strategist A: %v", err)
	}
	t.Cleanup(buildA.Controller.Close)
	if got := buildA.Snapshot.SystemPrompt(); got != "PROMPT A" {
		t.Fatalf("build A prompt = %q, want PROMPT A", got)
	}
	if buildA.Snapshot.CacheHash() == baseHash {
		t.Fatal("strategy-replaced prompt did not change the cache hash")
	}

	installBootFakePlugin(t, home, "strategist", map[string]any{
		"replaces": []string{"system_prompt"},
		"env":      map[string]string{bootFakeEnvReplacePrompt: "PROMPT B"},
	})
	buildB, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("BuildRuntime with strategist B: %v", err)
	}
	t.Cleanup(buildB.Controller.Close)
	if got := buildB.Snapshot.SystemPrompt(); got != "PROMPT B" {
		t.Fatalf("build B prompt = %q, want PROMPT B", got)
	}
	if buildB.Snapshot.CacheHash() == buildA.Snapshot.CacheHash() {
		t.Fatal("different strategy replacements produced the same cache hash — attribution is broken")
	}
}

// TestBootStableExtensionCacheGuard proves that an enabled deterministic
// strategy has one stable provider-visible prefix across independent runtime
// generations. Installing the extension may intentionally create one cold
// prefix versus the no-extension baseline; identical reloads must not create
// a new one.
func TestBootStableExtensionCacheGuard(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	installBootFakePlugin(t, config.ReasonixHomeDir(), "stable-strategist", map[string]any{
		"replaces": []string{"system_prompt"},
		"env":      map[string]string{bootFakeEnvReplacePrompt: "STABLE EXTENSION PROMPT"},
	})

	first, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("first BuildRuntime: %v", err)
	}
	firstPrompt := first.Snapshot.SystemPrompt()
	firstHash := first.Snapshot.CacheHash()
	firstSystemHash, firstToolsHash := first.Snapshot.CacheShape()
	firstSchemas, err := json.Marshal(first.Snapshot.ToolSchemas())
	if err != nil {
		first.Controller.Close()
		t.Fatalf("marshal first tool schemas: %v", err)
	}
	firstSessionPrompt := systemMessage(first.Controller.History())
	first.Controller.Close()

	second, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("second BuildRuntime: %v", err)
	}
	t.Cleanup(second.Controller.Close)
	secondSchemas, err := json.Marshal(second.Snapshot.ToolSchemas())
	if err != nil {
		t.Fatalf("marshal second tool schemas: %v", err)
	}
	secondSystemHash, secondToolsHash := second.Snapshot.CacheShape()

	if second.Snapshot.Generation() == first.Snapshot.Generation() {
		t.Fatal("independent builds reused the same runtime generation")
	}
	if got := second.Snapshot.SystemPrompt(); got != firstPrompt {
		t.Fatalf("stable extension prompt drifted: %q vs %q", got, firstPrompt)
	}
	if got := systemMessage(second.Controller.History()); got != firstSessionPrompt {
		t.Fatalf("controller prompt drifted across stable reload: %q vs %q", got, firstSessionPrompt)
	}
	if got := second.Snapshot.CacheHash(); got != firstHash {
		t.Fatalf("stable extension cache hash drifted: %s vs %s", got, firstHash)
	}
	if secondSystemHash != firstSystemHash || secondToolsHash != firstToolsHash {
		t.Fatalf("stable extension cache shape drifted: system %s/%s tools %s/%s",
			firstSystemHash, secondSystemHash, firstToolsHash, secondToolsHash)
	}
	if string(secondSchemas) != string(firstSchemas) {
		t.Fatal("stable extension tool schema bytes drifted across reload")
	}
}

func TestBootSystemPromptStrategyFailureFailsBuild(t *testing.T) {
	t.Run("block", func(t *testing.T) {
		isolateConfigHome(t)
		dir := robustTempDir(t)
		t.Chdir(dir)
		writeRuntimeFixture(t, dir)
		installBootFakePlugin(t, config.ReasonixHomeDir(), "blocker", map[string]any{
			"replaces": []string{"system_prompt"},
			"env":      map[string]string{bootFakeEnvBlockEvent: "system_prompt.build"},
		})
		_, err := BuildRuntime(context.Background(), Options{})
		if err == nil {
			t.Fatal("BuildRuntime succeeded with a blocking system_prompt owner")
		}
		var blockErr *dispatch.BlockError
		if !errors.As(err, &blockErr) {
			t.Fatalf("error %v is not a dispatch.BlockError", err)
		}
	})
	t.Run("contract violation", func(t *testing.T) {
		isolateConfigHome(t)
		dir := robustTempDir(t)
		t.Chdir(dir)
		writeRuntimeFixture(t, dir)
		installBootFakePlugin(t, config.ReasonixHomeDir(), "violator", map[string]any{
			"replaces": []string{"system_prompt"},
			"env":      map[string]string{bootFakeEnvInvalidEvent: "system_prompt.build"},
		})
		_, err := BuildRuntime(context.Background(), Options{})
		if err == nil {
			t.Fatal("BuildRuntime succeeded with a DTO-violating system_prompt owner")
		}
		var violationErr *dispatch.ViolationError
		if !errors.As(err, &violationErr) {
			t.Fatalf("error %v is not a dispatch.ViolationError", err)
		}
	})
}

func TestBootSystemPromptBuildEventObserved(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	home := config.ReasonixHomeDir()
	eventLog := filepath.Join(dir, "events.log")

	// The slot owner replaces the prompt; the observer subscribes to
	// system_prompt.build and records what the broadcast carries.
	installBootFakePlugin(t, home, "prompt-owner", map[string]any{
		"replaces": []string{"system_prompt"},
		"env":      map[string]string{bootFakeEnvReplacePrompt: "EXTENSION PROMPT"},
	})
	installBootFakePlugin(t, home, "prompt-watcher", map[string]any{
		"intercepts": []string{"system_prompt.build"},
		"env":        map[string]string{bootFakeEnvEventLog: eventLog},
	})
	res, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	t.Cleanup(res.Controller.Close)

	// The notification is delivered when the host writes the frame; the fake
	// logs it on its own read loop, so poll instead of reading once.
	var line string
	waitForCond(t, "system_prompt.build event observation", 10*time.Second, func() bool {
		data, err := os.ReadFile(eventLog)
		if err != nil {
			return false
		}
		for candidate := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
			if strings.HasPrefix(candidate, "system_prompt.build ") {
				line = candidate
				return true
			}
		}
		return false
	})
	if !strings.Contains(line, `"prompt":"EXTENSION PROMPT"`) {
		t.Fatalf("observer saw %q, want the final replaced prompt", line)
	}
}

// TestBootSnapshotImmutableAcrossDispatch is the cache-safety invariant: no
// dispatch call — intercept replace, strategy, or event — ever flows back
// into the frozen snapshot. A replacement at input.receive shapes only that
// call's payload; SystemPrompt, ToolSchemas, and CacheHash stay byte-identical.
func TestBootSnapshotImmutableAcrossDispatch(t *testing.T) {
	res := bootWithFakePlugin(t, "input-rewriter", map[string]any{
		"intercepts": []string{"input.receive"},
		"env":        map[string]string{bootFakeEnvReplaceInput: "REPLACED INPUT"},
	})
	if res.Dispatcher == nil {
		t.Fatal("BuildRuntime returned no dispatcher")
	}
	snap := res.Snapshot
	promptBefore := snap.SystemPrompt()
	schemasBefore, err := json.Marshal(snap.ToolSchemas())
	if err != nil {
		t.Fatalf("marshal tool schemas: %v", err)
	}
	hashBefore := snap.CacheHash()

	// A replace at input.receive affects only that call's payload.
	payload := dispatch.InputPayload{Text: "original user text"}
	result, err := res.Dispatcher.Intercept(context.Background(), extension.PointInputReceive, &payload)
	if err != nil {
		t.Fatalf("Intercept: %v", err)
	}
	if result.Blocked || len(result.Applied) != 1 || result.Applied[0] != "input-rewriter" {
		t.Fatalf("intercept result = %+v, want one applied replacement", result)
	}
	if payload.Text != "REPLACED INPUT" {
		t.Fatalf("payload text = %q, want the extension replacement", payload.Text)
	}

	// Strategy + event traffic flows too (session_policy is unowned here, so
	// the strategy is a no-op; events are fire-and-forget).
	if err := res.Dispatcher.RunStrategy(context.Background(), extension.SlotSessionPolicy, extension.PointSessionSave,
		&dispatch.SessionPayload{SessionPath: "/tmp/s.jsonl", Phase: dispatch.PhaseSave}); err != nil {
		t.Fatalf("RunStrategy on an unowned slot: %v", err)
	}
	res.Dispatcher.Event(extension.PointSessionStart, dispatch.SessionPayload{Phase: dispatch.PhaseStart})
	res.Dispatcher.Event(extension.PointFrontendEvent, dispatch.FrontendEventPayload{Kind: "notice", Text: "hi"})

	if got := snap.SystemPrompt(); got != promptBefore {
		t.Fatal("dispatch mutated the snapshot system prompt")
	}
	schemasAfter, err := json.Marshal(snap.ToolSchemas())
	if err != nil {
		t.Fatalf("marshal tool schemas after: %v", err)
	}
	if string(schemasAfter) != string(schemasBefore) {
		t.Fatal("dispatch mutated the snapshot tool schemas")
	}
	if got := snap.CacheHash(); got != hashBefore {
		t.Fatalf("dispatch mutated the snapshot cache hash: %s vs %s", got, hashBefore)
	}
}
