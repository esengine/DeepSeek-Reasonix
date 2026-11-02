package delegation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

func testSubagentSpec(t *testing.T, name string) SubagentSpec {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	return SubagentSpec{
		ExecutionID:   "exec-test",
		Kind:          "task",
		Name:          name,
		WorkspaceRoot: testenv.TempDir(t),
		ParentSession: "parent-session",
		SystemPrompt:  "review persona",
		Registry:      reg,
		Model:         "deepseek",
		Effort:        "max",
	}
}

func TestPrepareFreshStartsAnEmptySessionAndCompletionPersistsIt(t *testing.T) {
	store := NewSubagentStore(testenv.TempDir(t))
	spec := testSubagentSpec(t, "audit")
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	defer run.Release()
	if run.Ref == "" || run.Meta.Status != sessionstore.SubagentRunning {
		t.Fatalf("fresh run = ref %q status %q, want a ref and running", run.Ref, run.Meta.Status)
	}
	if err := store.MarkRunning(run); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	if err := store.SaveCompleted(run); err != nil {
		t.Fatalf("SaveCompleted: %v", err)
	}
	meta, err := store.LoadMeta(run.Ref)
	if err != nil || meta.Status != sessionstore.SubagentCompleted || meta.Name != "audit" {
		t.Fatalf("stored meta = %+v err=%v, want completed audit", meta, err)
	}
}

func TestPrepareFreshRefusesASpecWithoutAParentSession(t *testing.T) {
	store := NewSubagentStore(testenv.TempDir(t))
	spec := testSubagentSpec(t, "audit")
	spec.ParentSession = ""
	if run, err := store.PrepareFresh(spec); err == nil {
		run.Release()
		t.Fatal("PrepareFresh accepted a spec with no parent session")
	}
}

func TestSpawnRefusesBadArgumentsBeforeRunningAnything(t *testing.T) {
	s := NewSpawner(NewRunner(RunnerOptions{}))
	for name, args := range map[string]string{
		"not json":   `{`,
		"empty":      `{"tasks":[]}`,
		"blank task": `{"tasks":[{"title":"a","task":"  "}]}`,
		"too many":   `{"tasks":[` + strings.TrimSuffix(strings.Repeat(`{"title":"a","task":"b"},`, maxSpawnTasks+1), ",") + `]}`,
	} {
		if out, err := s.Execute(context.Background(), json.RawMessage(args)); err == nil {
			t.Errorf("%s: Execute = %q, want an error", name, out)
		}
	}
}

func TestSpawnSchemaIsValidJSONAndSpawnIsNotReadOnly(t *testing.T) {
	s := NewSpawner(nil)
	if !json.Valid(s.Schema()) || s.ReadOnly() || s.Name() != SpawnToolName {
		t.Fatalf("spawn contract: valid=%v readOnly=%v name=%q", json.Valid(s.Schema()), s.ReadOnly(), s.Name())
	}
}

func TestTruncateRunesKeepsWholeCharacters(t *testing.T) {
	if got := truncateRunes("你好世界", 2); got != "你好" {
		t.Fatalf("truncateRunes = %q, want 你好", got)
	}
	if got := truncateRunes("ab", 5); got != "ab" {
		t.Fatalf("truncateRunes = %q, want ab", got)
	}
}
