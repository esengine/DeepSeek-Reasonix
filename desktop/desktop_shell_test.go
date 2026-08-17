package main

import (
	"os"
	"reflect"
	"testing"
	"time"
)

func TestDesktopPresentPlanUsesGtkPresentOnlyOnLinux(t *testing.T) {
	if got, want := desktopPresentPlanFor("linux", true), []desktopPresentAction{desktopPresentUnminimise}; !reflect.DeepEqual(got, want) {
		t.Fatalf("linux present actions = %v, want %v", got, want)
	}
	if got, want := desktopPresentPlanFor("linux", false), []desktopPresentAction{desktopPresentUnminimise}; !reflect.DeepEqual(got, want) {
		t.Fatalf("linux normal present actions = %v, want %v", got, want)
	}
}

func TestDesktopPresentPlanPreservesWindowsAndMacOrdering(t *testing.T) {
	tests := []struct {
		name      string
		goos      string
		maximised bool
		want      []desktopPresentAction
	}{
		{name: "windows maximised", goos: "windows", maximised: true, want: []desktopPresentAction{desktopPresentMaximise, desktopPresentWindowShow}},
		{name: "windows normal", goos: "windows", want: []desktopPresentAction{desktopPresentWindowShow, desktopPresentUnminimise}},
		{name: "mac", goos: "darwin", maximised: true, want: []desktopPresentAction{desktopPresentApplicationShow, desktopPresentWindowShow, desktopPresentUnminimise}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := desktopPresentPlanFor(tt.goos, tt.maximised); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("actions = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDesktopShellFrontendReadyIsSeparateFromDOMReady(t *testing.T) {
	shell := newDesktopShellCoordinator(&App{})
	shell.markDOMReady()
	if shell.frontendReady {
		t.Fatal("DOM readiness must not imply frontend readiness")
	}
	first, healthy := shell.markFrontendHeartbeat(time.Unix(10, 0))
	if !first || healthy {
		t.Fatal("first bridge heartbeat should transition frontend readiness")
	}
	if first, healthy = shell.markFrontendHeartbeat(time.Unix(11, 0)); first || healthy {
		t.Fatal("an immediate later heartbeat must not commit stable health")
	}
	if first, healthy = shell.markFrontendHeartbeat(time.Unix(13, 0)); first || !healthy {
		t.Fatal("a later heartbeat should commit stable health once")
	}
	if first, healthy = shell.markFrontendHeartbeat(time.Unix(16, 0)); first || healthy {
		t.Fatal("later heartbeats must not recommit process health")
	}
}

func TestDesktopShellHeartbeatPreservesVisiblePhase(t *testing.T) {
	shell := newDesktopShellCoordinator(&App{})
	shell.mu.Lock()
	shell.presented = true
	shell.phase = desktopShellVisible
	shell.mu.Unlock()

	shell.markDOMReady()
	shell.markFrontendHeartbeat(time.Unix(10, 0))
	shell.mu.Lock()
	defer shell.mu.Unlock()
	if shell.phase != desktopShellVisible {
		t.Fatalf("visible shell phase = %q, want %q", shell.phase, desktopShellVisible)
	}
}

func TestTrayLossWhileBackgroundHiddenRequestsPresentation(t *testing.T) {
	app := NewApp()
	called := make(chan string, 1)
	app.desktopShell.coordinator.presentOverride = func(source string) { called <- source }
	app.desktopShell.coordinator.backgroundHidden = true
	tray := newDesktopTray()
	app.mu.Lock()
	app.tray = tray
	app.trayReady = true
	app.desktopShell.trayState = "ready"
	app.mu.Unlock()

	app.setTrayHealth(tray, "unavailable", "no_host")
	select {
	case source := <-called:
		if source != "tray_unavailable" {
			t.Fatalf("presentation source = %q", source)
		}
	case <-time.After(time.Second):
		t.Fatal("tray loss did not request presentation")
	}
	status := app.GetDesktopShellStatus()
	if status.TrayState != "unavailable" || status.Reason != "no_host" {
		t.Fatalf("shell status = %+v", status)
	}
}

func TestEvaluateStatusNotifierSnapshot(t *testing.T) {
	const item = "org.kde.StatusNotifierItem-42-1"
	tests := []struct {
		name   string
		state  statusNotifierSnapshot
		ready  bool
		reason string
	}{
		{name: "no watcher", reason: "no_watcher"},
		{name: "no host", state: statusNotifierSnapshot{WatcherOwner: ":1.2"}, reason: "no_host"},
		{name: "item has no owner", state: statusNotifierSnapshot{WatcherOwner: ":1.2", Host: true}, reason: "item_no_owner"},
		{name: "item absent", state: statusNotifierSnapshot{WatcherOwner: ":1.2", Host: true, ItemOwner: ":1.9"}, reason: "item_not_registered"},
		{name: "well-known registered", state: statusNotifierSnapshot{WatcherOwner: ":1.2", Host: true, ItemOwner: ":1.9", Items: []string{item + "/StatusNotifierItem"}}, ready: true},
		{name: "unique owner registered", state: statusNotifierSnapshot{WatcherOwner: ":1.2", Host: true, ItemOwner: ":1.9", Items: []string{":1.9/StatusNotifierItem"}}, ready: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ready, reason := evaluateStatusNotifierSnapshot(tt.state, item)
			if ready != tt.ready || reason != tt.reason {
				t.Fatalf("evaluate = (%v, %q), want (%v, %q)", ready, reason, tt.ready, tt.reason)
			}
		})
	}
}

func TestLinuxRendererCompatibilityRestartJournalPreventsLoop(t *testing.T) {
	oldVersion := version
	version = "journal-test"
	oldCompatibility := os.Getenv(linuxRendererCompatibilityEnv)
	_ = os.Unsetenv(linuxRendererCompatibilityEnv)
	t.Cleanup(func() {
		version = oldVersion
		_ = os.Setenv(linuxRendererCompatibilityEnv, oldCompatibility)
	})
	_ = os.Remove(linuxRendererRecoveryJournalPath())
	t.Cleanup(func() { _ = os.Remove(linuxRendererRecoveryJournalPath()) })
	now := time.Now()
	if !claimLinuxRendererCompatibilityRestart(now, "startup") {
		t.Fatal("first compatibility restart was not claimed")
	}
	if claimLinuxRendererCompatibilityRestart(now.Add(time.Minute), "startup") {
		t.Fatal("same-version restart loop was not blocked")
	}
	if !claimLinuxRendererCompatibilityRestart(now.Add(linuxRendererRecoveryWindow+time.Second), "startup") {
		t.Fatal("restart was not allowed after the five-minute guard window")
	}
}

func TestProcessEnvWithOverridesReplacesInsteadOfDuplicating(t *testing.T) {
	got := processEnvWithOverrides([]string{"A=old", "B=kept", "A=duplicate"}, map[string]string{"A": "new", "C": "added"})
	counts := map[string]int{}
	values := map[string]string{}
	for _, entry := range got {
		for _, key := range []string{"A", "B", "C"} {
			prefix := key + "="
			if len(entry) >= len(prefix) && entry[:len(prefix)] == prefix {
				counts[key]++
				values[key] = entry[len(prefix):]
			}
		}
	}
	if counts["A"] != 1 || values["A"] != "new" || values["B"] != "kept" || values["C"] != "added" {
		t.Fatalf("overridden environment = %v", got)
	}
}

func TestWebKitReloadNeedsPostReloadFrontendHeartbeat(t *testing.T) {
	app := NewApp()
	recovery := app.desktopShell.linuxRecovery
	recovery.nativeEvent(webKitNativeEvent{recovery: webKitRecoverySucceeded, generation: 7}, false)
	recovery.mu.Lock()
	pending := recovery.pendingEvent
	recovery.mu.Unlock()
	if pending == nil || pending.generation != 7 {
		t.Fatal("native load-finished was incorrectly treated as complete recovery")
	}

	recovery.frontendReady()
	recovery.mu.Lock()
	pending = recovery.pendingEvent
	recovery.mu.Unlock()
	if pending != nil {
		t.Fatal("post-reload frontend heartbeat did not complete recovery")
	}
}

func TestWebKitRecoveryStopClearsPendingHeartbeat(t *testing.T) {
	app := NewApp()
	recovery := app.desktopShell.linuxRecovery
	recovery.nativeEvent(webKitNativeEvent{recovery: webKitRecoverySucceeded, generation: 11}, false)
	recovery.stop()

	recovery.mu.Lock()
	defer recovery.mu.Unlock()
	if !recovery.stopped || recovery.pendingTimer != nil || recovery.pendingEvent != nil {
		t.Fatalf("stopped recovery retained pending work: %+v", recovery)
	}
}
