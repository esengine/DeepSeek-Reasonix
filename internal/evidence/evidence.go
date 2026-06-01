package evidence

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Receipt is the host-runtime record of one tool call. It stays in memory for
// the current agent turn and is not serialized into prompts or session state.
type Receipt struct {
	ToolName string          `json:"tool_name"`
	Args     json.RawMessage `json:"args,omitempty"`
	Success  bool            `json:"success"`
	Command  string          `json:"command,omitempty"`
	Paths    []string        `json:"paths,omitempty"`
	Read     bool            `json:"read,omitempty"`
	Write    bool            `json:"write,omitempty"`
}

// Ledger stores the receipts available to complete_step for the current turn.
type Ledger struct {
	mu       sync.Mutex
	receipts []Receipt
}

func NewLedger() *Ledger { return &Ledger{} }

// Reset clears receipts between user turns.
func (l *Ledger) Reset() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.receipts = nil
}

// Record appends a receipt. Failed receipts are retained for auditability but
// are never accepted by the HasSuccessful* matchers.
func (l *Ledger) Record(r Receipt) {
	if l == nil {
		return
	}
	r.Command = strings.TrimSpace(r.Command)
	r.Paths = normalizePaths(r.Paths)
	if r.Args != nil {
		cp := make(json.RawMessage, len(r.Args))
		copy(cp, r.Args)
		r.Args = cp
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.receipts = append(l.receipts, r)
}

func (l *Ledger) HasSuccessfulCommand(command string) bool {
	command = strings.TrimSpace(command)
	if l == nil || command == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts {
		if r.Success && r.ToolName == "bash" && r.Command == command {
			return true
		}
	}
	return false
}

func (l *Ledger) HasSuccessfulWrite(paths []string) bool {
	return l.hasSuccessfulPaths(paths, func(r Receipt) bool { return r.Write })
}

func (l *Ledger) HasSuccessfulReadOrWrite(paths []string) bool {
	return l.hasSuccessfulPaths(paths, func(r Receipt) bool { return r.Read || r.Write })
}

func (l *Ledger) hasSuccessfulPaths(paths []string, accept func(Receipt) bool) bool {
	wanted := pathSet(normalizePaths(paths))
	if l == nil || len(wanted) == 0 {
		return false
	}
	found := map[string]bool{}

	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts {
		if !r.Success || !accept(r) {
			continue
		}
		for _, p := range r.Paths {
			if _, ok := wanted[p]; ok {
				found[p] = true
			}
		}
	}
	return len(found) == len(wanted)
}

type contextKey struct{}

func WithLedger(ctx context.Context, ledger *Ledger) context.Context {
	if ledger == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, ledger)
}

func FromContext(ctx context.Context) (*Ledger, bool) {
	ledger, ok := ctx.Value(contextKey{}).(*Ledger)
	return ledger, ok && ledger != nil
}

func ReceiptFromToolCall(toolName string, args json.RawMessage, success bool, readOnly bool) Receipt {
	r := Receipt{
		ToolName: toolName,
		Args:     args,
		Success:  success,
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err == nil {
		if toolName == "bash" {
			r.Command = stringField(fields, "command")
		}
		r.Paths = extractPaths(fields)
	}

	if isWriterTool(toolName) {
		r.Write = true
	} else if isReaderTool(toolName) || (readOnly && len(r.Paths) > 0) {
		r.Read = true
	}
	return r
}

func isWriterTool(name string) bool {
	switch name {
	case "write_file", "edit_file", "multi_edit", "notebook_edit", "delete_range", "delete_symbol":
		return true
	default:
		return false
	}
}

func isReaderTool(name string) bool {
	switch name {
	case "read_file", "ls", "grep":
		return true
	default:
		return false
	}
}

func extractPaths(fields map[string]json.RawMessage) []string {
	var paths []string
	for _, key := range []string{"path", "file_path", "notebook_path"} {
		if s := stringField(fields, key); s != "" {
			paths = append(paths, s)
		}
	}
	for _, key := range []string{"paths", "file_paths"} {
		paths = append(paths, stringSliceField(fields, key)...)
	}
	return paths
}

func stringField(fields map[string]json.RawMessage, key string) string {
	raw, ok := fields[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func stringSliceField(fields map[string]json.RawMessage, key string) []string {
	raw, ok := fields[key]
	if !ok {
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	return values
}

func pathSet(paths []string) map[string]bool {
	out := map[string]bool{}
	for _, p := range paths {
		if p != "" {
			out[p] = true
		}
	}
	return out
}

func normalizePaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = normalizePath(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, `\`, `/`)
	p = filepath.Clean(filepath.FromSlash(p))
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}
