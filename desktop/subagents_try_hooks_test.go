package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/hook"
	"reasonix/internal/provider"
)

const tryHookProbeKind = "desktop-try-hook-probe"

var tryHookProbe = &tryHookProbeProvider{}

func init() {
	provider.Register(tryHookProbeKind, func(provider.Config) (provider.Provider, error) {
		return tryHookProbe, nil
	})
}

// The try run reads the user's own open workspace, so it runs the hooks a chat
// session there would: the project's and the user's.
func TestTrySubagentProfileRunsWorkspaceAndUserHooks(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "marker.txt"), []byte("try hook probe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(`
default_model = "tryer"

[[providers]]
name = "tryer"
kind = "`+tryHookProbeKind+`"
model = "try-model"
base_url = "http://127.0.0.1:1"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	approveWorkspace(t, root)
	scripts := t.TempDir()
	projectLog := filepath.Join(scripts, "project.log")
	globalLog := filepath.Join(scripts, "global.log")
	writeTryHookSettings(t, hook.ProjectSettingsPath(root), writeTryHookScript(t, scripts, "project-log.sh", projectLog, 0))
	writeTryHookSettings(t, hook.GlobalSettingsPath(""), writeTryHookScript(t, scripts, "global-deny.sh", globalLog, 2))
	if err := hook.ApproveProjectHooks(hook.LoadOptions{ProjectRoot: root}); err != nil {
		t.Fatal(err)
	}

	a := NewApp()
	a.tabs = map[string]*WorkspaceTab{"test": {ID: "test", Scope: "project", WorkspaceRoot: root, Ready: true}}
	a.activeTabID = "test"
	tryHookProbe.reset()
	if _, err := a.TrySubagentProfile(SubagentProfileInput{SystemPrompt: "read marker.txt"}, "read marker.txt"); err != nil {
		t.Fatalf("TrySubagentProfile: %v", err)
	}
	if tryHookProbe.readLeaked() {
		t.Fatal("try run received marker.txt contents past a PreToolUse deny")
	}
	if !tryHookProbe.readBlocked() {
		t.Fatal("try run's read_file result was not blocked by PreToolUse")
	}
	project := readTryHookPayload(t, projectLog, "project")
	global := readTryHookPayload(t, globalLog, "global")
	for _, p := range []tryHookPayload{project, global} {
		if p.ToolName != "read_file" || !strings.HasPrefix(p.SessionID, "try-subagent:") || p.SessionID == "try-subagent:" {
			t.Fatalf("hook payload = %+v, want read_file under a per-run try-subagent session", p)
		}
	}
}

type tryHookPayload struct{ ToolName, SessionID string }

func writeTryHookScript(t *testing.T, dir, name, logPath string, exit int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	body := fmt.Sprintf("#!/bin/sh\ncat >> '%s'\nexit %d\n", strings.ReplaceAll(logPath, "'", `'\''`), exit)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeTryHookSettings(t *testing.T, path, command string) {
	t.Helper()
	settings, err := json.Marshal(map[string]any{"hooks": map[string]any{"PreToolUse": []any{map[string]string{"match": "read_file", "command": command}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, settings, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTryHookPayload(t *testing.T, path, scope string) tryHookPayload {
	t.Helper()
	log, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s PreToolUse hook never ran for the try run's read_file: %v", scope, err)
	}
	var p tryHookPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(log))), &p); err != nil {
		t.Fatalf("decode %s hook payload %q: %v", scope, log, err)
	}
	return p
}

type tryHookProbeProvider struct {
	mu      sync.Mutex
	calls   int
	blocked bool
	leaked  bool
}

func (p *tryHookProbeProvider) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls, p.blocked, p.leaked = 0, false, false
}

func (p *tryHookProbeProvider) readBlocked() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.blocked
}

func (p *tryHookProbeProvider) readLeaked() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.leaked
}

func (p *tryHookProbeProvider) Name() string { return tryHookProbeKind }

func (p *tryHookProbeProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	call := p.calls
	p.calls++
	for _, msg := range req.Messages {
		if msg.Role != provider.RoleTool || msg.Name != "read_file" {
			continue
		}
		if strings.Contains(msg.Content, "blocked:") {
			p.blocked = true
		}
		if strings.Contains(msg.Content, "try hook probe") {
			p.leaked = true
		}
	}
	p.mu.Unlock()

	chunks := []provider.Chunk{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}}
	if call == 0 {
		chunks = []provider.Chunk{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "try-read", Name: "read_file", Arguments: `{"path":"marker.txt"}`}}}
	}
	ch := make(chan provider.Chunk, len(chunks))
	for _, chunk := range chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}
