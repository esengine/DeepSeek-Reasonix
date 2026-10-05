package control

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
)

type killMidStreamProvider struct{ calls int }

func (*killMidStreamProvider) Name() string { return "kill-mid-stream" }

func (p *killMidStreamProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.calls++
	ch := make(chan provider.Chunk, 4)
	if p.calls == 1 {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "first answer"}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "partial reply"}
	go func() {
		time.Sleep(300 * time.Millisecond)
		os.Exit(73)
	}()
	return ch, nil
}

func TestKilledTurnKeepsItsPrompt(t *testing.T) {
	if root := os.Getenv("REASONIX_TURN_KILL_FIXTURE"); root != "" {
		a := agent.New(&killMidStreamProvider{}, tool.NewRegistry(), sessionstore.NewSession("sys"), agent.Options{}, event.Discard)
		c := New(Options{Executor: a, Runner: a, SessionPath: filepath.Join(root, "session.jsonl"), SessionDir: root, Sink: event.Discard})
		if err := c.RunTurn(context.Background(), "first question"); err != nil {
			t.Fatal(err)
		}
		c.Send("second question")
		time.Sleep(10 * time.Second)
		t.Fatal("fixture was not killed")
	}
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestKilledTurnKeepsItsPrompt$")
	cmd.Env = append(os.Environ(), "REASONIX_TURN_KILL_FIXTURE="+root)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 73 {
		t.Fatalf("kill fixture: %v %s", err, out)
	}
	path := filepath.Join(root, "session.jsonl")
	sess, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	a := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: a, SessionPath: path, SessionDir: root, Sink: event.Discard})
	c.recoverInterruptedTurn(path)
	history := c.History()
	found := false
	for _, m := range history {
		if m.Role == provider.RoleUser && strings.Contains(m.Content, "second question") {
			found = true
		}
	}
	if !found {
		t.Fatalf("prompt of the killed turn lost after restart:\n%s", requestMessagesText(history))
	}
}
