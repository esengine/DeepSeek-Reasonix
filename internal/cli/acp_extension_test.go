package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/acp"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/pluginpkg"
	"reasonix/internal/provider"
)

// ACP-level coverage for extension-hosted providers: a plugin/... model in
// session/new resolves and streams through boot's preflighted sidecar, and a
// mid-session switch (RebuildSession) moves to a config model and back.

const (
	acpFakeEnvEnable     = "REASONIX_ACP_FAKE_SIDECAR"
	acpFakeEnvPluginName = "REASONIX_ACP_FAKE_PLUGIN_NAME"
)

// TestACPFakeSidecarHelperProcess is the re-exec entry point for the ACP fake
// sidecar; it skips in the parent run. Mirrors the boot package's fake
// sidecar (which is test-scoped and not importable).
func TestACPFakeSidecarHelperProcess(t *testing.T) {
	if os.Getenv(acpFakeEnvEnable) != "1" {
		t.Skip("acp fake sidecar helper process")
	}
	runACPFakeSidecar(os.Stdin, os.Stdout)
	os.Exit(0)
}

func runACPFakeSidecar(stdin io.Reader, stdout io.Writer) {
	out := bufio.NewWriter(stdout)
	var writeMu sync.Mutex
	write := func(format string, args ...any) {
		writeMu.Lock()
		defer writeMu.Unlock()
		fmt.Fprintf(out, format+"\n", args...)
		_ = out.Flush()
	}
	pluginName := strings.TrimSpace(os.Getenv(acpFakeEnvPluginName))
	providerRef := "plugin/" + pluginName + "/fake/x"
	descriptor := fmt.Sprintf(`{"ref":%q,"displayName":"ACP Fake","model":"x","contextWindow":64000,"tools":true}`, providerRef)
	initResult := fmt.Sprintf(`{"protocolVersion":"2","name":"acp-fake","version":"1.0.0","stateSchemaVersion":0,"providers":[%s]}`, descriptor)

	streamCompletion := func(id json.RawMessage, rawParams json.RawMessage) {
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
			chunk(1, `{"type":"text","text":"acp-fake-hello "}`)
			chunk(2, `{"type":"text","text":"acp-fake-world"}`)
			lastSeq := 3
			if hasFinish {
				chunk(3, `{"type":"tool_call","toolCall":{"id":"acp-fake-finish","name":"finish","arguments":"{\"outcome\":\"completed\"}"}}`)
				lastSeq = 4
			}
			chunk(lastSeq, `{"type":"usage","usage":{"promptTokens":5,"completionTokens":7,"totalTokens":12,"cacheHitTokens":2,"cacheMissTokens":3,"reasoningTokens":4,"finishReason":"stop"}}`)
			write(`{"jsonrpc":"2.0","method":"extension/provider/stream/end","params":{"streamId":%q,"lastSeq":%d}}`, params.StreamID, lastSeq)
		}()
	}

	in := bufio.NewReader(stdin)
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
					result = initResult
				case "extension/provider/catalog":
					result = fmt.Sprintf(`{"providers":[%s]}`, descriptor)
				case "extension/provider/stream/open":
					streamCompletion(frame.ID, frame.Params)
					continue
				case "extension/provider/stream/cancel":
					result = `{"cancelled":true}`
				case "extension/shutdown":
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

// installACPFakeProviderPlugin installs the re-executed test binary as an
// enabled v1 runtime package declaring one extension provider.
func installACPFakeProviderPlugin(t *testing.T, home, name string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	root := filepath.Join(home, "plugins", name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest, err := json.Marshal(map[string]any{
		"apiVersion": pluginpkg.ManifestAPIVersionV2,
		"name":       name,
		"version":    "1.0.0",
		"runtime": map[string]any{
			"command":      exe,
			"args":         []string{"-test.run=^TestACPFakeSidecarHelperProcess$"},
			"capabilities": []string{"providers"},
			"env": map[string]any{
				acpFakeEnvEnable:     "1",
				acpFakeEnvPluginName: name,
			},
		},
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

func writeACPFixture(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
default_model = "local/fake-model"

[environment]
enabled = false

[[providers]]
name = "local"
kind = "acp-test-provider"
base_url = "http://example.invalid"
model = "fake-model"
api_key_env = "REASONIX_TEST_KEY"
`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runACPTurnAssistant(t *testing.T, ctrl interface {
	RunTurn(context.Context, string) error
	History() []provider.Message
}, input string,
) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ctrl.RunTurn(ctx, input); err != nil {
		t.Fatalf("RunTurn(%q): %v", input, err)
	}
	var sb strings.Builder
	for _, m := range ctrl.History() {
		if m.Role == provider.RoleAssistant {
			sb.WriteString(m.Content)
		}
	}
	return sb.String()
}

// TestACPSessionWithPluginModelStreamsAndSwitches: a plugin/... model in
// session/new resolves through boot's preflighted sidecar (RequireKey stays
// on for ACP and is correctly skipped for plugin refs), the turn streams the
// extension provider's completion, and RebuildSession moves to the config
// model and back to the plugin ref.
func TestACPSessionWithPluginModelStreamsAndSwitches(t *testing.T) {
	isolateCLIConfigHome(t)
	if _, err := config.SetCredential("REASONIX_TEST_KEY", "test-key"); err != nil {
		t.Fatalf("SetCredential: %v", err)
	}
	project := t.TempDir()
	writeACPFixture(t, project)
	name := "acpdemo"
	ref := "plugin/" + name + "/fake/x"
	installACPFakeProviderPlugin(t, config.ReasonixHomeDir(), name)

	factory := &acpFactory{}

	// sessionBootOptions carries the plugin ref verbatim into boot.
	opts, err := factory.sessionBootOptions(acp.SessionParams{Cwd: project, Model: ref, Sink: event.Discard})
	if err != nil {
		t.Fatalf("sessionBootOptions: %v", err)
	}
	if opts.Model != ref {
		t.Fatalf("sessionBootOptions Model = %q, want %q", opts.Model, ref)
	}

	// session/new with the plugin model.
	ctrl, err := factory.NewSession(context.Background(), acp.SessionParams{Cwd: project, Model: ref, Sink: event.Discard})
	if err != nil {
		t.Fatalf("NewSession with plugin model: %v", err)
	}
	if got := ctrl.ModelRef(); got != ref {
		t.Fatalf("session model ref = %q, want %q", got, ref)
	}
	if assistant := runACPTurnAssistant(t, ctrl, "say hi"); !strings.Contains(assistant, "acp-fake-hello acp-fake-world") {
		t.Fatalf("assistant = %q, want the extension provider's fixed completion", assistant)
	}

	// Mid-session switch to the config model, then back to the plugin ref.
	switched, err := factory.RebuildSession(context.Background(), acp.SessionParams{Cwd: project, Model: "local/fake-model", Sink: event.Discard}, ctrl)
	if err != nil {
		ctrl.Close()
		t.Fatalf("RebuildSession to config model: %v", err)
	}
	if got := switched.ModelRef(); got != "local/fake-model" {
		t.Fatalf("switched model ref = %q, want local/fake-model", got)
	}
	back, err := factory.RebuildSession(context.Background(), acp.SessionParams{Cwd: project, Model: ref, Sink: event.Discard}, switched)
	if err != nil {
		switched.Close()
		t.Fatalf("RebuildSession back to plugin model: %v", err)
	}
	defer back.Close()
	if got := back.ModelRef(); got != ref {
		t.Fatalf("back-switched model ref = %q, want %q", got, ref)
	}
	if assistant := runACPTurnAssistant(t, back, "say hi again"); !strings.Contains(assistant, "acp-fake-hello acp-fake-world") {
		t.Fatalf("switched-back assistant = %q, want the extension provider's fixed completion", assistant)
	}

	// The config-state surface tolerates the plugin ref too (no unknown-model
	// error), reporting it as the current model.
	state, err := factory.SessionConfigState(context.Background(), acp.SessionConfigStateParams{Cwd: project, Model: ref})
	if err != nil {
		t.Fatalf("SessionConfigState with plugin model: %v", err)
	}
	if state.Model != ref || state.Models == nil || state.Models.CurrentModelID != ref {
		t.Fatalf("SessionConfigState model = %q / %+v, want %q", state.Model, state.Models, ref)
	}

	switched.Close()
	ctrl.Close()
}
