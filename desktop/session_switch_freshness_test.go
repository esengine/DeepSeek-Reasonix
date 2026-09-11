package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/transcript"
)

func TestPreloadedPageDoesNotEraseNewlyCommittedTurn(t *testing.T) {
	isolateDesktopUserDirs(t)
	path := filepath.Join(t.TempDir(), "session.jsonl")
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	executor := agent.New(nil, nil, session, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: executor, SessionDir: filepath.Dir(path), SessionPath: path, Sink: event.Discard})
	defer ctrl.Close()
	// A same-session or detached-runtime switch reads before the owner settles.
	preloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	session.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "completed while switching"})
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	if ctrl.RuntimeStatus().Running || ctrl.SessionHasUnsavedChanges() {
		t.Fatal("fixture must be idle and fully persisted")
	}
	tab := &WorkspaceTab{ID: "tab", Ctrl: ctrl, SessionPath: path}
	page, reread := historyPageForController(tab, ctrl, preloaded, path, 0, defaultHistoryPageTurns)
	if !reread {
		t.Fatal("obsolete preload must be refreshed before installation")
	}
	current, _ := historyPageForController(tab, ctrl, nil, "", 0, defaultHistoryPageTurns)
	if current.TotalTurns != 2 {
		t.Fatalf("invalid current history: %d", current.TotalTurns)
	}
	if page.TotalTurns != current.TotalTurns {
		t.Fatalf("reused an obsolete preload: switch=%d current=%d", page.TotalTurns, current.TotalTurns)
	}
}

func TestTranscriptSwitchReportsActualLoadWithoutLegacyPage(t *testing.T) {
	for _, channel := range []bool{false, true} {
		t.Run(map[bool]string{false: "resume", true: "channel"}[channel], func(t *testing.T) {
			app, tab, _, _, _, _ := newAtomicRebindTestApp(t)
			path := resumedCleanTarget(t, app, tab, "snapshot-target.jsonl")
			var phases HistorySwitchPhases
			var err error
			if channel {
				phases, err = app.OpenChannelTranscriptSessionForTab(tab.ID, path)
			} else {
				phases, err = app.ResumeTranscriptSessionForTab(tab.ID, path)
			}
			if err != nil {
				t.Fatal(err)
			}
			if phases.Outcome != "ok" || phases.DurableReads != 1 || phases.LoadedCount == 0 || phases.LoadedBytes == 0 {
				t.Fatalf("missing load evidence: %+v", phases)
			}
			if phases.HistoryCount != 0 || phases.HistoryMs != 0 {
				t.Fatalf("snapshot adoption built a legacy page: %+v", phases)
			}
			snapshot, err := app.TranscriptSnapshotForTab(tab.ID, transcript.PageRequest{})
			if err != nil || snapshot.Identity.SessionID != agent.BranchID(path) || len(snapshot.Records) == 0 {
				t.Fatalf("snapshot did not adopt target: %v", err)
			}
			if tab.ReadOnly != channel {
				t.Fatal("switch changed channel write policy")
			}
		})
	}
}

func TestLargeTranscriptSwitchPhaseMeasurement(t *testing.T) {
	app, tab, _, _, _, _ := newAtomicRebindTestApp(t)
	path := resumedCleanTarget(t, app, tab, "large-snapshot-target.jsonl")
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	for range 500 {
		loaded.Add(provider.Message{Role: provider.RoleUser, Content: "question"})
		loaded.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("x", 10<<10)})
	}
	if err := loaded.Save(path); err != nil {
		t.Fatal(err)
	}
	phases, err := app.ResumeTranscriptSessionForTab(tab.ID, path)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	snapshot, err := app.TranscriptSnapshotForTab(tab.ID, transcript.PageRequest{})
	if err != nil || !snapshot.HasOlder {
		t.Fatalf("large snapshot: %v", err)
	}
	if phases.DurableReads != 1 || phases.HistoryCount != 0 {
		t.Fatalf("large switch repeated history work: %+v", phases)
	}
	t.Logf("loaded_bytes=%d loaded_messages=%d load_ms=%d rebind_ms=%d snapshot_ms=%d records=%d durable_reads=%d", phases.LoadedBytes, phases.LoadedCount, phases.LoadMs, phases.RebindMs, time.Since(started).Milliseconds(), len(snapshot.Records), phases.DurableReads)
}
