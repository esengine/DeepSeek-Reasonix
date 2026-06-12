package main

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/control"
)

func TestEnsureBlankTabInheritsActiveTabSettings(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspace := robustTempDir(t)
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"),
		[]byte("[codegraph]\nenabled = false\n"), 0o644); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}

	effort := "max"
	app := NewApp()
	src := &WorkspaceTab{
		ID:               "src",
		Scope:            "project",
		WorkspaceRoot:    workspace,
		TopicID:          "topic_src",
		SessionPath:      filepath.Join(workspace, "src.jsonl"), // non-empty so src isn't reused as the blank tab
		model:            "inherit/model",
		effort:           &effort,
		mode:             "plan",
		toolApprovalMode: control.ToolApprovalYolo,
		disabledMCP:      map[string]ServerView{"srv-x": {}},
		mcpOrder:         []string{"srv-x", "srv-y"},
	}
	src.sink = &tabEventSink{tabID: "src", app: app}
	app.tabs["src"] = src
	app.tabOrder = []string{"src"}
	app.activeTabID = "src"

	meta, err := app.EnsureBlankTab("project", workspace)
	if err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}
	if meta.ID == "" || meta.ID == "src" {
		t.Fatalf("expected a fresh tab, got meta.ID=%q", meta.ID)
	}

	created := app.tabs[meta.ID]
	if created == nil {
		t.Fatalf("new tab %q missing from app.tabs", meta.ID)
	}
	if created.effort == nil || *created.effort != "max" {
		t.Fatalf("effort = %v, want inherited \"max\"", created.effort)
	}
	if created.toolApprovalMode != control.ToolApprovalYolo {
		t.Fatalf("toolApprovalMode = %q, want inherited %q", created.toolApprovalMode, control.ToolApprovalYolo)
	}
	if created.mode != "plan" {
		t.Fatalf("mode = %q, want inherited \"plan\"", created.mode)
	}
	if _, ok := created.disabledMCP["srv-x"]; !ok {
		t.Fatalf("disabledMCP = %v, want inherited key \"srv-x\"", created.disabledMCP)
	}
	if len(created.mcpOrder) != 2 || created.mcpOrder[0] != "srv-x" {
		t.Fatalf("mcpOrder = %v, want inherited [srv-x srv-y]", created.mcpOrder)
	}
}
