package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/memory"
)

func TestEffectARecalledFactIsRecordedAsUsedAndItsFileStaysPut(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	rec := &effectRecordingProvider{}
	provider.Register("boot-effect-memory-usage", func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-effect-memory-usage"
model = "x"
`)
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	path, err := ctrl.SaveMemory(memory.Memory{
		Name: "warehouse-schema", Description: "the warehouse schema lives in schema.sql",
		Type: memory.TypeProject, Body: "The warehouse schema lives in schema.sql at the repository root.",
	})
	if err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"hello", "where does the warehouse schema live"} {
		if err := ctrl.Run(context.Background(), text); err != nil {
			t.Fatalf("Run(%q): %v", text, err)
		}
	}
	if got := lastUserTurn(t, agentRequests(rec.requests())); !strings.Contains(got, "schema.sql") {
		t.Fatalf("the fact never reached the provider, so there is no use to record:\n%s", got)
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), ".usage.json"))
	if err != nil {
		t.Fatalf("no usage file after a recall: %v", err)
	}
	var data struct {
		Facts map[string]struct {
			Uses int `json:"uses"`
		} `json:"facts"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	uses := 0
	for _, f := range data.Facts {
		uses += f.Uses
	}
	if uses != 1 {
		t.Fatalf("recorded uses = %d, want 1:\n%s", uses, raw)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("recording a use rewrote the fact file")
	}
}
