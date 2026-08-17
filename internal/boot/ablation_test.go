package boot

import (
	"context"
	"path/filepath"
	"testing"

	"reasonix/internal/ablation"
	"reasonix/internal/event"
)

func builtToolNames(t *testing.T, set ablation.Set) map[string]bool {
	t.Helper()
	ctrl, err := Build(context.Background(), Options{
		SessionDir: filepath.Join(t.TempDir(), "sessions"),
		TokenMode:  TokenModeFull,
		Sink:       event.Discard,
		Ablation:   set,
	})
	if err != nil {
		t.Fatalf("Build(%s): %v", set.Arm(), err)
	}
	defer ctrl.Close()
	// Ablation is judged against the full registry (including use_capability
	// dispatch targets), not only the lean provider-visible surface.
	names := map[string]bool{}
	for _, e := range ctrl.AllToolContractEntries() {
		names[e.Name] = true
	}
	return names
}

func TestAblationRemovesOnlyTheTargetedToolSurfaces(t *testing.T) {
	isolateConfigHome(t)
	t.Chdir(robustTempDir(t))

	full := builtToolNames(t, ablation.Set{})
	for _, name := range []string{"task", "read_only_task", "history", "memory", "remember", "list_sessions", "set_session_title"} {
		if !full[name] {
			t.Fatalf("control arm is missing %s; the ablation assertions below would be vacuous", name)
		}
	}
	if !full["use_capability"] {
		t.Fatal("control arm must register use_capability")
	}

	noSubagent := builtToolNames(t, ablation.New(ablation.Subagent))
	for _, name := range []string{"task", "read_only_task", "parallel_tasks", "fleet"} {
		if noSubagent[name] {
			t.Errorf("subagent ablation left %s registered", name)
		}
	}
	if !noSubagent["history"] || !noSubagent["memory"] {
		t.Error("subagent ablation must not touch the retrieval surfaces")
	}

	noRetrieval := builtToolNames(t, ablation.New(ablation.Retrieval))
	for _, name := range []string{"history", "memory"} {
		if noRetrieval[name] {
			t.Errorf("retrieval ablation left the BM25-backed %s registered", name)
		}
	}
	for _, name := range []string{"remember", "forget", "list_sessions", "read_session", "task"} {
		if !noRetrieval[name] {
			t.Errorf("retrieval ablation dropped %s, which is not a retrieval surface", name)
		}
	}
}
