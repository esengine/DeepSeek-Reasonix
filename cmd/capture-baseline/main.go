// Command capture-baseline dumps the full system prompt + registered tool schemas
// (as the LLM would see them) to stdout as JSON.
//
// Usage:
//
//	cd <repo-root> && go run ./cmd/capture-baseline/ > baseline.json
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/boot"

	// Blank imports wire compile-time built-ins into their registries.
	_ "reasonix/internal/provider/openai"
	_ "reasonix/internal/tool/builtin"
)

// toolSchema is a serializable tool contract entry.
type toolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type capture struct {
	SystemPrompt string       `json:"system_prompt"`
	PromptLength int          `json:"prompt_length"`
	NumTools     int          `json:"num_tools"`
	ToolSchemas  []toolSchema `json:"tool_schemas"`
}

func main() {
	// Build a temporary reasonix controller in an isolated directory.
	dir, err := os.MkdirTemp("", "capture-baseline-*")
	if err != nil {
		fatal("create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	tomlPath := filepath.Join(dir, "reasonix.toml")
	if err := os.WriteFile(tomlPath, []byte(minimalConfig), 0644); err != nil {
		fatal("write config: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		fatal("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Build the controller to get the real system prompt (with env probes,
	// skills index, memory folding, etc.) and live tool registry.
	ctrl, err := boot.Build(context.Background(), boot.Options{})
	if err != nil {
		fatal("Build: %v", err)
	}
	defer ctrl.Close()

	// Extract system prompt from the first system message.
	msgs := ctrl.History()
	var sysPrompt string
	for _, m := range msgs {
		if m.Role == "system" {
			sysPrompt = m.Content
			break
		}
	}
	if sysPrompt == "" {
		fatal("no system message found in controller history")
	}

	// Tool schemas from the controller's live registry (matches what the
	// agent sends to the LLM).
	entries := ctrl.ToolContractEntries()
	schemas := make([]toolSchema, len(entries))
	for i, e := range entries {
		schemas[i] = toolSchema{
			Name:        e.Name,
			Description: e.Description,
			Parameters:  e.Schema,
		}
	}

	out := capture{
		SystemPrompt: sysPrompt,
		PromptLength: len(sysPrompt),
		NumTools:     len(schemas),
		ToolSchemas:  schemas,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fatal("encode: %v", err)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "capture-baseline: "+format+"\n", args...)
	os.Exit(1)
}

// Minimal config avoids API key validation (RequireKey=false).
const minimalConfig = `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`
