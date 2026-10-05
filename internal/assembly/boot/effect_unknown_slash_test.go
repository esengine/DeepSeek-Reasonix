package boot

import (
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

func TestEffectUnknownSlashReachesProviderAsAUserMessageWithANotice(t *testing.T) {
	isolateConfigHome(t)
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	writeFile(t, workspace, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "BASE"
[environment]
enabled = false
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-unknown-slash"
model = "x"
`)
	approveWorkspace(t, workspace)
	rec := &effectRecordingProvider{}
	provider.Register("boot-unknown-slash", func(provider.Config) (provider.Provider, error) { return rec, nil })
	var mu sync.Mutex
	var notices []event.Event
	ctrl, err := Build(t.Context(), Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Code == event.NoticeCodeUnknownCommand {
			mu.Lock()
			notices = append(notices, e)
			mu.Unlock()
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()

	const line = "/not-a-registered-command with arguments"
	ctrl.Submit(line)
	waitForCond(t, "unknown slash provider request", 10*time.Second, func() bool { return len(rec.requests()) == 1 && !ctrl.Running() })

	req := rec.requests()[0]
	var user string
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser {
			user = m.Content
		}
	}
	if !strings.Contains(user, line) {
		t.Fatalf("provider user message lacks the line:\n%s", user)
	}
	if raw := rec.rawUserInputs(); len(raw) != 1 || raw[0] != line {
		t.Fatalf("turn pipeline saw raw input %q, want the typed line", raw)
	}
	mu.Lock()
	got := append([]event.Event(nil), notices...)
	mu.Unlock()
	if len(got) != 1 || !strings.Contains(got[0].Text, "/not-a-registered-command") {
		t.Fatalf("notices = %+v", got)
	}

	ctrl.SubmitHTTPOptions("/another-unknown", control.SubmitOptions{RefuseUnknownSlash: true})
	if ctrl.Running() || len(rec.requests()) != 1 {
		t.Fatal("a submitter that asked to refuse still started a turn")
	}
}
