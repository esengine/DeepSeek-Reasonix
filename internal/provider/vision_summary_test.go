package provider

import (
	"testing"
)

func TestModelMessagesStripsVisionSummaryMetadataButKeepsSummaryContent(t *testing.T) {
	for _, role := range []Role{RoleUser, RoleTool} {
		stored := []Message{{
			Role:    role,
			Content: "question\n\n<reasonix-image-context>chart</reasonix-image-context>",
			VisionSummary: &VisionSummary{
				Version: 1, PromptVersion: "image-summary-v1", ModelRef: "vision/model",
				ImageDigests: []string{"digest"}, Summary: "chart",
			},
		}}
		model := ModelMessages(stored)
		if len(model) != 1 || model[0].VisionSummary != nil || model[0].Content != stored[0].Content {
			t.Fatalf("provider projection = %+v", model)
		}
		if stored[0].VisionSummary == nil {
			t.Fatal("provider projection mutated stored metadata")
		}
	}
}
