package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// writeTraj lays down a trajectory whose rounds all report prefix hash "p1".
func writeTraj(t *testing.T, dir, id string, lines ...string) string {
	t.Helper()
	path := filepath.Join(dir, id+".trajectory.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

const (
	trajHeader = `{"seq":1,"run_header":{"model_ref":"ds/v4","workspace_root":"/tmp/wk","system":"sys","prefix_hash":"p1","tools":[{"name":"bash"}]}}`
	trajUsage  = `{"seq":9,"event":{"kind":"usage","usage":{"cacheDiagnostics":{"prefixHash":"p1"}}}}`
)

func TestBuildSFTSampleJoinsPrefixToConversation(t *testing.T) {
	path := writeTraj(t, testenv.TempDir(t), "demo",
		trajHeader,
		`{"seq":2,"event":{"kind":"tool_dispatch","tool":{"id":"c1","name":"bash","partial":true}}}`,
		`{"seq":3,"event":{"kind":"message","text":"","reasoning":"look first"}}`,
		trajUsage,
		`{"seq":10,"event":{"kind":"tool_dispatch","tool":{"id":"c1","name":"bash","args":"{\"command\":\"ls /tmp/wk\"}"}}}`,
		`{"seq":11,"event":{"kind":"tool_result","tool":{"id":"c1","name":"bash","output":"/tmp/wk/a.py"}}}`,
		`{"seq":12,"event":{"kind":"message","text":"done"}}`,
		`{"seq":13,"event":{"kind":"usage","usage":{"cacheDiagnostics":{"prefixHash":"p1"}}}}`,
	)
	got, why := buildSFTSample("demo", "fix it", path)
	if got == nil {
		t.Fatalf("no sample: %s", why)
	}
	roles := make([]string, len(got.Messages))
	for i, m := range got.Messages {
		roles[i] = m.Role
	}
	want := []string{"system", "user", "assistant", "tool", "assistant"}
	if strings.Join(roles, ",") != strings.Join(want, ",") {
		t.Fatalf("roles = %v, want %v", roles, want)
	}
	if got.Messages[2].Reasoning != "look first" || len(got.Messages[2].ToolCalls) != 1 {
		t.Errorf("assistant turn lost its reasoning or call: %+v", got.Messages[2])
	}
	// The partial dispatch carries no arguments; the full one must win.
	if args := got.Messages[2].ToolCalls[0].Function.Arguments; !strings.Contains(args, "command") {
		t.Errorf("arguments = %q, want the full dispatch's", args)
	}
	if got.Messages[3].ToolCallID != "c1" || got.Messages[4].Content != "done" {
		t.Errorf("result or final answer misplaced: %+v", got.Messages[3:])
	}
	// The workspace path is per-run; training on it teaches a dead location.
	for _, m := range got.Messages {
		if strings.Contains(m.Content, "/tmp/wk") {
			t.Errorf("workspace path survived into %q", m.Content)
		}
	}
	if !strings.Contains(got.Messages[2].ToolCalls[0].Function.Arguments, workspacePlaceholder) {
		t.Errorf("tool arguments were not made portable: %+v", got.Messages[2].ToolCalls[0])
	}
}

func TestBuildSFTSampleRejectsRunWithoutHeader(t *testing.T) {
	path := writeTraj(t, testenv.TempDir(t), "bare", trajUsage)
	if got, why := buildSFTSample("bare", "p", path); got != nil || why != "no run header" {
		t.Errorf("got (%v, %q), want a refusal naming the missing header", got, why)
	}
}

// A header from another build describes a different request than the rounds it
// sits next to; exporting it would train against a prefix that never ran.
func TestBuildSFTSampleRejectsPrefixMismatch(t *testing.T) {
	path := writeTraj(t, testenv.TempDir(t), "drift",
		trajHeader,
		`{"seq":9,"event":{"kind":"usage","usage":{"cacheDiagnostics":{"prefixHash":"OTHER"}}}}`,
		`{"seq":10,"event":{"kind":"message","text":"hi"}}`,
	)
	got, why := buildSFTSample("drift", "p", path)
	if got != nil || !strings.Contains(why, "0/1 rounds") {
		t.Errorf("got (%v, %q), want a refusal counting the uncovered rounds", got, why)
	}
}

// Sub-agent calls were sampled against their own prefix, so they must not be
// attributed to this sample's header.
func TestConvertSFTSkipsSubagentCalls(t *testing.T) {
	path := writeTraj(t, testenv.TempDir(t), "deleg",
		trajHeader,
		`{"seq":2,"event":{"kind":"tool_dispatch","tool":{"id":"child","name":"bash","args":"{}","parentId":"c1"}}}`,
		`{"seq":3,"event":{"kind":"tool_result","tool":{"id":"child","name":"bash","output":"x","parentId":"c1"}}}`,
		`{"seq":4,"event":{"kind":"message","text":"done"}}`,
		trajUsage,
	)
	got, why := buildSFTSample("deleg", "p", path)
	if got == nil {
		t.Fatalf("no sample: %s", why)
	}
	for _, m := range got.Messages {
		if m.ToolCallID == "child" || len(m.ToolCalls) > 0 {
			t.Errorf("sub-agent call leaked into the sample: %+v", m)
		}
	}
}

func TestRunSFTModeKeepsOnlyGradedPasses(t *testing.T) {
	dir := testenv.TempDir(t)
	traj := filepath.Join(dir, "traj")
	if err := os.Mkdir(traj, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []string{trajHeader, `{"seq":2,"event":{"kind":"message","text":"ok"}}`, trajUsage}
	writeTraj(t, traj, "fizzbuzz", body...)
	writeTraj(t, traj, "palindrome", body...)

	report := filepath.Join(dir, "r.json")
	raw, _ := json.Marshal([]result{
		{task: task{ID: "fizzbuzz"}, Passed: true},
		{task: task{ID: "palindrome"}, Passed: false},
	})
	if err := os.WriteFile(report, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "sft.jsonl")
	if err := runSFTMode(traj, "../../benchmarks/e2e", report, out); err != nil {
		t.Fatalf("runSFTMode: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("wrote %d samples, want only the graded pass:\n%s", len(lines), data)
	}
	var got sftSample
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.TaskID != "fizzbuzz" || got.ModelRef != "ds/v4" {
		t.Errorf("sample = %+v, want the passing task with its model", got)
	}
	if got.Messages[1].Role != "user" || got.Messages[1].Content == "" {
		t.Errorf("task prompt missing from the sample: %+v", got.Messages[1])
	}
}

func TestRunSFTModeRequiresAReport(t *testing.T) {
	if err := runSFTMode(testenv.TempDir(t), "../../benchmarks/e2e", "", "out.jsonl"); err == nil {
		t.Error("want an error naming the missing report")
	}
}
