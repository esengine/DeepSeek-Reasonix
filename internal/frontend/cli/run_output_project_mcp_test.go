package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
)

// A held project server reaches a headless run as one diagnostic line naming
// the server, the command and how to approve it; stdout keeps only the answer
// and the run finishes as it would have.
func TestRunOutputReportsAHeldProjectServerOnOneLine(t *testing.T) {
	var out, errOut bytes.Buffer
	sink := newRunOutputSink(&out, runOutputText)
	sink.errOut = &errOut
	sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Code: event.NoticeCodeProjectMCPChanged,
		Text:   "MCP server \"docs\" changed since you enabled it; run `reasonix mcp enable docs` to approve the command shown.",
		Detail: "command: node ./mcp/server.js"})
	sink.Emit(event.Event{Kind: event.Message, Text: "answer"})
	if err := sink.Finalize("session", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	if out.String() != "answer\n" {
		t.Fatalf("stdout = %q", out.String())
	}
	lines := strings.Split(strings.TrimSpace(errOut.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "reasonix mcp enable docs") || !strings.Contains(lines[0], "node ./mcp/server.js") {
		t.Fatalf("diagnostics = %q, want one line with the server, how to approve and the command", errOut.String())
	}
}
