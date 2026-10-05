package delegation

import (
	"testing"
)

func TestTaskUsageModelRefUsesCanonicalRuntimeIdentity(t *testing.T) {
	task := (&TaskTool{baseModel: "deepseek/deepseek-v4-pro"}).WithTranscriptIdentityResolver(
		func(modelRef, effort string) (string, string) {
			if modelRef == "flash" {
				return "deepseek/deepseek-v4-flash", effort
			}
			return "deepseek/deepseek-v4-pro", effort
		},
	)
	if got := task.usageModelRef("flash", "high"); got != "deepseek/deepseek-v4-flash" {
		t.Fatalf("alias usage model = %q", got)
	}
	if got := task.usageModelRef("", ""); got != "deepseek/deepseek-v4-pro" {
		t.Fatalf("inherited usage model = %q", got)
	}
}
