package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/hook"
)

func TestBuildResumedSkillKeepsHookSessionID(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	registerBootSubagentTestProvider()
	prov := &bootSubagentTestProvider{hookSessionProbe: true}
	setBootSubagentTestProvider(t, prov)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[[providers]]
name = "test-model"
kind = "boot-subagent-test"
model = "x"
`)
	approveWorkspace(t, dir)
	writeFile(t, dir, "marker.txt", "hook probe")
	writeFile(t, dir, ".reasonix/skills/hook-probe.md", "---\ndescription: inspect a marker\nrunAs: subagent\nallowed-tools: read_file\n---\nRead the requested file.")
	logPath := filepath.Join(dir, "hook.log")
	script := filepath.Join(dir, "log-read.sh")
	writeFile(t, dir, "log-read.sh", "#!/bin/sh\ncat >> "+shellQuoteForTest(logPath)+"\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	settings, err := json.Marshal(map[string]any{"hooks": map[string]any{"PreToolUse": []any{map[string]string{"match": "read_file", "command": script}}}})
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
	ctrl.EnsureSessionPath()
	if err := ctrl.Run(context.Background(), "read marker with the skill"); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	ref := subagentRefFromHistory(t, ctrl.History())
	prov.setContinueRef(ref)
	if err := ctrl.Run(context.Background(), "resume the skill and read marker again"); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read hook log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	if len(lines) != 2 {
		t.Fatalf("PreToolUse calls = %d, want two child reads; log=%q", len(lines), log)
	}
	var sessionIDs [2]string
	for i, line := range lines {
		var payload struct{ ToolName, SessionID string }
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("decode hook payload: %v", err)
		}
		if payload.ToolName != "read_file" || payload.SessionID == "" {
			t.Fatalf("hook payload = %+v, want read_file with session ID", payload)
		}
		sessionIDs[i] = payload.SessionID
	}
	if sessionIDs[0] != sessionIDs[1] {
		t.Fatalf("resumed skill hook session IDs differ: %q and %q", sessionIDs[0], sessionIDs[1])
	}
	if sessionIDs[0] != "subagent:"+ref {
		t.Fatalf("skill hook session ID = %q, want transcript identity %q", sessionIDs[0], "subagent:"+ref)
	}
}
