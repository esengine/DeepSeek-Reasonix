package serve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/store"
)

func TestWireLogOmitsURLPromptButKeepsItsDecisionAndOrdinaryForms(t *testing.T) {
	session := filepath.Join(testenv.TempDir(t), "session.jsonl")
	var log wireLog
	urlPrompt := []byte(`{"kind":"ask_request","nonPersistable":true,"ask":{"id":"url","questions":[{"id":"mcp.url","prompt":"Complete an external interaction"}],"origin":{"kind":"mcp","source":"external","url":"https://example.invalid/?state=PRIVATE_URL","message":"PRIVATE_MESSAGE"}}}`)
	log.write(session, urlPrompt)
	log.write(session, []byte(`{"kind":"notice","nonPersistable":true,"text":"PRIVATE_NOTICE"}`))
	log.write(session, []byte(`{"kind":"notice","decisionReceipt":{"id":"url","kind":"ask","subject":"external interaction from external","outcome":"accepted"}}`))
	log.write(session, []byte(`{"kind":"ask_request","ask":{"id":"form","origin":{"kind":"mcp","source":"external","message":"Fill the form"}}}`))
	data, err := os.ReadFile(store.SessionWireLog(session))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "PRIVATE_") || !strings.Contains(string(data), "accepted") || !strings.Contains(string(data), "Fill the form") {
		t.Fatalf("unsafe or incomplete persisted projection: %s", data)
	}
	if !strings.Contains(string(urlPrompt), "PRIVATE_URL") {
		t.Fatal("persistence changed the live frame")
	}
}
