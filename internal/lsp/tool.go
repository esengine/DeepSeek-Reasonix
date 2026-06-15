package lsp

import (
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/tool"
)

// Tools adapts the manager's read-only queries to the tool.Tool interface. They
// report ReadOnly=true, so a batch of them rides the agent's parallel dispatch
// and shares one server per language.
func Tools(m *Manager) []tool.Tool {
	return []tool.Tool{
		posTool{m, "lsp_definition", "Jump to where a symbol is defined. Give the file, the 1-based line the symbol appears on, and the symbol text itself.", m.Definition},
		posTool{m, "lsp_references", "List every reference to a symbol across the workspace. Give the file, the 1-based line, and the symbol text.", m.References},
		posTool{m, "lsp_hover", "Show the type signature and documentation for a symbol. Give the file, the 1-based line, and the symbol text.", m.Hover},
		diagTool{m},
	}
}

type posArgs struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Symbol string `json:"symbol"`
}

type posTool struct {
	m    *Manager
	name string
	desc string
	fn   func(context.Context, string, int, string) (string, error)
}

func (t posTool) Name() string        { return t.name }
func (t posTool) Description() string { return t.desc }
func (t posTool) ReadOnly() bool      { return true }
func (t posTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "file":{"type":"string","description":"Path to the source file, relative to the workspace root or absolute."},
  "line":{"type":"integer","description":"1-based line number the symbol appears on."},
  "symbol":{"type":"string","description":"The exact symbol text on that line, e.g. \"executeBatch\". Used to locate the column."}
},
"required":["file","line","symbol"]
}`)
}

func (t posTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p posArgs
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.File == "" || p.Symbol == "" || p.Line < 1 {
		return "", fmt.Errorf("file, line (>=1) and symbol are required")
	}
	return t.fn(ctx, p.File, p.Line, p.Symbol)
}

type diagTool struct{ m *Manager }

func (diagTool) Name() string   { return "lsp_diagnostics" }
func (diagTool) ReadOnly() bool { return true }
func (diagTool) Description() string {
	return "Report compiler/linter diagnostics (errors, warnings) for a file from its language server. Use after editing to check the change compiles."
}
func (diagTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string","description":"Path to the source file, relative to the workspace root or absolute."}},"required":["file"]}`)
}

func (t diagTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		File string `json:"file"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.File == "" {
		return "", fmt.Errorf("file is required")
	}
	return t.m.Diagnostics(ctx, p.File)
}
