// Package builtin provides Reasonix's compile-time built-in tools.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(jsonFormatTool{}) }

// jsonFormatTool pretty-prints or minifies JSON. It reads from a file
// (input_file) or stdin and writes the formatted JSON to stdout. It is
// read-only: it never writes to disk.
type jsonFormatTool struct{}

func (jsonFormatTool) Name() string { return "json_format" }

func (jsonFormatTool) Description() string {
	return "Pretty-print or minify JSON. Reads from input_file (or stdin if omitted) and returns formatted JSON. Use indent to control spacing (0 compacts to one line, default 2). Use minify to collapse to a single line."
}

func (jsonFormatTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "input_file":{"type":"string","description":"Path to a JSON file to read. Omit to read from stdin."},
  "indent":{"type":"integer","description":"Spaces per indent level (0-8, default 2). Ignored when minify is true.","minimum":0,"maximum":8},
  "minify":{"type":"boolean","description":"Collapse the JSON into a single line (default false)."}
}
}`)
}

func (jsonFormatTool) ReadOnly() bool { return true }

// SnipHint keeps a generous head and tail so JSON is readable when truncated.
func (jsonFormatTool) SnipHint() tool.SnipHint {
	return tool.SnipHint{Head: 200, Tail: 40, HeadChars: 20000, TailChars: 4000}
}

func (jsonFormatTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		InputFile string `json:"input_file"`
		Indent    int    `json:"indent"`
		Minify    bool   `json:"minify"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Indent < 0 {
		p.Indent = 2
	}
	if p.Indent > 8 {
		p.Indent = 8
	}

	var raw []byte
	if p.InputFile != "" {
		b, err := os.ReadFile(p.InputFile)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", p.InputFile, err)
		}
		raw = b
	} else {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		raw = b
	}

	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}

	if p.Minify {
		out, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("marshal: %w", err)
		}
		return string(out), nil
	}

	indent := strings.Repeat(" ", p.Indent)
	out, err := json.MarshalIndent(v, "", indent)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	return string(out), nil
}
