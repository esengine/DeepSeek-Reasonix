package boot

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

const projectionDropConfig = `
default_model = "model-a"

[agent]
system_prompt = %q
compact_ratio = 0.5
recent_keep = 2

[[providers]]
name = "model-a"
kind = %q
model = "a"
context_window = 32000
`

func writeProjectionDropConfig(t *testing.T, dir, kind, systemPrompt string) {
	t.Helper()
	writeFile(t, dir, "reasonix.toml", fmt.Sprintf(projectionDropConfig, systemPrompt, kind))
}

// compactedSession builds a session that has folded once and snapshots it, so a
// second Build can adopt it the way a resume or a rebuild does.
func compactedSession(t *testing.T, rec *compactionEffectProvider, home, dir string) *control.Controller {
	t.Helper()
	from, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build from: %v", err)
	}
	t.Cleanup(func() { from.Close() })
	from.EnsureSessionPath()
	for _, prompt := range []string{"start the task", "second", "keep going", "keep going", "keep going", "keep going", "keep going"} {
		if err := from.Run(context.Background(), prompt); err != nil {
			t.Fatalf("Run(%q): %v", prompt, err)
		}
	}
	if _, carrying := digestRequests(rec.requests()); carrying == 0 {
		t.Fatal("fixture never compacted")
	}
	if err := from.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	return from
}

// adoptCollectingNotices rebuilds on the same transcript and returns the
// notices the adoption produced.
func adoptCollectingNotices(t *testing.T, from *control.Controller, home, dir string) []event.Event {
	t.Helper()
	var notices []event.Event
	to, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e)
		}
	})})
	if err != nil {
		t.Fatalf("Build to: %v", err)
	}
	t.Cleanup(func() { to.Close() })
	if err := ApplyRuntimeMigration(to, from, CaptureRuntimeMigration(from)); err != nil {
		t.Fatalf("ApplyRuntimeMigration: %v", err)
	}
	if from.History()[0].Content != to.History()[0].Content {
		t.Logf("system prompt changed during migration: %q", firstDivergence(from.History()[0].Content, to.History()[0].Content))
	}
	return notices
}

func foldNotReusedNotices(notices []event.Event) []event.Event {
	var out []event.Event
	for _, e := range notices {
		if e.Code == event.NoticeCodeFoldNotReusedHistory {
			out = append(out, e)
		}
	}
	return out
}

// The covered-prefix hash includes the system message, so a changed system
// prompt retires the projection. Pinned at boot.Build so the notice is not lost
// to a rebuild that re-binds the session.
func TestEffectSystemPromptChangeSaysTheFoldWasNotReused(t *testing.T) {
	t.Parallel()
	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	rec := &compactionEffectProvider{bulk: strings.Repeat("work output line with detail. ", 400)}
	const kind = "boot-projection-drop-edited"
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeProjectionDropConfig(t, dir, kind, "BASE")
	approveWorkspaceAt(t, home, dir)

	from := compactedSession(t, rec, home, dir)
	writeProjectionDropConfig(t, dir, kind, "BASE, edited")
	approveWorkspaceAt(t, home, dir)

	dropped := foldNotReusedNotices(adoptCollectingNotices(t, from, home, dir))
	if len(dropped) != 1 {
		t.Fatalf("got %d fold-not-reused notice(s), want exactly 1", len(dropped))
	}
	if dropped[0].Level != event.LevelWarn {
		t.Fatalf("level = %v, want warn", dropped[0].Level)
	}
}

// A rebuild that leaves the system prompt alone keeps the projection, so it has
// nothing to report: the notice must not become noise on every resume.
func TestEffectUnchangedSystemPromptKeepsTheProjectionQuietly(t *testing.T) {
	t.Parallel()
	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	rec := &compactionEffectProvider{bulk: strings.Repeat("work output line with detail. ", 400)}
	const kind = "boot-projection-drop-unchanged"
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeProjectionDropConfig(t, dir, kind, "BASE")
	approveWorkspaceAt(t, home, dir)

	from := compactedSession(t, rec, home, dir)
	if got := foldNotReusedNotices(adoptCollectingNotices(t, from, home, dir)); len(got) != 0 {
		t.Fatalf("got %d fold-not-reused notice(s) for an unchanged prompt, want none", len(got))
	}
}
