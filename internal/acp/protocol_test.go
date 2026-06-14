package acp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestFlattenPrompt(t *testing.T) {
	tests := []struct {
		name   string
		blocks []ContentBlock
		want   string
	}{
		{
			name:   "text blocks join with blank line",
			blocks: []ContentBlock{{Type: "text", Text: "hello"}, {Type: "text", Text: "world"}},
			want:   "hello\n\nworld",
		},
		{
			name: "resource contributes inline text",
			blocks: []ContentBlock{
				{Type: "text", Text: "see file:"},
				{Type: "resource", Resource: &ResourceContents{URI: "file:///x", Text: "contents"}},
			},
			want: "see file:\n\ncontents",
		},
		{
			name: "resource without inline text is dropped",
			blocks: []ContentBlock{
				{Type: "resource", Resource: &ResourceContents{URI: "file:///x"}},
				{Type: "text", Text: "only this"},
			},
			want: "only this",
		},
		{
			name: "image and audio blocks are ignored",
			blocks: []ContentBlock{
				{Type: "image", MimeType: "image/png", Data: "base64"},
				{Type: "text", Text: "kept"},
				{Type: "audio", MimeType: "audio/wav", Data: "base64"},
			},
			want: "kept",
		},
		{
			name:   "surrounding whitespace trimmed",
			blocks: []ContentBlock{{Type: "text", Text: "  spaced  "}},
			want:   "spaced",
		},
		{
			name:   "empty input",
			blocks: nil,
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FlattenPrompt(tt.blocks); got != tt.want {
				t.Errorf("FlattenPrompt() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToolKindFor(t *testing.T) {
	tests := map[string]string{
		"read_file":             "read",
		"ls":                    "read",
		"glob":                  "read",
		"grep":                  "search",
		"edit_file":             "edit",
		"multiedit":             "edit",
		"write_file":            "edit",
		"bash":                  "execute",
		"webfetch":              "other",
		"task":                  "other",
		"mcp__server__do_thing": "other",
		"semantic_search":       "search", // heuristic fallback
		"run_command":           "execute",
		"unknown":               "other",
	}
	for name, want := range tests {
		if got := toolKindFor(name); got != want {
			t.Errorf("toolKindFor(%q) = %q, want %q", name, got, want)
		}
	}
}

// --- newSessionID ---

func TestNewSessionID(t *testing.T) {
	id, err := newSessionID()
	if err != nil {
		t.Fatalf("newSessionID: %v", err)
	}
	parts := strings.Split(id, "-")
	if len(parts) != 5 {
		t.Fatalf("UUID format: %q has %d parts, want 5", id, len(parts))
	}
	if len(parts[0]) != 8 || len(parts[1]) != 4 || len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		t.Errorf("UUID part lengths: %v", parts)
	}
	// Version 4: bits 4-7 of byte 6 are 0100.
	if id[14] != '4' {
		t.Errorf("UUID version: char at 14 = %c, want '4'", id[14])
	}
	// Variant: bits 6-7 of byte 8 are 10.
	variant := id[19]
	if variant != '8' && variant != '9' && variant != 'a' && variant != 'b' {
		t.Errorf("UUID variant: char at 19 = %c, want 8/9/a/b", variant)
	}
}

func TestNewSessionIDUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := newSessionID()
		if err != nil {
			t.Fatalf("newSessionID: %v", err)
		}
		if seen[id] {
			t.Fatalf("duplicate session id: %s", id)
		}
		seen[id] = true
	}
}

// --- mcpSpecs ---

func TestMcpSpecsNil(t *testing.T) {
	if got, err := mcpSpecs(nil, ""); err != nil || got != nil {
		t.Errorf("mcpSpecs(nil) = %v, want nil", got)
	}
	if got, err := mcpSpecs([]MCPServerSpec{}, ""); err != nil || got != nil {
		t.Errorf("mcpSpecs([]) = %v, want nil", got)
	}
}

func TestMcpSpecsConversion(t *testing.T) {
	in := []MCPServerSpec{
		{Name: "codegraph", Command: "codegraph", Args: []string{"--stdio"}, Env: MCPEnv{"HOME": "/tmp"}},
	}
	got, err := mcpSpecs(in, "/workspace")
	if err != nil {
		t.Fatalf("mcpSpecs: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Name != "codegraph" || got[0].Type != "stdio" || got[0].Command != "codegraph" {
		t.Errorf("spec = %+v", got[0])
	}
	if got[0].Args[0] != "--stdio" {
		t.Errorf("args = %v", got[0].Args)
	}
	if got[0].Env["HOME"] != "/tmp" {
		t.Errorf("env = %v", got[0].Env)
	}
	if got[0].Dir != "/workspace" {
		t.Errorf("dir = %q, want /workspace", got[0].Dir)
	}
}

func TestMCPEnvAcceptsOfficialArrayShape(t *testing.T) {
	var p SessionNewParams
	raw := []byte(`{
		"cwd":"/tmp",
		"mcpServers":[{
			"name":"fs",
			"command":"mcp-fs",
			"args":["--stdio"],
			"env":[{"name":"HOME","value":"/tmp"},{"name":"EMPTY","value":""}]
		}]
	}`)
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal official env array: %v", err)
	}
	got, err := mcpSpecs(p.MCPServers, p.Cwd)
	if err != nil {
		t.Fatalf("mcpSpecs: %v", err)
	}
	if got[0].Env["HOME"] != "/tmp" || got[0].Env["EMPTY"] != "" {
		t.Fatalf("env = %v, want HOME and EMPTY from official array", got[0].Env)
	}
}

func TestMcpSpecsRejectsUnsupportedTransport(t *testing.T) {
	_, err := mcpSpecs([]MCPServerSpec{{Name: "remote", Type: "http", Command: "ignored"}}, "/tmp")
	if err == nil || !strings.Contains(err.Error(), "unsupported transport") {
		t.Fatalf("mcpSpecs unsupported transport err = %v", err)
	}
}

// --- transcriptPath ---

func TestTranscriptPath(t *testing.T) {
	dir := t.TempDir()
	got := transcriptPath(dir, "abc-123")
	if want := filepath.Join(dir, "abc-123.jsonl"); got != want {
		t.Errorf("transcriptPath = %q, want %q", got, want)
	}
}

// --- Protocol constants ---

func TestProtocolVersion(t *testing.T) {
	if ProtocolVersion != 1 {
		t.Errorf("ProtocolVersion = %d", ProtocolVersion)
	}
}

func TestErrorCodes(t *testing.T) {
	if ErrParse != -32700 {
		t.Errorf("ErrParse = %d", ErrParse)
	}
	if ErrInvalidRequest != -32600 {
		t.Errorf("ErrInvalidRequest = %d", ErrInvalidRequest)
	}
	if ErrMethodNotFound != -32601 {
		t.Errorf("ErrMethodNotFound = %d", ErrMethodNotFound)
	}
	if ErrInvalidParams != -32602 {
		t.Errorf("ErrInvalidParams = %d", ErrInvalidParams)
	}
	if ErrInternal != -32603 {
		t.Errorf("ErrInternal = %d", ErrInternal)
	}
}

// --- acpSession ---

func TestAcpSessionSetCancelAbort(t *testing.T) {
	sess := &acpSession{id: "test"}
	aborted := false
	_, cancel, ok := sess.begin(context.Background())
	if !ok {
		t.Fatal("begin should succeed")
	}
	sess.mu.Lock()
	sess.cancel = func() {
		aborted = true
		cancel()
	}
	sess.mu.Unlock()
	sess.abort()
	if !aborted {
		t.Error("abort should call the cancel func")
	}
}

func TestAcpSessionAbortNil(t *testing.T) {
	sess := &acpSession{id: "test"}
	sess.abort() // should not panic
}

func TestAcpSessionSetCancelNil(t *testing.T) {
	sess := &acpSession{id: "test"}
	sess.finish()
	sess.abort() // should not panic
}
