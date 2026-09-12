package serve

import (
	"testing"

	"reasonix/internal/provider"
)

func TestHistoryMessagesExposeOnlyBuiltInPresentMetadata(t *testing.T) {
	messages := []provider.Message{
		{Role: provider.RoleTool, ToolCallID: "present-1", Name: "present", Content: "Presented report.md", PresentedFiles: provider.NewPresentedFilesMetadata([]provider.PresentedFile{{Path: "report.md", Description: "Report"}})},
		{Role: provider.RoleTool, ToolCallID: "plugin-1", Name: "plugin.present", Content: "fake", PresentedFiles: provider.NewPresentedFilesMetadata([]provider.PresentedFile{{Path: "fake.md"}})},
	}
	history := historyMessages(messages)
	if len(history) != 2 || len(history[0].PresentedFiles) != 1 || history[0].PresentedFiles[0].Path != "report.md" {
		t.Fatalf("present history = %#v", history)
	}
	if len(history[1].PresentedFiles) != 0 {
		t.Fatalf("plugin metadata escaped into trusted history: %#v", history[1].PresentedFiles)
	}
}
