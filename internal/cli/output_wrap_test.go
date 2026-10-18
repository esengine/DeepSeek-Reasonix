package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"reasonix/internal/event"
)

// TestShellOutputWrapsLongLines proves a long shell output line is wrapped to the
// viewport width instead of being clamped: the terminal then never soft-wraps it
// out of the connector gutter, and no text is lost.
func TestShellOutputWrapsLongLines(t *testing.T) {
	m := newTestChatTUI()
	m.width = 60

	// The tail of a real PowerShell CommandNotFoundException, in Russian.
	long := "ды : Имя \"ды\" не опознано как имя командлета, функции, файла сценария " +
		"или выполняемой программы. Проверьте правильность написания имени, а также " +
		"наличие и правильность пути и повторите попытку."

	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
		ID: "shell-ды", Name: "bash", Args: `{"command":"ды"}`,
	}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		ID: "shell-ды", Name: "bash", Output: long + "\n",
	}})

	joined := ansi.Strip(strings.Join(m.transcript, "\n"))
	if !strings.Contains(joined, "повторите попытку") {
		t.Fatalf("the tail of a long line must survive rendering:\n%s", joined)
	}
	for _, entry := range m.transcript {
		for _, ln := range strings.Split(ansi.Strip(entry), "\n") {
			if got := visibleWidth(ln); got > m.width {
				t.Fatalf("rendered line exceeds viewport width (%d > %d): %q", got, m.width, ln)
			}
		}
	}
}
