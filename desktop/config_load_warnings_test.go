package main

import (
	"context"
	"testing"
	"time"
)

func TestEmitConfigLoadWarningsRequiresContextAndOwnsPayload(t *testing.T) {
	if (&App{}).emitConfigLoadWarnings(1, []string{"warning"}) {
		t.Fatal("handler accepted warnings without a Wails context")
	}
	if (&App{ctx: context.Background()}).emitConfigLoadWarnings(1, nil) {
		t.Fatal("handler accepted an empty warning list")
	}

	type emittedEvent struct {
		name     string
		warnings []string
		revision uint64
	}
	started := make(chan struct{})
	release := make(chan struct{})
	events := make(chan emittedEvent, 1)
	app := &App{ctx: context.Background()}
	app.runtimeEvents.emit = func(_ context.Context, name string, payload ...any) {
		close(started)
		<-release
		warnings, _ := payload[0].([]string)
		revision, _ := payload[1].(uint64)
		events <- emittedEvent{name: name, warnings: warnings, revision: revision}
	}

	warnings := []string{"user config is invalid"}
	if !app.emitConfigLoadWarnings(42, warnings) {
		t.Fatal("handler rejected warnings with a Wails context")
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("runtime event emitter did not start")
	}
	warnings[0] = "mutated by caller"
	close(release)

	select {
	case got := <-events:
		if got.name != configLoadWarningsEvent {
			t.Fatalf("event name = %q, want %q", got.name, configLoadWarningsEvent)
		}
		if got.revision != 42 {
			t.Fatalf("event revision = %d, want 42", got.revision)
		}
		if len(got.warnings) != 1 || got.warnings[0] != "user config is invalid" {
			t.Fatalf("event warnings = %v, want owned original payload", got.warnings)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runtime event was not delivered")
	}
}

func TestConfigLoadWarningRevisionsFenceEventsStartedBeforeReload(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	app := &App{ctx: context.Background()}
	type warningEvent struct {
		warnings []string
		revision uint64
	}
	events := make(chan warningEvent, 2)
	app.runtimeEvents.emit = func(_ context.Context, _ string, payload ...any) {
		warnings, _ := payload[0].([]string)
		revision, _ := payload[1].(uint64)
		events <- warningEvent{warnings: warnings, revision: revision}
	}

	oldBuildHandler := app.configLoadWarningsHandler()
	reloaded, err := app.ReloadUserConfig()
	if err != nil {
		t.Fatalf("ReloadUserConfig: %v", err)
	}
	if reloaded.ConfigWarningsRevision == 0 {
		t.Fatal("reload did not return a config warning revision")
	}
	if !oldBuildHandler([]string{"stale warning"}) {
		t.Fatal("old build handler rejected warning")
	}
	newBuildHandler := app.configLoadWarningsHandler()
	if !newBuildHandler([]string{"new warning"}) {
		t.Fatal("new build handler rejected warning")
	}

	receive := func() warningEvent {
		t.Helper()
		select {
		case payload := <-events:
			return payload
		case <-time.After(5 * time.Second):
			t.Fatal("runtime warning event was not delivered")
			return warningEvent{}
		}
	}
	oldPayload := receive()
	newPayload := receive()
	if len(oldPayload.warnings) != 1 || oldPayload.revision >= reloaded.ConfigWarningsRevision {
		t.Fatalf("old build event = %+v, reload barrier = %d", oldPayload, reloaded.ConfigWarningsRevision)
	}
	if len(newPayload.warnings) != 1 || newPayload.revision <= reloaded.ConfigWarningsRevision {
		t.Fatalf("new build event = %+v, reload barrier = %d", newPayload, reloaded.ConfigWarningsRevision)
	}
}
