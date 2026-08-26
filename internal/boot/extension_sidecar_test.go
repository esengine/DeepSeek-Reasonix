package boot

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/sidecar"
	"reasonix/internal/pluginpkg"
)

// Boot-level fake sidecar (re-exec helper-process pattern, mirroring the
// sidecar package's own tests): the boot test binary re-executes itself with
// REASONIX_BOOT_FAKE_SIDECAR=1 and speaks Extension Protocol v2 over
// stdin/stdout. REASONIX_BOOT_FAKE_INIT_RESULT overrides the initialize
// result; REASONIX_BOOT_FAKE_MODE=ignore_shutdown keeps the process alive
// through extension/shutdown. Intercept steering for the dispatch tests:
//
//	REASONIX_BOOT_FAKE_BLOCK_EVENT      answer block at this event
//	REASONIX_BOOT_FAKE_INVALID_EVENT    answer a DTO-violating replace at this event
//	REASONIX_BOOT_FAKE_REPLACE_PROMPT   answer system_prompt.build replace with this prompt
//	REASONIX_BOOT_FAKE_REPLACE_INPUT    answer input.receive replace with this text
//	REASONIX_BOOT_FAKE_EVENT_LOG        append one "event payload" line per extension/event
//
// Provider steering for the stage 7 adapter tests:
//
//	REASONIX_BOOT_FAKE_PLUGIN_NAME      the installed plugin name (provider ref namespace)
//	REASONIX_BOOT_FAKE_PROVIDER         when "1", declare plugin/<name>/fake/x and serve
//	                                    catalog/stream/open/stream/cancel with a fixed
//	                                    two-chunk completion plus usage
//
// UI steering for the stage 8a hub tests:
//
//	REASONIX_BOOT_FAKE_UI_PUBLISH       when "1", publish one credential-bearing
//	                                    status surface through host/ui/publish after
//	                                    the handshake completes
//
// Process-lifecycle steering for the failure-cleanup tests:
//
//	REASONIX_BOOT_FAKE_PID_FILE         write the sidecar PID to this file on start,
//	                                    so the parent can poll for a leaked process
//	REASONIX_BOOT_FAKE_EXIT_IMMEDIATELY when "1", write the PID file (if set) and
//	                                    exit 0 at once — a sidecar that dies before
//	                                    answering the handshake
const (
	bootFakeEnvEnable          = "REASONIX_BOOT_FAKE_SIDECAR"
	bootFakeEnvInitResult      = "REASONIX_BOOT_FAKE_INIT_RESULT"
	bootFakeEnvMode            = "REASONIX_BOOT_FAKE_MODE"
	bootFakeEnvBlockEvent      = "REASONIX_BOOT_FAKE_BLOCK_EVENT"
	bootFakeEnvInvalidEvent    = "REASONIX_BOOT_FAKE_INVALID_EVENT"
	bootFakeEnvReplacePrompt   = "REASONIX_BOOT_FAKE_REPLACE_PROMPT"
	bootFakeEnvReplaceInput    = "REASONIX_BOOT_FAKE_REPLACE_INPUT"
	bootFakeEnvEventLog        = "REASONIX_BOOT_FAKE_EVENT_LOG"
	bootFakeEnvPluginName      = "REASONIX_BOOT_FAKE_PLUGIN_NAME"
	bootFakeEnvProvider        = "REASONIX_BOOT_FAKE_PROVIDER"
	bootFakeEnvUIPublish       = "REASONIX_BOOT_FAKE_UI_PUBLISH"
	bootFakeEnvPIDFile         = "REASONIX_BOOT_FAKE_PID_FILE"
	bootFakeEnvExitImmediately = "REASONIX_BOOT_FAKE_EXIT_IMMEDIATELY"
)

// TestExtensionFakeSidecarHelperProcess is the re-exec entry point; it skips
// in the parent run.
func TestExtensionFakeSidecarHelperProcess(t *testing.T) {
	if os.Getenv(bootFakeEnvEnable) != "1" {
		t.Skip("boot fake sidecar helper process")
	}
	runBootFakeSidecar(os.Stdin, os.Stdout)
	os.Exit(0)
}

func runBootFakeSidecar(stdin io.Reader, stdout io.Writer) {
	if pidFile := strings.TrimSpace(os.Getenv(bootFakeEnvPIDFile)); pidFile != "" {
		_ = os.WriteFile(pidFile, fmt.Appendf(nil, "%d", os.Getpid()), 0o644)
	}
	if os.Getenv(bootFakeEnvExitImmediately) == "1" {
		os.Exit(0)
	}
	out := bufio.NewWriter(stdout)
	var writeMu sync.Mutex
	write := func(format string, args ...any) {
		writeMu.Lock()
		defer writeMu.Unlock()
		fmt.Fprintf(out, format+"\n", args...)
		_ = out.Flush()
	}
	pluginName := strings.TrimSpace(os.Getenv(bootFakeEnvPluginName))
	providerMode := os.Getenv(bootFakeEnvProvider) == "1" && pluginName != ""
	providerRef := "plugin/" + pluginName + "/fake/x"
	providerDescriptor := func() string {
		return fmt.Sprintf(`{"ref":%q,"displayName":"Boot Fake","model":"x","contextWindow":64000,"tools":true,"reasoning":true,"efforts":["low","high"],"defaultEffort":"low"}`, providerRef)
	}
	initResult := strings.TrimSpace(os.Getenv(bootFakeEnvInitResult))
	if initResult == "" && providerMode {
		initResult = fmt.Sprintf(`{"protocolVersion":"2","name":"boot-fake","version":"1.0.0","stateSchemaVersion":0,"providers":[%s]}`, providerDescriptor())
	}
	if initResult == "" {
		initResult = `{"protocolVersion":"2","name":"boot-fake","version":"1.0.0","stateSchemaVersion":0}`
	}
	ignoreShutdown := os.Getenv(bootFakeEnvMode) == "ignore_shutdown"

	// streamFakeCompletion answers stream/open and then pushes the fixed
	// completion — two text chunks and one usage chunk, sealed by stream/end —
	// from its own goroutine so the read loop keeps answering other requests.
	streamFakeCompletion := func(id json.RawMessage, rawParams json.RawMessage) {
		var params struct {
			StreamID string `json:"streamId"`
			Request  struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"request"`
		}
		_ = json.Unmarshal(rawParams, &params)
		hasFinish := false
		for _, schema := range params.Request.Tools {
			hasFinish = hasFinish || schema.Name == "finish"
		}
		write(`{"jsonrpc":"2.0","id":%s,"result":{"accepted":true}}`, string(id))
		go func() {
			chunk := func(seq int, body string) {
				write(`{"jsonrpc":"2.0","method":"extension/provider/stream/chunk","params":{"streamId":%q,"seq":%d,"chunk":%s}}`, params.StreamID, seq, body)
			}
			chunk(1, `{"type":"text","text":"fake-hello "}`)
			chunk(2, `{"type":"text","text":"fake-world"}`)
			lastSeq := 3
			if hasFinish {
				chunk(3, `{"type":"tool_call","toolCall":{"id":"boot-fake-finish","name":"finish","arguments":"{\"outcome\":\"completed\"}"}}`)
				lastSeq = 4
			}
			chunk(lastSeq, `{"type":"usage","usage":{"promptTokens":5,"completionTokens":7,"totalTokens":12,"cacheHitTokens":2,"cacheMissTokens":3,"reasoningTokens":4,"finishReason":"stop"}}`)
			write(`{"jsonrpc":"2.0","method":"extension/provider/stream/end","params":{"streamId":%q,"lastSeq":%d}}`, params.StreamID, lastSeq)
		}()
	}

	in := bufio.NewReader(stdin)
	var sessionID string
	var generation uint64
	uiPublish := os.Getenv(bootFakeEnvUIPublish) == "1"
	for {
		line, err := in.ReadBytes('\n')
		if len(line) > 0 {
			var frame struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if json.Unmarshal(line, &frame) == nil && frame.Method != "" {
				var result string
				switch frame.Method {
				case "extension/initialize":
					var params struct {
						Session struct {
							SessionID  string `json:"sessionId"`
							Generation uint64 `json:"generation"`
						} `json:"session"`
					}
					_ = json.Unmarshal(frame.Params, &params)
					sessionID = params.Session.SessionID
					generation = params.Session.Generation
					result = initResult
				case "extension/initialized":
					// notification; the stage-8a publish mode fires one
					// credential-bearing status surface once the handshake
					// completes (the host must redact before surfacing).
					if uiPublish {
						uiPublish = false
						payload, _ := json.Marshal(map[string]any{
							"surfaceId": "boot-status", "sessionId": sessionID, "generation": generation,
							"kind": "status",
							"payload": map[string]any{
								"label": "boot fake ready api_key=sk-abcdef1234567890SECRETKEY", "severity": "info",
							},
						})
						write(`{"jsonrpc":"2.0","id":66001,"method":"host/ui/publish","params":%s}`, string(payload))
					}
					continue
				case "extension/ui/action":
					result = `{"accepted":true,"message":"boot fake action ran"}`
				case "extension/ui/submit":
					result = `{"accepted":true}`
				case "extension/intercept":
					result = bootFakeInterceptAnswer(frame.Params)
				case "extension/event":
					bootFakeLogEvent(frame.Params)
					continue // notification: never answer
				case "extension/provider/catalog":
					result = fmt.Sprintf(`{"providers":[%s]}`, providerDescriptor())
				case "extension/provider/stream/open":
					streamFakeCompletion(frame.ID, frame.Params)
					continue
				case "extension/provider/stream/cancel":
					result = `{"cancelled":true}`
				case "extension/shutdown":
					if ignoreShutdown {
						continue
					}
					write(`{"jsonrpc":"2.0","id":%s,"result":{"accepted":true}}`, string(frame.ID))
					return
				default:
					continue
				}
				write(`{"jsonrpc":"2.0","id":%s,"result":%s}`, string(frame.ID), result)
			}
		}
		if err != nil {
			return
		}
	}
}

// bootFakeInterceptAnswer computes the steered ruling for one
// extension/intercept call from the env knobs.
func bootFakeInterceptAnswer(rawParams json.RawMessage) string {
	var params struct {
		Event   string          `json:"event"`
		Payload json.RawMessage `json:"payload"`
	}
	_ = json.Unmarshal(rawParams, &params)
	switch {
	case params.Event != "" && params.Event == os.Getenv(bootFakeEnvBlockEvent):
		return `{"decision":"block","reason":"boot fake block"}`
	case params.Event != "" && params.Event == os.Getenv(bootFakeEnvInvalidEvent):
		// A replacement that fails the point's DTO: the host must treat it as
		// a contract violation, not apply it.
		return `{"decision":"replace","replacement":{"bogus":true}}`
	case params.Event == "system_prompt.build" && os.Getenv(bootFakeEnvReplacePrompt) != "":
		// Echo the incoming workspaceRoot back so the replacement passes the
		// payload DTO validation.
		var payload struct {
			WorkspaceRoot string `json:"workspaceRoot"`
		}
		_ = json.Unmarshal(params.Payload, &payload)
		replacement, _ := json.Marshal(map[string]string{
			"prompt":        os.Getenv(bootFakeEnvReplacePrompt),
			"workspaceRoot": payload.WorkspaceRoot,
		})
		return fmt.Sprintf(`{"decision":"replace","replacement":%s}`, string(replacement))
	case params.Event == "input.receive" && os.Getenv(bootFakeEnvReplaceInput) != "":
		replacement, _ := json.Marshal(map[string]string{"text": os.Getenv(bootFakeEnvReplaceInput)})
		return fmt.Sprintf(`{"decision":"replace","replacement":%s}`, string(replacement))
	default:
		return `{"decision":"continue"}`
	}
}

// bootFakeLogEvent appends one "event payload" line per extension/event
// notification to the env-named log file, so the parent test can assert what
// observers received.
func bootFakeLogEvent(rawParams json.RawMessage) {
	logPath := os.Getenv(bootFakeEnvEventLog)
	if logPath == "" {
		return
	}
	var params struct {
		Event   string          `json:"event"`
		Payload json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(rawParams, &params) != nil {
		return
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", params.Event, string(params.Payload))
}

// installBootFakePlugin installs an enabled v2 runtime package (the
// re-executed test binary) into the pluginpkg state under home.
func installBootFakePlugin(t *testing.T, home, name string, runtime map[string]any) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	env := map[string]any{bootFakeEnvEnable: "1"}
	for key, value := range runtime {
		if key == "env" {
			for k, v := range value.(map[string]string) {
				env[k] = v
			}
			delete(runtime, "env")
		}
	}
	runtime["command"] = exe
	runtime["args"] = []string{"-test.run=^TestExtensionFakeSidecarHelperProcess$"}
	runtime["env"] = env

	root := filepath.Join(home, "plugins", name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest, err := json.Marshal(map[string]any{
		"apiVersion": pluginpkg.ManifestAPIVersionV2,
		"name":       name,
		"version":    "1.0.0",
		"runtime":    runtime,
	})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, pluginpkg.NativeManifest), manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{
		Name: name, Root: pluginpkg.RelativeRoot(home, root), Version: "1.0.0", Enabled: true,
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
}

// bootWithFakePlugin builds the runtime fixture with one installed sidecar
// package and returns the build result.
func bootWithFakePlugin(t *testing.T, name string, runtime map[string]any) *BuildResult {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	installBootFakePlugin(t, config.ReasonixHomeDir(), name, runtime)
	res, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	t.Cleanup(res.Controller.Close)
	return res
}

func TestBootIsolatesIncompatibleExternalPlugin(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)

	root := robustTempDir(t)
	if err := os.WriteFile(filepath.Join(root, pluginpkg.NativeManifest), []byte(`{"name":"irmia-devkit","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	home := config.ReasonixHomeDir()
	if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: "irmia-devkit", Root: root, Enabled: true}); err != nil {
		t.Fatal(err)
	}

	res, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("incompatible plugin blocked core controller: %v", err)
	}
	t.Cleanup(res.Controller.Close)
	if res.Controller == nil || res.Extensions != nil {
		t.Fatalf("build result = %#v, want core controller without extensions", res)
	}
	state, err := pluginpkg.LoadState(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Plugins) != 1 || state.Plugins[0].Status != pluginpkg.PluginStatusDisabledIncompatible {
		t.Fatalf("plugin state = %#v", state.Plugins)
	}
}

func TestBootStartsExtensionSidecar(t *testing.T) {
	res := bootWithFakePlugin(t, "bootplugin", map[string]any{
		"intercepts": []string{"input.receive"},
	})
	if res.Extensions == nil {
		t.Fatal("BuildRuntime returned no extension manager")
	}
	if res.Runtime == nil || res.Runtime.Len() < 1 {
		t.Fatalf("runtime set holds %d effects, want at least the sidecar manager", res.Runtime.Len())
	}
	client := res.Extensions.Client("bootplugin")
	if client == nil {
		t.Fatal("manager has no client for bootplugin")
	}

	// The sidecar speaks the real protocol: ping it with an intercept.
	result, err := client.Intercept(context.Background(), protocol.EventInputReceive, json.RawMessage(`{"text":"ping"}`), 5*time.Second)
	if err != nil {
		t.Fatalf("Intercept: %v", err)
	}
	if result.Decision != protocol.DecisionContinue {
		t.Fatalf("decision = %q", result.Decision)
	}

	// The snapshot catalog carries the declaration-level contribution.
	if res.Snapshot == nil {
		t.Fatal("snapshot is nil")
	}
	stubs := res.Snapshot.Catalog().Get(extension.KindInterceptor, "input.receive")
	if len(stubs) != 1 || stubs[0].Source.PluginID != "bootplugin" {
		t.Fatalf("interceptor stubs = %+v", stubs)
	}

	// Controller teardown retires the sidecar: process exits, runtime set
	// closes with the controller generation.
	res.Controller.Close()
	waitForCond(t, "sidecar process exit", 10*time.Second, client.Exited)
	if !res.Runtime.Closed() {
		t.Fatal("runtime set was not closed by controller teardown")
	}
}

func TestBootExtensionStrategyClaimInSnapshot(t *testing.T) {
	res := bootWithFakePlugin(t, "claimer", map[string]any{
		"replaces": []string{"compaction"},
	})
	if res.Snapshot == nil {
		t.Fatal("snapshot is nil")
	}
	owner, ok := res.Snapshot.Replacements()[extension.SlotCompaction]
	if !ok || owner.PluginID != "claimer" {
		t.Fatalf("compaction slot owner = %+v (ok=%v)", owner, ok)
	}
}

func TestBootFailsWhenTwoRuntimesClaimOneSlot(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	reasonixHome := config.ReasonixHomeDir()
	installBootFakePlugin(t, reasonixHome, "claim-one", map[string]any{
		"replaces": []string{"system_prompt"},
	})
	installBootFakePlugin(t, reasonixHome, "claim-two", map[string]any{
		"replaces": []string{"system_prompt"},
	})
	_, err := BuildRuntime(context.Background(), Options{})
	if err == nil {
		t.Fatal("BuildRuntime succeeded with two runtimes claiming system_prompt")
	}
	var slotErr *extension.SlotConflictError
	if !errors.As(err, &slotErr) {
		t.Fatalf("error %v is not a SlotConflictError", err)
	}
}

func TestBootFailsWhenRequiredRuntimeFails(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	installBootFakePlugin(t, config.ReasonixHomeDir(), "required-broken", map[string]any{
		"required": true,
		// Extension Protocol v1 is rejected by the v2 host.
		"env": map[string]string{bootFakeEnvInitResult: `{"protocolVersion":"1","name":"x","version":"1","stateSchemaVersion":0}`},
	})
	_, err := BuildRuntime(context.Background(), Options{})
	if err == nil {
		t.Fatal("BuildRuntime succeeded with a broken required runtime")
	}
	var requiredErr *sidecar.RequiredStartError
	if !errors.As(err, &requiredErr) {
		t.Fatalf("error %v is not a RequiredStartError", err)
	}
}

func TestBootOptionalRuntimeFailureDegradesToWarning(t *testing.T) {
	res := bootWithFakePlugin(t, "optional-broken", map[string]any{
		"env": map[string]string{bootFakeEnvInitResult: `{"protocolVersion":"1","name":"x","version":"1","stateSchemaVersion":0}`},
	})
	// Optional failure: boot succeeds, no manager, empty runtime set.
	if res.Extensions != nil {
		t.Fatal("broken optional runtime produced a manager")
	}
	if res.Runtime == nil || res.Runtime.Len() != 0 {
		t.Fatalf("runtime set holds %d closers, want 0", res.Runtime.Len())
	}
}

// TestRebuildRetiresOldSidecars pins the Rebuild contract: the old
// controller's Close retires its sidecars, while the replacement build's
// sidecars keep serving their own generation.
func TestRebuildRetiresOldSidecars(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	installBootFakePlugin(t, config.ReasonixHomeDir(), "rebuildplugin", map[string]any{})

	oldRes, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	newRes, err := Rebuild(context.Background(), oldRes.Controller, Options{})
	if err != nil {
		oldRes.Controller.Close()
		t.Fatalf("Rebuild: %v", err)
	}
	t.Cleanup(newRes.Controller.Close)
	if oldRes.Extensions == nil || newRes.Extensions == nil {
		t.Fatal("both builds must have extension managers")
	}
	oldClient := oldRes.Extensions.Client("rebuildplugin")
	newClient := newRes.Extensions.Client("rebuildplugin")
	if oldClient == nil || newClient == nil {
		t.Fatal("both builds must have a sidecar client")
	}
	if oldRes.Snapshot.Generation() == newRes.Snapshot.Generation() {
		t.Fatal("rebuild reused the old generation")
	}

	// Closing the old controller retires the old sidecar only.
	oldRes.Controller.Close()
	waitForCond(t, "old sidecar exit", 10*time.Second, oldClient.Exited)
	result, err := newClient.Intercept(context.Background(), protocol.EventSessionStart, json.RawMessage(`{}`), 5*time.Second)
	if err != nil || result.Decision != protocol.DecisionContinue {
		t.Fatalf("new sidecar Intercept after old close = %+v, %v", result, err)
	}

	newRes.Controller.Close()
	waitForCond(t, "new sidecar exit", 10*time.Second, newClient.Exited)
}

// TestExplicitReloadReplacesUnchangedSidecar pins the linked-development
// contract: a user-requested reload must start a fresh process even when the
// manifest graph is unchanged. The provider-visible prefix stays stable when
// the replacement contributes identical bytes.
func TestExplicitReloadReplacesUnchangedSidecar(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	pidFile := filepath.Join(dir, "linked-sidecar.pid")
	installBootFakePlugin(t, config.ReasonixHomeDir(), "linkedplugin", map[string]any{
		"env": map[string]string{bootFakeEnvPIDFile: pidFile},
	})

	oldRes, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	oldPID := readFakePID(t, pidFile)
	if err := os.Remove(pidFile); err != nil {
		oldRes.Controller.Close()
		t.Fatalf("remove first-generation PID file: %v", err)
	}
	newRes, err := RebuildFrom(context.Background(), oldRes, Options{
		RuntimeReload: RuntimeReload{ForceFullRebuild: true},
	})
	if err != nil {
		oldRes.Controller.Close()
		t.Fatalf("RebuildFrom: %v", err)
	}
	t.Cleanup(newRes.Controller.Close)

	oldClient := oldRes.Extensions.Client("linkedplugin")
	newClient := newRes.Extensions.Client("linkedplugin")
	if oldClient == nil || newClient == nil {
		t.Fatal("both generations must have a sidecar client")
	}
	if oldClient == newClient {
		t.Fatal("explicit reload adopted the outgoing sidecar instead of starting a replacement")
	}
	newPID := readFakePID(t, pidFile)
	if newPID == oldPID {
		t.Fatalf("explicit reload kept sidecar PID %d", oldPID)
	}
	if oldClient.Exited() {
		t.Fatal("outgoing sidecar exited before the replacement controller published")
	}
	if oldRes.Snapshot.CacheHash() != newRes.Snapshot.CacheHash() {
		t.Fatalf("unchanged extension bytes changed cache hash: old=%s new=%s", oldRes.Snapshot.CacheHash(), newRes.Snapshot.CacheHash())
	}

	oldRes.Controller.Close()
	waitForCond(t, "outgoing sidecar exit", 10*time.Second, oldClient.Exited)
	if newClient.Exited() {
		t.Fatal("replacement sidecar exited with the outgoing controller")
	}
}

// TestExplicitReloadSidecarFailureKeepsOldProcess proves that forcing a fresh
// linked process does not weaken reload failure atomicity. The replacement can
// fail before publish while the previous process keeps answering requests.
func TestExplicitReloadSidecarFailureKeepsOldProcess(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	installBootFakePlugin(t, config.ReasonixHomeDir(), "stable-linked", map[string]any{
		"required": true,
	})

	oldRes, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	t.Cleanup(oldRes.Controller.Close)
	oldClient := oldRes.Extensions.Client("stable-linked")
	if oldClient == nil {
		t.Fatal("first build has no sidecar client")
	}

	// Keep the declared graph identical while making the linked program fail on
	// its next launch. Without the explicit-restart instruction, RebuildFrom
	// would adopt oldClient and incorrectly report success.
	installBootFakePlugin(t, config.ReasonixHomeDir(), "stable-linked", map[string]any{
		"required": true,
		"env":      map[string]string{bootFakeEnvExitImmediately: "1"},
	})
	_, err = RebuildFrom(context.Background(), oldRes, Options{
		RuntimeReload: RuntimeReload{ForceFullRebuild: true},
	})
	if err == nil {
		t.Fatal("explicit reload succeeded after the replacement sidecar failed")
	}
	var requiredErr *sidecar.RequiredStartError
	if !errors.As(err, &requiredErr) {
		t.Fatalf("reload error %v is not a RequiredStartError", err)
	}
	if oldClient.Exited() || oldRes.Runtime.Closed() {
		t.Fatal("failed explicit reload retired the outgoing runtime")
	}
	result, interceptErr := oldClient.Intercept(context.Background(), protocol.EventSessionStart, json.RawMessage(`{}`), 5*time.Second)
	if interceptErr != nil || result.Decision != protocol.DecisionContinue {
		t.Fatalf("outgoing sidecar after failed reload = %+v, %v", result, interceptErr)
	}
}

func waitForCond(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
