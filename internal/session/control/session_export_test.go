package control

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
)

func exportController(t *testing.T, workspace string, msgs ...provider.Message) *Controller {
	t.Helper()
	sess := sessionstore.NewSession("system text never exported")
	for _, m := range msgs {
		sess.Add(m)
	}
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	return New(Options{Executor: exec, WorkspaceRoot: workspace, Sink: event.Discard})
}

func TestExportKeepsOnlyWhatWasSaid(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "fix the bug"},
		{Role: provider.RoleAssistant, Content: "looking", ReasoningContent: "private thoughts"},
		{Role: provider.RoleTool, Content: "tool output"},
		{Role: provider.RoleUser, Content: "host note", HostAuthored: true},
		{Role: provider.RoleAssistant, Content: "done"},
	}
	got, n := ConversationMarkdown(msgs)
	want := "# reasonix session\n\n## User\n\nfix the bug\n\n## Assistant\n\nlooking\n\ndone\n\n"
	if got != want || n != 3 {
		t.Fatalf("export = %q (%d), want %q", got, n, want)
	}
}

// Two exports in the same second must both survive, and a name an attacker
// planted as a symlink must be skipped, not written through.
func TestExportCreatesExclusivelyAndNeverFollowsASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	ws := testenv.TempDir(t)
	outside := filepath.Join(testenv.TempDir(t), "victim")
	c := exportController(t, ws, provider.Message{Role: provider.RoleUser, Content: "hello"})

	first, n, err := c.ExportConversation()
	if err != nil || n != 1 {
		t.Fatalf("first export = %q, %d, %v", first, n, err)
	}
	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, first); err != nil {
		t.Fatal(err)
	}
	second, _, err := c.ExportConversation()
	if err != nil || second == first {
		t.Fatalf("export over a planted symlink = %q, %v", second, err)
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("the symlink target was written: %v", err)
	}
	third, _, err := c.ExportConversation()
	if err != nil || third == second || third == first {
		t.Fatalf("same-second export collided: %q %q %q, %v", first, second, third, err)
	}
	if raw, _ := os.ReadFile(second); !strings.Contains(string(raw), "hello") {
		t.Fatalf("exported file = %q", raw)
	}
	if info, _ := os.Stat(second); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}

func TestExportOfAnEmptyConversationWritesNothing(t *testing.T) {
	ws := testenv.TempDir(t)
	c := exportController(t, ws)
	if path, n, err := c.ExportConversation(); path != "" || n != 0 || err != nil {
		t.Fatalf("empty export = %q, %d, %v", path, n, err)
	}
	if entries, _ := os.ReadDir(ws); len(entries) != 0 {
		t.Fatalf("workspace has %d files", len(entries))
	}
}
