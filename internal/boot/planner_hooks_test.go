package boot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/hook"
	"reasonix/internal/provider"
)

const plannerHookProbeKind = "boot-planner-hook-probe"

var (
	plannerHookProbeOnce    sync.Once
	plannerHookProbeMu      sync.Mutex
	plannerHookProbeCurrent *plannerHookProbeProvider
)

type hookPayloadForTest struct {
	Event     string `json:"event"`
	ToolName  string `json:"toolName"`
	SessionID string `json:"sessionId"`
}

func TestBuildRunsPreToolUseInsidePlanner(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	plannerHookProbeOnce.Do(func() {
		provider.Register(plannerHookProbeKind, func(cfg provider.Config) (provider.Provider, error) {
			plannerHookProbeMu.Lock()
			defer plannerHookProbeMu.Unlock()
			if plannerHookProbeCurrent == nil {
				return nil, errors.New("planner hook probe provider is not installed")
			}
			if cfg.Model != "planner-model" {
				return &plannerHookProbeProvider{}, nil
			}
			return plannerHookProbeCurrent, nil
		})
	})
	prov := &plannerHookProbeProvider{planner: true}
	plannerHookProbeMu.Lock()
	plannerHookProbeCurrent = prov
	plannerHookProbeMu.Unlock()
	t.Cleanup(func() {
		plannerHookProbeMu.Lock()
		plannerHookProbeCurrent = nil
		plannerHookProbeMu.Unlock()
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "executor"

[agent]
planner_model = "planner"

[[providers]]
name = "executor"
kind = "`+plannerHookProbeKind+`"
model = "executor-model"

[[providers]]
name = "planner"
kind = "`+plannerHookProbeKind+`"
model = "planner-model"
`)
	approveWorkspace(t, dir)
	writeFile(t, dir, "marker.txt", "planner hook probe")
	logPath := filepath.Join(dir, "hook.log")
	writeFile(t, dir, "deny-read.sh", "#!/bin/sh\ncat >> "+shellQuoteForTest(logPath)+"\nexit 2\n")
	writeFile(t, dir, "log-prompt.sh", "#!/bin/sh\ncat >> "+shellQuoteForTest(logPath)+"\n")
	for _, name := range []string{"deny-read.sh", "log-prompt.sh"} {
		if err := os.Chmod(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	settings, err := json.Marshal(map[string]any{"hooks": map[string]any{
		"PreToolUse":       []any{map[string]string{"match": "read_file", "command": filepath.Join(dir, "deny-read.sh")}},
		"UserPromptSubmit": []any{map[string]string{"command": filepath.Join(dir, "log-prompt.sh")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, ".reasonix/settings.json", string(settings))
	if err := hook.ApproveProjectHooks(hook.LoadOptions{ProjectRoot: dir}); err != nil {
		t.Fatal(err)
	}

	ctrl, err := Build(context.Background(), withTestSession(t, Options{Sink: event.Discard}))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "plan only: read marker.txt and outline the change"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !prov.plannerRan() {
		t.Fatal("planner never reached the provider")
	}
	first := plannerHookSessions(t, logPath)
	if prov.readLeaked() {
		t.Fatal("planner received marker.txt contents past a PreToolUse deny")
	}
	if !prov.readBlocked() {
		t.Fatal("planner's read_file result was not blocked by PreToolUse")
	}

	if err := ctrl.ClearSession(); err != nil {
		t.Fatalf("ClearSession: %v", err)
	}
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Run(context.Background(), "plan only: read marker.txt again and outline the change"); err != nil {
		t.Fatalf("Run after ClearSession: %v", err)
	}
	second := plannerHookSessions(t, logPath)
	if second == first {
		t.Fatalf("parent session %q did not change across ClearSession", first)
	}
}

// plannerHookSessions returns the parent session the prompt hook saw, after
// checking the planner's read_file hook fired as "<parent>:planner".
func plannerHookSessions(t *testing.T, logPath string) string {
	t.Helper()
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("no hook ran: %v", err)
	}
	var parent, planner string
	for line := range strings.SplitSeq(strings.TrimSpace(string(log)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var p hookPayloadForTest
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			t.Fatalf("decode hook payload %q: %v", line, err)
		}
		switch {
		case p.Event == "UserPromptSubmit":
			parent = p.SessionID
		case p.Event == "PreToolUse" && p.ToolName == "read_file":
			planner = p.SessionID
		}
	}
	if parent == "" {
		t.Fatalf("UserPromptSubmit hook logged no session ID:\n%s", log)
	}
	if planner == "" {
		t.Fatalf("PreToolUse hook never ran for the planner's read_file:\n%s", log)
	}
	if planner != parent+":planner" {
		t.Fatalf("planner hook session = %q, want %q", planner, parent+":planner")
	}
	return parent
}

type plannerHookProbeProvider struct {
	planner bool
	mu      sync.Mutex
	calls   int
	blocked bool
	leaked  bool
}

func (p *plannerHookProbeProvider) Name() string { return plannerHookProbeKind }

func (p *plannerHookProbeProvider) plannerRan() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls > 0
}

func (p *plannerHookProbeProvider) readBlocked() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.blocked
}

func (p *plannerHookProbeProvider) readLeaked() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.leaked
}

func (p *plannerHookProbeProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.calls++
	for _, msg := range req.Messages {
		if msg.Role != provider.RoleTool || msg.Name != "read_file" {
			continue
		}
		if strings.Contains(msg.Content, "blocked:") {
			p.blocked = true
		}
		if strings.Contains(msg.Content, "planner hook probe") {
			p.leaked = true
		}
	}
	p.mu.Unlock()

	chunks := []provider.Chunk{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}}
	if p.planner && len(req.Messages) > 0 {
		switch last := req.Messages[len(req.Messages)-1]; {
		case last.Role == provider.RoleUser:
			chunks = []provider.Chunk{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "planner-read", Name: "read_file", Arguments: `{"path":"marker.txt"}`}}}
		case last.Role == provider.RoleTool && last.Name == "read_file":
			chunks = []provider.Chunk{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "planner-submit", Name: "submit_plan", Arguments: `{"objective":"outline","steps":[{"title":"outline the change"}]}`}}}
		}
	}
	ch := make(chan provider.Chunk, len(chunks))
	for _, chunk := range chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}
