package cli

import (
	"path/filepath"
	"reflect"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/session"
	"reasonix/internal/tool"
)

func TestAppendSystemPromptRestoresCurrentGuidanceWithExactSession(t *testing.T) {
	isolateCLIConfigHome(t)
	service, err := session.NewService("local", session.NewFilesystemPersistence(filepath.Join(t.TempDir(), "sessions")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(t.Context()) })
	newController := func(prompt string) *control.Controller {
		executor := agent.New(nil, tool.NewRegistry(), agent.NewSession(prompt), agent.Options{}, event.Discard)
		ctrl := newOwnedTestController(t, control.Options{Executor: executor, SystemPrompt: prompt, SessionService: service, ExclusiveSession: true, Sink: event.Discard})
		t.Cleanup(ctrl.Close)
		return ctrl
	}
	old := newController("previous process guidance")
	ref, err := old.BindFreshSession(t.Context(), "exact-session")
	if err != nil {
		t.Fatal(err)
	}
	history := old.History()
	history = append(history, provider.Message{ID: agent.NewMessageID(), Role: provider.RoleUser, Content: "retain this task"})
	old.AdoptHistory(history, "")
	runtime, ok := service.Runtime(ref)
	if !ok {
		t.Fatal("session runtime missing")
	}
	visibleBefore := runtime.Session().Snapshot().Projection.Messages
	compacted := []provider.Message{
		history[0],
		{ID: agent.NewMessageID(), Role: provider.RoleUser, Content: "compacted task summary"},
	}
	if err := old.AdoptRebuiltModelContext(compacted); err != nil {
		t.Fatal(err)
	}
	if err := old.Snapshot(); err != nil {
		t.Fatal(err)
	}
	old.Close()
	current := newController("current process guidance 世界")
	if err := commitStartupResumeWithStandingInstructions(nil, nil, current, nil, cliResumeTarget{ref: ref}, flagTakeoverApproval(false), true); err != nil {
		t.Fatal(err)
	}
	gotRef, ok := current.SessionRef()
	if !ok || gotRef != ref {
		t.Fatalf("identity changed: %v", gotRef)
	}
	got := current.History()
	if len(got) != 2 || got[0].Role != provider.RoleSystem || got[0].Content != "current process guidance 世界" || got[1].Content != "compacted task summary" {
		t.Fatalf("restored history = %#v", got)
	}
	if visibleAfter := runtime.Session().Snapshot().Projection.Messages; !reflect.DeepEqual(visibleAfter, visibleBefore) {
		t.Fatalf("resume rewrote full visible history: before=%#v after=%#v", visibleBefore, visibleAfter)
	}
	current.Close()
	check := newController("unrelated new default")
	if _, err := check.OpenSession(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
	if got := check.History(); len(got) != 2 || got[0].Content != "current process guidance 世界" {
		t.Fatalf("guidance not durable: %#v", got)
	}
}
