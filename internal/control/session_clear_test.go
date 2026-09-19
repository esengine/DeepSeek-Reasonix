package control

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestSubmitClearDiscardsCurrentContextWithoutSavingTranscript(t *testing.T) {
	dir := t.TempDir()
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "old context"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	path := filepath.Join(dir, "session.jsonl")
	clearResult := make(chan string, 1)
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind != event.Notice || (e.Text != "context cleared" && !strings.HasPrefix(e.Text, "clear context failed: ")) {
			return
		}
		select {
		case clearResult <- e.Text:
		default:
		}
	})
	c := newOwnedTestController(t, Options{Executor: exec, SystemPrompt: "sys", SessionDir: dir, SessionPath: path, Label: "test", Sink: sink})
	if err := c.Snapshot(); err != nil {
		t.Fatal(err)
	}
	ckpt := ckptDir(path)
	if err := os.MkdirAll(ckpt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ckpt, "turn-0.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	c.submit("/clear", "", "")
	select {
	case result := <-clearResult:
		if result != "context cleared" {
			t.Fatal(result)
		}
	case <-time.After(30 * time.Second):
		stack := make([]byte, 2<<20)
		n := runtime.Stack(stack, true)
		t.Fatalf("/clear did not finish\n%s", stack[:n])
	}
	if c.SessionPath() == path {
		t.Fatal("/clear did not rotate to a fresh session path")
	}
	for _, p := range []string{path, agent.BranchMetaPath(path), ckpt} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("discarded artifact %s still exists or stat failed with %v", p, err)
		}
	}
	if _, err := os.Stat(c.SessionPath()); !os.IsNotExist(err) {
		t.Fatalf("fresh empty session should not be saved yet; stat err=%v", err)
	}
	current := exec.Session().Snapshot()
	if len(current) != 1 || current[0].Role != provider.RoleSystem || current[0].Content != "sys" {
		t.Fatalf("cleared context = %+v, want only system prompt", current)
	}
}
