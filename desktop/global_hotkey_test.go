package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseHotkeyBindingSpace(t *testing.T) {
	mods, key, err := parseHotkeyBinding("ctrl+shift+space")
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 2 {
		t.Fatalf("mods = %v", mods)
	}
	if key == 0 {
		t.Fatal("expected space key")
	}
}

func TestParseHotkeyBindingRequiresModifier(t *testing.T) {
	if _, _, err := parseHotkeyBinding("space"); err == nil {
		t.Fatal("expected error for bare key")
	}
}

func TestShouldShowOnGlobalHotkeyWhenBackgrounded(t *testing.T) {
	app := NewApp()
	t.Cleanup(func() { app.shutdown(context.Background()) })
	app.backgroundHidden.Store(true)
	if !app.shouldShowOnGlobalHotkey() {
		t.Fatal("expected show while background-hidden")
	}
	app.backgroundHidden.Store(false)
	// Frontmost probe is platform-specific; only assert the backgrounded branch.
}

func TestRebindGlobalHotkeyRegistersAndClears(t *testing.T) {
	app := NewApp()
	t.Cleanup(func() { app.shutdown(context.Background()) })

	var registered atomic.Int32
	var stopped atomic.Int32
	original := registerGlobalHotkeyBinding
	t.Cleanup(func() {
		registerGlobalHotkeyBinding = original
		app.stopGlobalHotkey()
	})
	registerGlobalHotkeyBinding = func(ctx context.Context, binding string, onTrigger func()) (func(), error) {
		registered.Add(1)
		done := make(chan struct{})
		go func() {
			<-ctx.Done()
			stopped.Add(1)
			close(done)
		}()
		if binding != "ctrl+shift+space" {
			t.Fatalf("binding = %q", binding)
		}
		if onTrigger == nil {
			t.Fatal("missing trigger")
		}
		return func() { <-done }, nil
	}

	if err := app.rebindGlobalHotkey("ctrl+shift+space"); err != nil {
		t.Fatal(err)
	}
	if registered.Load() != 1 {
		t.Fatalf("registered = %d", registered.Load())
	}
	if err := app.rebindGlobalHotkey(""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for stopped.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if stopped.Load() != 1 {
		t.Fatalf("stopped = %d", stopped.Load())
	}
}

func TestRebindGlobalHotkeyWaitsForUnregisterBeforeNextRegister(t *testing.T) {
	app := NewApp()
	t.Cleanup(func() { app.shutdown(context.Background()) })

	var order []string
	var mu sync.Mutex
	original := registerGlobalHotkeyBinding
	t.Cleanup(func() {
		registerGlobalHotkeyBinding = original
		app.stopGlobalHotkey()
	})
	registerGlobalHotkeyBinding = func(ctx context.Context, binding string, _ func()) (func(), error) {
		mu.Lock()
		order = append(order, "register:"+binding)
		mu.Unlock()
		done := make(chan struct{})
		go func() {
			<-ctx.Done()
			time.Sleep(30 * time.Millisecond)
			mu.Lock()
			order = append(order, "unregister:"+binding)
			mu.Unlock()
			close(done)
		}()
		return func() { <-done }, nil
	}

	if err := app.rebindGlobalHotkey("ctrl+shift+a"); err != nil {
		t.Fatal(err)
	}
	if err := app.rebindGlobalHotkey("ctrl+shift+b"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]string(nil), order...)
	mu.Unlock()
	want := []string{"register:ctrl+shift+a", "unregister:ctrl+shift+a", "register:ctrl+shift+b"}
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestEmitGlobalHotkeyErrorPersistsForSettings(t *testing.T) {
	clearLastGlobalHotkeyError()
	t.Cleanup(clearLastGlobalHotkeyError)
	app := NewApp()
	t.Cleanup(func() { app.shutdown(context.Background()) })
	app.emitGlobalHotkeyError(fmt.Errorf("grant Accessibility permission"))
	if got := lastGlobalHotkeyErrorMessage(); got != "grant Accessibility permission" {
		t.Fatalf("persisted error = %q", got)
	}
	if err := app.rebindGlobalHotkey(""); err != nil {
		t.Fatal(err)
	}
	if got := lastGlobalHotkeyErrorMessage(); got != "" {
		t.Fatalf("error should clear on disable, got %q", got)
	}
}
