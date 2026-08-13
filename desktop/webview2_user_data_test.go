package main

import (
	"path/filepath"
	"testing"
)

func TestWebview2UserDataPathKeepsDefaultForOrdinaryInstall(t *testing.T) {
	clearRuntimeDirEnv(t)

	if got := webview2UserDataPath(); got != "" {
		t.Fatalf("webview2UserDataPath() = %q, want empty Wails default", got)
	}
}

func TestWebview2UserDataPathUsesReasonixCacheForHomeIsolation(t *testing.T) {
	clearRuntimeDirEnv(t)
	home := filepath.Join(t.TempDir(), "reasonix-home")
	t.Setenv(reasonixHomeEnv, home)

	want := filepath.Join(home, "cache", "webview2")
	if got := webview2UserDataPath(); filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("webview2UserDataPath() = %q, want %q", got, want)
	}
}

func TestWebview2UserDataPathPrefersExplicitCacheIsolation(t *testing.T) {
	clearRuntimeDirEnv(t)
	home := filepath.Join(t.TempDir(), "reasonix-home")
	cache := filepath.Join(t.TempDir(), "reasonix-cache")
	t.Setenv(reasonixHomeEnv, home)
	t.Setenv(reasonixCacheHomeEnv, cache)

	want := filepath.Join(cache, "webview2")
	if got := webview2UserDataPath(); filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("webview2UserDataPath() = %q, want %q", got, want)
	}
}

func TestWebview2UserDataPathUsesStateForStateOnlyIsolation(t *testing.T) {
	clearRuntimeDirEnv(t)
	state := filepath.Join(t.TempDir(), "reasonix-state")
	t.Setenv(reasonixStateHomeEnv, state)

	want := filepath.Join(state, "webview2")
	if got := webview2UserDataPath(); filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("webview2UserDataPath() = %q, want %q", got, want)
	}
}

func clearRuntimeDirEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{reasonixHomeEnv, reasonixStateHomeEnv, reasonixCacheHomeEnv} {
		t.Setenv(name, "")
	}
}
