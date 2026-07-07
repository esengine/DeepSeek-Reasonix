package agent

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestRepeatGuardBlocksRepeatedSuccessfulBashFileWrite(t *testing.T) {
	var calls int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false, calls: &calls})
	args := `{"command":"python -c \"with open('prompt.txt', 'w') as f: f.write('hello')\""}`
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("c1", "bash", args), {Type: provider.ChunkDone}},
		{toolCallChunk("c2", "bash", args), {Type: provider.ChunkDone}},
		{toolCallChunk("c3", "bash", args), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "update the prompt file"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("bash executed %d times, want 2 before the repeat guard blocks", got)
	}
	results := toolResults(a.session, "bash")
	if len(results) != 3 {
		t.Fatalf("tool results = %d, want 3", len(results))
	}
	last := results[len(results)-1]
	if !strings.Contains(last, "[loop guard]") || !strings.Contains(last, "edit_file") {
		t.Fatalf("third repeated write should nudge the model to change tools, got %q", last)
	}
}

func TestRepeatGuardAllowsRepeatedNonWritingBashCommand(t *testing.T) {
	var calls int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false, calls: &calls})
	args := `{"command":"go test ./internal/agent"}`
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("c1", "bash", args), {Type: provider.ChunkDone}},
		{toolCallChunk("c2", "bash", args), {Type: provider.ChunkDone}},
		{toolCallChunk("c3", "bash", args), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "verify repeatedly"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("bash executed %d times, want 3 for non-writing commands", got)
	}
	if last := lastToolResult(a.session, "bash"); strings.Contains(last, "[loop guard]") {
		t.Fatalf("non-writing bash should not trip the repeat guard, got %q", last)
	}
}

func TestRepeatGuardBashWriteRedirectDetectionUsesAST(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		{
			name: "stdout file redirect",
			cmd:  "printf hi > prompt.txt",
			want: true,
		},
		{
			name: "stderr file redirect",
			cmd:  "printf hi 2>err.log",
			want: true,
		},
		{
			name: "append redirect",
			cmd:  "printf hi >> prompt.txt",
			want: true,
		},
		{
			name: "combined redirect",
			cmd:  "printf hi &> prompt.txt",
			want: true,
		},
		{
			name: "read write redirect",
			cmd:  "cat <> prompt.txt",
			want: true,
		},
		{
			name: "null sink redirect",
			cmd:  "printf hi >/dev/null",
			want: false,
		},
		{
			name: "powershell null sink spelling",
			cmd:  "printf hi >$null",
			want: false,
		},
		{
			name: "windows nul sink spelling",
			cmd:  "printf hi >NUL",
			want: false,
		},
		{
			name: "fd duplication",
			cmd:  "printf hi 2>&1",
			want: false,
		},
		{
			name: "quoted redirect text",
			cmd:  `printf '%s\n' 'a > b'`,
			want: false,
		},
		{
			name: "heredoc body redirect text",
			cmd:  "cat <<'EOF'\n> prompt.txt\nEOF",
			want: false,
		},
		{
			name: "heredoc with file redirect",
			cmd:  "cat <<'EOF' > prompt.txt\nbody\nEOF",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isShellFileWriteCommand(tt.cmd); got != tt.want {
				t.Fatalf("isShellFileWriteCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestRepeatGuardNormalizesStaticBashFields(t *testing.T) {
	singleQuoted := normalizeShellCommand(`printf '%s\n' 'hello world'`)
	doubleQuoted := normalizeShellCommand(`printf "%s\n" "hello world"`)
	if singleQuoted != doubleQuoted {
		t.Fatalf("normalized quote styles differ:\n single: %q\n double: %q", singleQuoted, doubleQuoted)
	}
}

func TestRepeatGuardAllowsTwoRepeatedWriterSuccesses(t *testing.T) {
	var calls int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false, calls: &calls})
	args := `{"path":"prompt.txt","content":"hello"}`
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("c1", "write_file", args), {Type: provider.ChunkDone}},
		{toolCallChunk("c2", "write_file", args), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "write twice"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("writer executed %d times, want 2 before the guard threshold", got)
	}
	if last := lastToolResult(a.session, "write_file"); strings.Contains(last, "[loop guard]") {
		t.Fatalf("second repeated writer call should still be allowed, got %q", last)
	}
}
