package serve

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
)

type countSink struct {
	event.Sink
	dormant *int
}

func (c countSink) Emit(e event.Event) {
	if e.Kind == event.Notice && e.Code == event.NoticeCodePermissionRulesDormant {
		*c.dormant++
	}
	c.Sink.Emit(e)
}

// A model switch inside one conversation does not repeat a build-time notice
// the client already has; /new retires what clients were shown, so the same
// still-true notice is shown again on the next build.
func TestNewSessionMakesStillTrueBuildNoticesVisibleAgain(t *testing.T) {
	home, root := testenv.TempDir(t), testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))
	t.Chdir(root)
	kind := fmt.Sprintf("build-notices-session-%d", switchSkillProviderSeq.Add(1))
	rec := testutil.NewMock(kind, testutil.Turn{Text: "ok"})
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writePluginFile(t, filepath.Join(home, "config.toml"), `
default_model = "first"
[environment]
enabled = false
[codegraph]
enabled = false
[permissions]
ask = ["rm"]
[[providers]]
name = "first"
kind = "`+kind+`"
model = "a"
[[providers]]
name = "second"
kind = "`+kind+`"
model = "b"
`)
	bc := NewBroadcaster()
	var dormant int
	initial, err := boot.BuildRuntime(t.Context(), boot.Options{Sink: countSink{bc, &dormant}, WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	s := New(initial.Controller, bc, config.ServeConfig{})
	s.AdoptRuntime(initial)
	s.SetPaneSink(countSink{bc, &dormant})
	leases := control.NewSessionLeaseKeeper()
	s.SetSessionLeases(leases)
	t.Cleanup(leases.Release)
	t.Cleanup(func() { s.ctl().Close() })
	if dormant != 1 {
		t.Fatalf("first build emitted %d dormant-rule notices, want 1", dormant)
	}
	if err := s.switchModel(t.Context(), "second"); err != nil {
		t.Fatal(err)
	}
	if dormant != 1 {
		t.Fatalf("a model switch repeated the notice: %d", dormant)
	}
	s.newSession(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/new", strings.NewReader("")))
	if err := s.switchModel(t.Context(), "first"); err != nil {
		t.Fatal(err)
	}
	if dormant != 2 {
		t.Fatalf("after /new the still-true notice was not shown again: %d", dormant)
	}
}
