package boot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/providerext"
	"reasonix/internal/extension/sidecar"
	"reasonix/internal/provider"
)

// First-boot coverage for extension-hosted providers (stage 7 follow-up):
// sidecars start in preflight BEFORE model resolution, so a plugin-namespaced
// default_model resolves and streams on the very first build, and switching
// to/from it rides the ordinary Rebuild path.

// writePluginDefaultFixture writes the shared runtime fixture with
// default_model pointed at a plugin-namespaced ref.
func writePluginDefaultFixture(t *testing.T, dir, pluginRef string) {
	t.Helper()
	writeFile(t, dir, "reasonix.toml", fmt.Sprintf(`
default_model = %q

[agent]
system_prompt = "BASE SYSTEM PROMPT"

[environment]
enabled = false

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`, pluginRef))
}

// installProviderFake installs the fake sidecar in provider mode under name.
func installProviderFake(t *testing.T, home, name string, extraEnv map[string]string) {
	t.Helper()
	env := map[string]string{
		bootFakeEnvPluginName: name,
		bootFakeEnvProvider:   "1",
	}
	maps.Copy(env, extraEnv)
	installBootFakePlugin(t, home, name, map[string]any{
		"capabilities": []string{"providers"},
		"env":          env,
	})
}

// runTurnAndCollectAssistant drives one synchronous turn and returns the
// concatenated assistant text it appended to history.
func runTurnAndCollectAssistant(t *testing.T, res *BuildResult, input string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := res.Controller.RunTurn(ctx, input); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	var sb strings.Builder
	for _, m := range res.Controller.History() {
		if m.Role == provider.RoleAssistant {
			sb.WriteString(m.Content)
		}
	}
	return sb.String()
}

func TestBootFirstBootPluginDefaultModelStreams(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	name := "firstboot"
	ref := "plugin/" + name + "/fake/x"
	writePluginDefaultFixture(t, dir, ref)
	installProviderFake(t, config.ReasonixHomeDir(), name, nil)

	res, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("BuildRuntime with plugin default_model: %v", err)
	}
	t.Cleanup(res.Controller.Close)

	// The executor was built from the plugin ref on the FIRST boot — before
	// any resolver merge of earlier stages, model resolution itself routed
	// through the merged catalog.
	if got := res.Controller.ModelRef(); got != ref {
		t.Fatalf("executor model ref = %q, want %q", got, ref)
	}
	// The only streamable provider in this build is the extension one (the
	// config provider's endpoint is unreachable), so the fixed fake
	// completion in history proves the executor streams from the sidecar.
	assistant := runTurnAndCollectAssistant(t, res, "say hi")
	if !strings.Contains(assistant, "fake-hello fake-world") {
		t.Fatalf("assistant text = %q, want the extension provider's fixed completion", assistant)
	}
}

func TestBootUnknownPluginRefListsAvailableRefs(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writePluginDefaultFixture(t, dir, "plugin/nope/x/y")
	installProviderFake(t, config.ReasonixHomeDir(), "providerdemo", nil)

	_, err := BuildRuntime(context.Background(), Options{})
	if err == nil {
		t.Fatal("BuildRuntime succeeded with an unknown plugin default_model")
	}
	if !errors.Is(err, ErrUnknownModel) {
		t.Fatalf("error %v is not boot.ErrUnknownModel", err)
	}
	if !strings.Contains(err.Error(), `"plugin/nope/x/y"`) {
		t.Fatalf("error %q should name the requested ref", err)
	}
	if !strings.Contains(err.Error(), "plugin/providerdemo/fake/x") {
		t.Fatalf("error %q should list the available plugin ref", err)
	}
}

// TestBootSwitchToPluginModelStreams pins the switch path: boot on the
// builtin model, Rebuild onto the plugin ref, and the replacement streams
// from the extension provider while the old generation stays fully alive
// until the caller closes it.
func TestBootSwitchToPluginModelStreams(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	name := "switchdemo"
	ref := "plugin/" + name + "/fake/x"
	installProviderFake(t, config.ReasonixHomeDir(), name, nil)

	oldRes, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	if oldRes.Extensions == nil || oldRes.Extensions.Client(name) == nil {
		t.Fatal("first build has no sidecar client")
	}
	oldClient := oldRes.Extensions.Client(name)

	newRes, err := Rebuild(context.Background(), oldRes.Controller, Options{Model: ref})
	if err != nil {
		oldRes.Controller.Close()
		t.Fatalf("Rebuild onto plugin ref: %v", err)
	}
	t.Cleanup(newRes.Controller.Close)

	// The old generation is untouched by the rebuild: its sidecar still
	// serves, its runtime set is open.
	if oldClient.Exited() {
		t.Fatal("Rebuild retired the old sidecar before the swap completed")
	}
	if oldRes.Runtime.Closed() {
		t.Fatal("Rebuild closed the old runtime set")
	}
	if got := newRes.Controller.ModelRef(); got != ref {
		t.Fatalf("switched model ref = %q, want %q", got, ref)
	}
	assistant := runTurnAndCollectAssistant(t, newRes, "say hi")
	if !strings.Contains(assistant, "fake-hello fake-world") {
		t.Fatalf("switched turn = %q, want the extension provider's fixed completion", assistant)
	}

	// Closing the old controller after the swap retires only its generation.
	oldRes.Controller.Close()
	waitForCond(t, "old sidecar exit", 10*time.Second, oldClient.Exited)
	newClient := newRes.Extensions.Client(name)
	if newClient == nil || newClient.Exited() {
		t.Fatal("the switched generation's sidecar died with the old controller")
	}
}

// TestBootStartsExtensionPackagesOncePerBuild pins the single-start contract:
// preflight starts the generation's sidecars and snapshot assembly reuses
// that same Manager — no second StartPackages anywhere in one build.
func TestBootStartsExtensionPackagesOncePerBuild(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	installBootFakePlugin(t, config.ReasonixHomeDir(), "counted", map[string]any{})

	calls := 0
	orig := startExtensionPackages
	startExtensionPackages = func(ctx context.Context, home string, sessionCtx protocol.SessionContext, ui sidecar.UIHandler, previous *sidecar.Manager, plan *extension.RuntimePlan) (*sidecar.Manager, []string, error) {
		calls++
		return orig(ctx, home, sessionCtx, ui, previous, plan)
	}
	t.Cleanup(func() { startExtensionPackages = orig })

	res, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	t.Cleanup(res.Controller.Close)
	if calls != 1 {
		t.Fatalf("StartPackages ran %d times in one build, want exactly 1", calls)
	}
	if res.Extensions == nil || res.Extensions.Client("counted") == nil {
		t.Fatal("the preflighted manager did not reach the build result")
	}
}

func TestBootRedactsExtensionStartupWarnings(t *testing.T) {
	const secret = "sk-abcdef1234567890SECRETKEY"
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	installBootFakePlugin(t, config.ReasonixHomeDir(), "warning-source", map[string]any{})

	orig := startExtensionPackages
	startExtensionPackages = func(ctx context.Context, home string, sessionCtx protocol.SessionContext, ui sidecar.UIHandler, previous *sidecar.Manager, plan *extension.RuntimePlan) (*sidecar.Manager, []string, error) {
		manager, warnings, err := orig(ctx, home, sessionCtx, ui, previous, plan)
		return manager, append(warnings, "sidecar rejected api_key="+secret), err
	}
	t.Cleanup(func() { startExtensionPackages = orig })

	var notices []string
	res, err := BuildRuntime(context.Background(), Options{Sink: event.FuncSink(func(ev event.Event) {
		if ev.Kind == event.Notice {
			notices = append(notices, ev.Text)
		}
	})})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	t.Cleanup(res.Controller.Close)
	joined := strings.Join(notices, "\n")
	if strings.Contains(joined, secret) {
		t.Fatalf("extension startup notice leaked a credential: %q", joined)
	}
	if !strings.Contains(joined, "****") {
		t.Fatalf("extension startup notice contains no redaction marker: %q", joined)
	}
}

// readFakePID polls for the fake sidecar's PID file and returns the PID.
func readFakePID(t *testing.T, path string) int {
	t.Helper()
	pid := 0
	waitForCond(t, "fake sidecar PID file", 10*time.Second, func() bool {
		body, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(body)))
		return err == nil && pid > 0
	})
	return pid
}

// TestBootRequiredExitLeavesNoSidecarProcess: a required sidecar that exits
// before answering the handshake fails the build with RequiredStartError —
// and its process is gone, not leaked.
func TestBootRequiredExitLeavesNoSidecarProcess(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	pidFile := filepath.Join(dir, "sidecar.pid")
	installBootFakePlugin(t, config.ReasonixHomeDir(), "required-dies", map[string]any{
		"required": true,
		"env": map[string]string{
			bootFakeEnvPIDFile:         pidFile,
			bootFakeEnvExitImmediately: "1",
		},
	})

	_, err := BuildRuntime(context.Background(), Options{})
	if err == nil {
		t.Fatal("BuildRuntime succeeded with a required sidecar that exits immediately")
	}
	var requiredErr *sidecar.RequiredStartError
	if !errors.As(err, &requiredErr) {
		t.Fatalf("error %v is not a RequiredStartError", err)
	}
	pid := readFakePID(t, pidFile)
	waitForCond(t, "required sidecar process exit", 10*time.Second, func() bool { return !pidAlive(pid) })
}

// TestBootProviderConflictLeavesNoSidecarProcess: a provider-ref conflict
// without the plugin's claim fails the build with ConflictError — and the
// preflighted sidecar is retired, not leaked.
func TestBootProviderConflictLeavesNoSidecarProcess(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	name := "conflicter"
	writeRuntimeFixtureWithConflictingProvider(t, dir, name)
	pidFile := filepath.Join(dir, "sidecar.pid")
	installProviderFake(t, config.ReasonixHomeDir(), name, map[string]string{
		bootFakeEnvPIDFile: pidFile,
	})

	_, err := BuildRuntime(context.Background(), Options{})
	if err == nil {
		t.Fatal("BuildRuntime succeeded with an unclaimed provider conflict")
	}
	var conflictErr *providerext.ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("error %v is not a providerext.ConflictError", err)
	}
	pid := readFakePID(t, pidFile)
	waitForCond(t, "conflicted sidecar process exit", 10*time.Second, func() bool { return !pidAlive(pid) })
}

// TestRebuildPluginPreflightFailureKeepsOldRuntime pins reload atomicity with
// extensions: a Rebuild whose NEW generation fails preflight (a newly
// installed required plugin cannot start) returns the error and leaves the
// old controller, its manager, and its sidecars fully usable.
func TestRebuildPluginPreflightFailureKeepsOldRuntime(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	installBootFakePlugin(t, config.ReasonixHomeDir(), "stable", map[string]any{})

	oldRes, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	t.Cleanup(oldRes.Controller.Close)
	oldClient := oldRes.Extensions.Client("stable")
	if oldClient == nil {
		t.Fatal("first build has no sidecar client")
	}

	// A newly installed required plugin dies immediately: the replacement
	// build's preflight must fail before touching the old generation.
	installBootFakePlugin(t, config.ReasonixHomeDir(), "required-dies", map[string]any{
		"required": true,
		"env":      map[string]string{bootFakeEnvExitImmediately: "1"},
	})
	_, err = Rebuild(context.Background(), oldRes.Controller, Options{})
	if err == nil {
		t.Fatal("Rebuild succeeded with a required plugin that cannot start")
	}
	var requiredErr *sidecar.RequiredStartError
	if !errors.As(err, &requiredErr) {
		t.Fatalf("Rebuild error %v is not a RequiredStartError", err)
	}

	// Old runtime fully usable: sidecar alive and answering, runtime set open.
	if oldClient.Exited() {
		t.Fatal("failed Rebuild retired the old sidecar")
	}
	if oldRes.Runtime.Closed() {
		t.Fatal("failed Rebuild closed the old runtime set")
	}
	result, ierr := oldClient.Intercept(context.Background(), protocol.EventSessionStart, json.RawMessage(`{}`), 5*time.Second)
	if ierr != nil || result.Decision != protocol.DecisionContinue {
		t.Fatalf("old sidecar Intercept after failed Rebuild = %+v, %v", result, ierr)
	}
}
