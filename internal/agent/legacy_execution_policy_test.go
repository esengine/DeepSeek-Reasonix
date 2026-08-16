package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestLegacyExecutionPolicyIsStrippedFromProviderRequest(t *testing.T) {
	sess := NewSession("system")
	sess.Add(provider.Message{
		Role:       provider.RoleUser,
		Content:    "fix parser.go\n\n<execution-policy version=\"2\">\nroute=full-plan risk=high\n</execution-policy>",
		RawContent: "fix parser.go",
	})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "looking"})
	prov := &userInputCaptureProvider{}
	a := New(prov, tool.NewRegistry(), sess, Options{}, event.Discard)
	if err := a.Run(WithRawUserInput(context.Background(), "continue"), "continue"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, m := range prov.request.Messages {
		if m.Role == provider.RoleUser && strings.Contains(m.Content, "<execution-policy") {
			t.Fatalf("historical execution-policy leaked into provider request: %q", m.Content)
		}
	}
	stored := sess.Snapshot()
	if !strings.Contains(stored[1].Content, "<execution-policy version=\"2\">") {
		t.Fatal("stored historical execution-policy must remain readable")
	}
	if stored[1].RawContent != "fix parser.go" {
		t.Fatalf("raw content rewritten: %q", stored[1].RawContent)
	}
}
