package boot

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent/testutil"
)

func TestResolveShellWithNoticeReportsFallback(t *testing.T) {
	var stderr bytes.Buffer
	var notices []event.Event
	resolveShellWithNotice("not-a-shell", "", &stderr, event.FuncSink(func(e event.Event) {
		notices = append(notices, e)
	}))

	if !strings.Contains(stderr.String(), "not recognised") {
		t.Fatalf("stderr = %q, want the shell warning", stderr.String())
	}
	if len(notices) != 1 {
		t.Fatalf("notices = %+v, want one load warning", notices)
	}
	got := notices[0]
	if got.Kind != event.Notice || got.Level != event.LevelWarn || got.Audience != event.NoticeAudienceOperator || !strings.Contains(got.Detail, "not recognised") {
		t.Fatalf("notice = %+v, want an operator warning carrying the shell detail", got)
	}
}

func TestEffectShellFallbackNoticeReachesFrontendSink(t *testing.T) {
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	writeFile(t, workspace, "reasonix.toml", `
default_model = "test-model"
[codegraph]
enabled = false
[tools.shell]
prefer = "not-a-shell"
[[providers]]
name = "test-model"
kind = "`+bootTokenProfileTestProviderKind+`"
model = "x"
`)
	approveWorkspace(t, workspace)
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("shell-notice"))

	var notices []event.Event
	ctrl, err := Build(t.Context(), Options{
		Sink:          event.FuncSink(func(e event.Event) { notices = append(notices, e) }),
		Stderr:        io.Discard,
		Home:          reasonixHome,
		WorkspaceRoot: workspace,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(ctrl.Close)
	var shellWarnings []event.Event
	for _, notice := range notices {
		if notice.Kind == event.Notice && strings.Contains(notice.Detail, "not recognised") {
			shellWarnings = append(shellWarnings, notice)
		}
	}
	if len(shellWarnings) != 1 {
		t.Fatalf("notices = %+v, want one shell fallback warning", notices)
	}
	got := shellWarnings[0]
	if got.Kind != event.Notice || got.Level != event.LevelWarn || got.Audience != event.NoticeAudienceOperator || !strings.Contains(got.Detail, "not recognised") {
		t.Fatalf("notice = %+v, want an operator warning carrying the shell detail", got)
	}
}
