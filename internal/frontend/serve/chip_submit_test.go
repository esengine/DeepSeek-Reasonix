package serve

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/skill"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

type chipRig struct {
	s       *Server
	model   *testutil.MockProvider
	started chan event.Event
	done    chan struct{}
}

func newChipRig(t *testing.T) *chipRig {
	t.Helper()
	dir := testenv.TempDir(t)
	rig := &chipRig{
		model:   testutil.NewMock("exec", testutil.Turn{Text: "done"}),
		started: make(chan event.Event, 4),
		done:    make(chan struct{}, 4),
	}
	exec := agent.New(rig.model, tool.NewRegistry(), sessionstore.NewSession("sys"), agent.Options{}, event.Discard)
	c := control.New(control.Options{
		Runner: exec, Executor: exec, SessionDir: dir, SessionPath: filepath.Join(dir, "s.jsonl"),
		Skills: []skill.Skill{{Name: "probe", Body: "PROBE BODY", RunAs: skill.RunInline, Scope: skill.ScopeGlobal}},
		Sink: event.FuncSink(func(e event.Event) {
			switch e.Kind {
			case event.TurnStarted:
				rig.started <- e
			case event.TurnDone:
				rig.done <- struct{}{}
			}
		}),
	})
	t.Cleanup(c.Close)
	rig.s = &Server{ctrl: c}
	return rig
}

// post sends one body to a handler and returns the user message the model read.
func (rig *chipRig) post(t *testing.T, handler http.HandlerFunc, path, body string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST %s = %d %s", path, rec.Code, rec.Body.String())
	}
	select {
	case <-rig.started:
	case <-time.After(20 * time.Second):
		t.Fatalf("POST %s started no turn", path)
	}
	select {
	case <-rig.done:
	case <-time.After(20 * time.Second):
		t.Fatalf("POST %s: the turn never finished", path)
	}
	reqs := rig.model.Requests()
	if len(reqs) == 0 {
		t.Fatalf("POST %s reached no provider", path)
	}
	var user string
	for _, m := range reqs[len(reqs)-1].Messages {
		if m.Role == provider.RoleUser {
			user = m.Content
		}
	}
	return user
}

// A chip placed anywhere in the line reaches the model as the same bytes a
// leading "/probe task" does.
func TestASkillChipAnywhereInTheLineRunsAsTheInvocation(t *testing.T) {
	typed := newChipRig(t)
	want := typed.post(t, typed.s.submit, "/submit", `{"input":"/probe tidy the notes"}`)
	if !strings.Contains(want, "<skill-pin name=\"probe\">") || !strings.Contains(want, "Arguments: tidy the notes") {
		t.Fatalf("typed /probe did not pin the skill:\n%s", want)
	}
	cases := []struct{ name, path, body string }{
		{"submit mid-line", "/submit", `{"input":"tidy /probe the notes","submit":"tidy the notes","invocations":[{"name":"probe","kind":"skill","offset":5}]}`},
		{"submit at the end", "/submit", `{"input":"tidy the notes /probe","submit":"tidy the notes","invocations":[{"name":"probe","kind":"skill","offset":15}]}`},
		{"queued follow-up", "/inbox/items", `{"input":"tidy /probe the notes","submit":"tidy the notes","invocations":[{"name":"probe","kind":"skill","offset":5}]}`},
		{"steer holding a chip", "/inbox/items", `{"input":"tidy /probe the notes","submit":"tidy the notes","intent":"steer","invocations":[{"name":"probe","kind":"skill","offset":5}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newChipRig(t)
			handler := rig.s.submit
			if tc.path == "/inbox/items" {
				handler = rig.s.inboxEnqueue
			}
			if got := rig.post(t, handler, tc.path, tc.body); got != want {
				t.Fatalf("chip turn reached the model as\n%s\nwant the typed form\n%s", got, want)
			}
		})
	}
}

func TestAChipAloneIsASubmittableLine(t *testing.T) {
	rig := newChipRig(t)
	got := rig.post(t, rig.s.submit, "/submit", `{"input":"/probe","submit":"","invocations":[{"name":"probe","kind":"skill","offset":0}]}`)
	if !strings.Contains(got, "PROBE BODY") || strings.Contains(got, "Arguments:") {
		t.Fatalf("a lone chip reached the model as:\n%s", got)
	}
}
