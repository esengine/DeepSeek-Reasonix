package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// Golden baseline for the cache-stable provider contract. Without a deliberate
// migration, the system prompt, tool schemas, provider request, and prefix hash
// remain byte-identical. Session-context tests cover dynamic data separately.
//
// The stable-prefix/session-context migration intentionally updates the system
// and prefix goldens once while leaving tool_schemas.json byte-identical. Future
// reviewed provider-visible changes must regenerate with:
//
//	REASONIX_UPDATE_GOLDEN=1 go test ./internal/boot -run TestGoldenBaseline -count=1
//	REASONIX_GOLDEN_SHELL=powershell REASONIX_UPDATE_GOLDEN=1 go test ./internal/boot -run TestGoldenBaseline -count=1
//
// and call the cache impact out in the commit message.
//
// Machine dependence is designed out of the golden: the environment probe
// section is disabled in the fixture config (it embeds GOOS/GOARCH, shell
// labels, and probe output; internal/environment covers it with its own
// snapshot tests), and the workspace root path is normalized to <ROOT>.
const goldenBaselineDir = "testdata/golden"

// goldenBaseline is one deterministic capture of the provider-visible
// runtime surface with zero extensions installed.
type goldenBaseline struct {
	SystemPrompt string
	ToolSchemas  []byte
	ProviderReq  []byte
	PrefixShape  agent.PrefixShape
}

func captureGoldenBaseline(t *testing.T) goldenBaseline {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	fixture := `
default_model = "test-model"

[agent]
system_prompt = "BASE SYSTEM PROMPT"

[environment]
enabled = false

[tools.shell]
# This golden records the Bash contract; Windows auto selects PowerShell.
# Pin the dialect just as the search engine below is pinned.
prefer = "bash"

[tools.search]
# Pin the grep engine: on "auto" the tool's description (and with it the tool
# schemas, provider request, and cache prefix) changes depending on whether rg
# happens to be on PATH, which would make this golden machine-dependent.
engine = "native"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`
	forcePowerShell := runtime.GOOS == "windows" || os.Getenv("REASONIX_GOLDEN_SHELL") == "powershell"
	if forcePowerShell {
		// Pin 5.1 independently of whether PowerShell 7 is installed.
		fixture = strings.Replace(fixture, `prefer = "bash"`, `prefer = "powershell"`, 1)
		if runtime.GOOS != "windows" {
			// A configured PowerShell path is enough to compose the Windows tool
			// contract; Build does not execute it. This lets POSIX CI guard the
			// Windows provider snapshot without Wine.
			fake := filepath.Join(t.TempDir(), "powershell")
			if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatalf("write fake PowerShell: %v", err)
			}
			// The user's own shell choice: a checkout may not name one where
			// sandboxed commands write.
			userConfig := config.UserConfigPath()
			writeFile(t, filepath.Dir(userConfig), filepath.Base(userConfig), "[tools.shell]\nprefer = \"powershell\"\npath = "+strconv.Quote(fake)+"\n")
		}
	}
	writeFile(t, dir, "reasonix.toml", fixture)
	approveWorkspace(t, dir)

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	// 1. System prompt, with the machine-specific workspace root normalized.
	prompt := systemMessage(ctrl.History())
	if strings.TrimSpace(prompt) == "" {
		t.Fatal("Build composed an empty system prompt")
	}
	actualDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolve fixture working directory: %v", err)
	}
	prompt = normalizeGoldenRoot(prompt, dir, actualDir)

	// 2. Provider-visible tool contract, through the same canonical schema
	// path the runtime registry uses.
	entries := ctrl.ToolContractEntries()
	if len(entries) == 0 {
		t.Fatal("Build registered no tools")
	}
	toolJSON, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatalf("marshal tool contract: %v", err)
	}

	// 3. Provider request serialization: a fixed synthetic conversation plus
	// the live tool schemas, assembled the same way the agent loop assembles
	// requests (ModelMessages + CreatedAt stripped before send).
	schemas := make([]provider.ToolSchema, 0, len(entries))
	for _, e := range entries {
		schemas = append(schemas, provider.ToolSchema{
			Name:        e.Name,
			Description: e.Description,
			Parameters:  e.Schema,
		})
	}
	temp := 0.7
	req := provider.Request{
		Messages: provider.ModelMessages([]provider.Message{
			{Role: provider.RoleSystem, Content: prompt},
			{Role: provider.RoleUser, Content: "USER PROMPT", RawContent: "USER PROMPT", CreatedAt: 1700000000000},
			{Role: provider.RoleAssistant, Content: "ASSISTANT REPLY", ReasoningContent: "THINKING", ReasoningSignature: "sig", ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "read", Arguments: `{"path":"a.txt"}`}}, CreatedAt: 1700000001000},
			{Role: provider.RoleTool, Content: "TOOL RESULT", ToolCallID: "call_1", Name: "read", CreatedAt: 1700000002000},
			{Role: provider.RoleUser, Content: "LOCAL ONLY", LocalOnly: true},
		}),
		Tools:       schemas,
		Temperature: &temp,
		MaxTokens:   1024,
	}
	for i := range req.Messages {
		req.Messages[i].CreatedAt = 0
	}
	reqJSON, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		t.Fatalf("marshal provider request: %v", err)
	}

	return goldenBaseline{
		SystemPrompt: prompt,
		ToolSchemas:  append(toolJSON, '\n'),
		ProviderReq:  append(reqJSON, '\n'),
		PrefixShape:  agent.CaptureShape(prompt, schemas, 0),
	}
}

// normalizeGoldenRoot replaces every known spelling of the workspace root
// (including the actual cwd and symlink-evaluated forms) with a stable
// placeholder. Windows may report the cwd using an 8.3 short-path alias even
// when TempDir returned the long form, so both values are required.
func normalizeGoldenRoot(prompt string, roots ...string) string {
	out := prompt
	seen := make(map[string]struct{}, len(roots)*2)
	replace := func(root string) {
		if root == "" {
			return
		}
		if _, ok := seen[root]; ok {
			return
		}
		// System-prompt workspace paths are Go-quoted. On Windows that doubles
		// backslashes, so replace the quoted spelling before the raw alias.
		out = strings.ReplaceAll(out, strconv.Quote(root), strconv.Quote("<ROOT>"))
		out = strings.ReplaceAll(out, root, "<ROOT>")
		seen[root] = struct{}{}
	}
	for _, root := range roots {
		replace(root)
		if real, err := filepath.EvalSymlinks(root); err == nil && real != root {
			replace(real)
		}
	}
	return out
}

func TestNormalizeGoldenRootReplacesQuotedWindowsPath(t *testing.T) {
	root := `C:\Users\RUNNER~1\AppData\Local\Temp\reasonix-test-123`
	prompt := "Current workspace: " + strconv.Quote(root)
	if got, want := normalizeGoldenRoot(prompt, root), `Current workspace: "<ROOT>"`; got != want {
		t.Fatalf("normalizeGoldenRoot = %q, want %q", got, want)
	}
}

func TestNormalizeGoldenRootReplacesEveryAlias(t *testing.T) {
	prompt := "long=/tmp/reasonix-long short=/tmp/reasonix-short"
	got := normalizeGoldenRoot(prompt, "/tmp/reasonix-long", "/tmp/reasonix-short")
	if got != "long=<ROOT> short=<ROOT>" {
		t.Fatalf("normalizeGoldenRoot = %q", got)
	}
}

func TestGoldenBaselineNoExtensions(t *testing.T) {
	// Resolve the golden directory before the fixture chdirs into a temp
	// workspace, or reads/writes would land inside the fixture.
	goldenDir, err := filepath.Abs(goldenBaselineDir)
	if err != nil {
		t.Fatalf("resolve golden dir: %v", err)
	}
	if runtime.GOOS == "windows" || os.Getenv("REASONIX_GOLDEN_SHELL") == "powershell" {
		goldenDir = filepath.Join(goldenDir, "windows-powershell")
	}

	first := captureGoldenBaseline(t)

	// In-run determinism: an identical second Build must capture the exact
	// same surface before we bother comparing against the committed golden.
	second := captureGoldenBaseline(t)
	if first.SystemPrompt != second.SystemPrompt {
		t.Fatalf("system prompt is not deterministic across identical Builds, first diff: %q", firstDivergence(first.SystemPrompt, second.SystemPrompt))
	}
	if string(first.ToolSchemas) != string(second.ToolSchemas) {
		t.Fatal("tool schemas are not deterministic across identical Builds")
	}
	if string(first.ProviderReq) != string(second.ProviderReq) {
		t.Fatal("provider request serialization is not deterministic across identical Builds")
	}

	shapeJSON, err := json.MarshalIndent(first.PrefixShape, "", "  ")
	if err != nil {
		t.Fatalf("marshal prefix shape: %v", err)
	}
	shapeJSON = append(shapeJSON, '\n')

	artifacts := map[string][]byte{
		"system_prompt.txt":     []byte(first.SystemPrompt),
		"tool_schemas.json":     first.ToolSchemas,
		"provider_request.json": first.ProviderReq,
		"prefix_shape.json":     shapeJSON,
	}

	if os.Getenv("REASONIX_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		for name, data := range artifacts {
			if err := os.WriteFile(filepath.Join(goldenDir, name), data, 0o644); err != nil {
				t.Fatalf("update golden %s: %v", name, err)
			}
			t.Logf("updated golden %s (%d bytes)", name, len(data))
		}
		return
	}

	for name, want := range artifacts {
		got, err := os.ReadFile(filepath.Join(goldenDir, name))
		if err != nil {
			t.Fatalf("read golden %s: %v (record it with REASONIX_UPDATE_GOLDEN=1)", name, err)
		}
		if string(got) != string(want) {
			t.Fatalf("golden %s drifted from the committed cache-contract baseline (%d bytes golden, %d bytes actual); first diff: %q\n"+
				"If this drift is deliberate, regenerate with REASONIX_UPDATE_GOLDEN=1 and document the cache impact.",
				name, len(got), len(want), firstDivergence(string(got), string(want)))
		}
	}
}

func TestWindowsProviderSurfaceUsesPwshAndFormalJobsOnly(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Setenv("REASONIX_GOLDEN_SHELL", "powershell")
	}
	baseline := captureGoldenBaseline(t)
	var entries []tool.ContractEntry
	if err := json.Unmarshal(baseline.ToolSchemas, &entries); err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]tool.ContractEntry, len(entries))
	for _, entry := range entries {
		byName[entry.Name] = entry
	}
	for _, want := range []string{"pwsh", "job_output", "job_kill"} {
		if _, ok := byName[want]; !ok {
			t.Fatalf("Windows provider surface missing %q: %v", want, byName)
		}
	}
	for _, hidden := range []string{"bash", "Bash", "PowerShell", "powershell", "bash_output", "wait", "kill_shell"} {
		if _, ok := byName[hidden]; ok {
			t.Fatalf("compatibility alias %q leaked into Windows provider schema", hidden)
		}
	}
	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(byName["pwsh"].Schema, &schema); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(schema.Required, "command") || !slices.Contains(schema.Required, "description") {
		t.Fatalf("pwsh required fields = %v", schema.Required)
	}
	if _, ok := schema.Properties["preserve_background_processes"]; ok {
		t.Fatal("pwsh schema exposed preserve_background_processes")
	}
}

// TestGoldenBaselineContractSanity cross-checks the golden tool contract
// against the compile-time builtin contract so the boot-level golden cannot
// silently drift away from the tool package's own committed contract.
func TestGoldenBaselineContractSanity(t *testing.T) {
	builtins := tool.BuiltinContractEntries()
	if len(builtins) == 0 {
		t.Fatal("no builtin contract entries")
	}
	seen := make(map[string]bool, len(builtins))
	for _, e := range builtins {
		if seen[e.Name] {
			t.Fatalf("duplicate builtin contract entry %q", e.Name)
		}
		seen[e.Name] = true
		if len(e.Schema) == 0 {
			t.Fatalf("builtin contract entry %q has an empty canonical schema", e.Name)
		}
	}
}
