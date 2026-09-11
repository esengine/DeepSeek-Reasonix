package main

import (
	"context"
	"testing"

	"reasonix/internal/agent"
)

func TestModelSettingsCredentialRefreshPersistsSelectedConnection(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	for _, name := range []string{"connection-one", "connection-two"} {
		if _, err := app.SaveProviderWithKey(ProviderView{Name: name, Kind: "openai", BaseURL: "http://127.0.0.1:1/v1", Models: []string{"same-model"}}, "test-key"); err != nil {
			t.Fatal(err)
		}
	}
	app.ctx = context.Background()
	app.readyHook = func() {}
	tab := modelSettingsBootTab(t, app, "persisted-connection", t.TempDir(), "connection-one/same-model")
	app.activeTabID = tab.ID
	const want = "connection-two/same-model"
	if err := app.SetModelForTab(tab.ID, want); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SetConnectionKey("connection-two", "next-test-key"); err != nil {
		t.Fatal(err)
	}
	admission, current, err := app.beginTabTurn(tab.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	admission.abort()
	if got := current.ModelRef(); got != want {
		t.Fatalf("runtime model = %q, want %q", got, want)
	}
	if err := current.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if got, ok := agent.LoadSessionModel(current.SessionPath()); !ok || got != want {
		t.Fatalf("persisted model = %q, present=%v, want %q", got, ok, want)
	}
}
