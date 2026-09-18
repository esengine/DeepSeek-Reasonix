package control

import (
	"context"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/session"
	"reasonix/internal/tool"
)

func TestAttachmentStoreFollowsLatePublishedSessionOwner(t *testing.T) {
	root := t.TempDir()
	service, err := session.NewService("desktop", session.NewFilesystemPersistence(filepath.Join(root, "by-id")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	newAgent := func() *agent.Agent {
		return agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	}
	exec := newAgent()
	opts := Options{WorkspaceRoot: root, SessionDir: filepath.Join(root, "legacy-sessions"), SessionService: service, Executor: exec, Runner: exec}
	c := newOwnedTestController(t, opts)
	ref, err := c.BindFreshSession(t.Context(), "late-owner")
	if err != nil {
		t.Fatal(err)
	}
	runtime, ok := service.Runtime(ref)
	if !ok {
		t.Fatal("session was not published")
	}
	if got, want := c.attachmentService().Store().Root(), runtime.Session().ContentStore().Root(); got != want {
		t.Fatalf("attachment store=%s, session store=%s", got, want)
	}
	draft, err := c.StageImage(t.Context(), "rebuild.png", "image/png", "data:image/png;base64,"+tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Session().ContentStore().Verify(t.Context(), draft.Ref.Content); err != nil {
		t.Fatalf("original is not in the session content graph: %v", err)
	}
	opts.SessionRuntime, opts.ExclusiveSession = runtime, true
	opts.Executor = newAgent()
	opts.Runner = opts.Executor
	next := newOwnedTestController(t, opts)
	if err := ActivateSessionAPIReplacement(c, next); err != nil {
		t.Fatal(err)
	}
	rebound, err := next.RebindDraftImage(t.Context(), draft.ID)
	if err != nil || rebound.ID == draft.ID {
		t.Fatalf("rebind=%+v err=%v", rebound, err)
	}
	if _, _, err := next.ReadDraftImage(t.Context(), rebound.ID); err != nil {
		t.Fatal(err)
	}
}
