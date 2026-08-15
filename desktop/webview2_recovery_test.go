package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestWebView2RecoveryStateRequiresReadyAfterReload(t *testing.T) {
	now := time.Date(2026, 8, 14, 1, 2, 3, 0, time.UTC)
	state, action := (webView2RecoveryState{}).nativeFailure(webView2NativeEvent{
		Kind:     1,
		Recovery: "reload_navigation_succeeded",
	}, now)
	if action != webView2RecoveryWaitForReady || state.Phase != webView2RecoveryAwaitingReady {
		t.Fatalf("reload action=%q state=%+v", action, state)
	}
	if got, want := state.Deadline, now.Add(webView2ReadyTimeout); !got.Equal(want) {
		t.Fatalf("deadline=%v want %v", got, want)
	}

	state, action = state.ready(now.Add(3 * time.Second))
	if action != webView2RecoveryMarkRecovered || state.Phase != webView2RecoveryIdle {
		t.Fatalf("ready action=%q state=%+v", action, state)
	}
}

func TestWebView2RecoveryStateEscalatesTimeoutAndRepeatedFailure(t *testing.T) {
	now := time.Date(2026, 8, 14, 1, 2, 3, 0, time.UTC)
	state, _ := (webView2RecoveryState{}).nativeFailure(webView2NativeEvent{
		Kind:     2,
		Recovery: "reload_navigation_succeeded",
	}, now)
	state, action := state.tick(now.Add(webView2ReadyTimeout))
	if action != webView2RecoveryRestart || state.Reason != "renderer_ready_timeout" {
		t.Fatalf("timeout action=%q state=%+v", action, state)
	}

	state, action = (webView2RecoveryState{}).nativeFailure(webView2NativeEvent{
		Kind:     1,
		Recovery: "reload_suppressed",
	}, now)
	if action != webView2RecoveryRestart || state.Phase != webView2RecoveryRestarting {
		t.Fatalf("repeat action=%q state=%+v", action, state)
	}
}

func TestWebView2RecoveryStateSeparatesBrowserAndAuxiliaryFailures(t *testing.T) {
	now := time.Date(2026, 8, 14, 1, 2, 3, 0, time.UTC)
	_, action := (webView2RecoveryState{}).nativeFailure(webView2NativeEvent{Kind: 0}, now)
	if action != webView2RecoveryRestart {
		t.Fatalf("browser action=%q", action)
	}
	for _, kind := range []int{3, 4, 5, 6, 7, 8, 9} {
		state, action := (webView2RecoveryState{}).nativeFailure(webView2NativeEvent{Kind: kind}, now)
		if action != webView2RecoveryNoAction || state.Phase != "" {
			t.Fatalf("aux kind=%d action=%q state=%+v", kind, action, state)
		}
	}
}

func TestWebView2RecoveryJournalGuardsRestartLoop(t *testing.T) {
	now := time.Date(2026, 8, 14, 1, 2, 3, 0, time.UTC)
	journal := webView2RecoveryJournal{
		path: filepath.Join(t.TempDir(), "webview2-recovery.jsonl"),
		now:  func() time.Time { return now },
	}
	if allowed, err := journal.automaticRestartAllowed(now); err != nil || !allowed {
		t.Fatalf("fresh guard allowed=%v err=%v", allowed, err)
	}
	if err := journal.append("auto_restart", "renderer"); err != nil {
		t.Fatal(err)
	}
	if allowed, err := journal.automaticRestartAllowed(now.Add(4 * time.Minute)); err != nil || allowed {
		t.Fatalf("guard allowed=%v err=%v", allowed, err)
	}
	if allowed, err := journal.automaticRestartAllowed(now.Add(5 * time.Minute)); err != nil || !allowed {
		t.Fatalf("expired guard allowed=%v err=%v", allowed, err)
	}
}

func TestWebView2RecoveryJournalShowsBlockedGuidanceOnce(t *testing.T) {
	now := time.Date(2026, 8, 14, 1, 2, 3, 0, time.UTC)
	journal := webView2RecoveryJournal{
		path: filepath.Join(t.TempDir(), "webview2-recovery.jsonl"),
		now:  func() time.Time { return now },
	}
	if err := journal.append("restart_blocked", "renderer"); err != nil {
		t.Fatal(err)
	}
	if pending, err := journal.pendingGuidance(now.Add(time.Hour)); err != nil || !pending {
		t.Fatalf("pending=%v err=%v", pending, err)
	}
	if err := journal.append("guidance_shown", "guard"); err != nil {
		t.Fatal(err)
	}
	if pending, err := journal.pendingGuidance(now.Add(time.Hour)); err != nil || pending {
		t.Fatalf("pending after shown=%v err=%v", pending, err)
	}
}
