package control

import (
	"path/filepath"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
)

func TestMutationAuthorityCannotBorrowAReplacementConversation(t *testing.T) {
	for _, change := range []string{"new-session", "same-path-replacement", "close"} {
		t.Run(change, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "source.jsonl")
			executor := agent.New(nil, tool.NewRegistry(), sessionstore.NewSession(""), agent.Options{}, event.Discard)
			controller := New(Options{Executor: executor, SessionPath: path, SessionDir: dir, Sink: event.Discard})
			t.Cleanup(func() { controller.Close(); controller.autosaveWG.Wait() })
			validate := controller.MutationExecutionValidator(sessionstore.BranchID(path))
			if err := validate(t.Context()); err != nil {
				t.Fatalf("original conversation lost authorization: %v", err)
			}
			switch change {
			case "new-session":
				if err := controller.NewSession(); err != nil {
					t.Fatal(err)
				}
			case "same-path-replacement":
				executor.SetSession(sessionstore.NewSession(""))
			case "close":
				controller.Close()
			}
			if err := validate(t.Context()); err == nil {
				t.Fatal("old integration borrowed replacement runtime/session authority")
			}
		})
	}
}
