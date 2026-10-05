package trajectory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
)

func TestURLInteractionSecretsStayInTheLiveFrontend(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "trajectory.jsonl")
	inner := &capabilitySink{}
	r, err := New(inner, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Emit(event.Event{Kind: event.AskRequest, Ask: event.Ask{ID: "a1", Origin: &event.AskOrigin{Kind: event.AskOriginMCP, Source: "external", URL: "https://example.invalid/?state=private-url", Message: "private-message"}, Questions: []event.AskQuestion{{ID: "mcp.url", Prompt: "Complete an external interaction"}}}})
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private-") {
		t.Fatalf("URL interaction leaked: %s", data)
	}
	if inner.events[0].Ask.Origin.URL != "https://example.invalid/?state=private-url" || inner.events[0].Ask.Origin.Message != "private-message" {
		t.Fatal("live frontend lost its URL or server text")
	}
}
