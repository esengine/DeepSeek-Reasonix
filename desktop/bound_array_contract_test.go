package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestBoundArrayPayloadsAreNonNilBeforeStartup(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	cases := []struct {
		name string
		got  any
	}{
		{"Checkpoints", app.Checkpoints()},
		{"ListSessions", app.ListSessions()},
		{"ListTrashedSessions", app.ListTrashedSessions()},
		{"ListWorkspaces", app.ListWorkspaces()},
		{"History", app.History()},
		{"Jobs", app.Jobs()},
		{"Commands", app.Commands()},
		{"Models", app.Models()},
		{"ListDir", app.ListDir("__missing__")},
		{"ListTabs", app.ListTabs()},
		{"ListProjectTree", app.ListProjectTree()},
	}
	for _, tc := range cases {
		assertNonNilSliceJSON(t, tc.name, tc.got)
	}

	if got := app.SlashArgs("/skill "); got.Items == nil {
		t.Fatal("SlashArgs().Items is nil; frontend expects []")
	}
	if got := app.WorkspaceChanges(""); got.Files == nil {
		t.Fatal(`WorkspaceChanges("").Files is nil; frontend expects []`)
	}
	if got := app.ContextPanel("missing"); got.ReadFiles == nil || got.ChangedFiles == nil {
		t.Fatalf("ContextPanel(missing) arrays = read:%v changed:%v, want non-nil", got.ReadFiles, got.ChangedFiles)
	}
	if got := app.HooksSettings("global"); got.Hooks == nil || got.Events == nil {
		t.Fatalf("HooksSettings(global) arrays = hooks:%v events:%v, want non-nil", got.Hooks, got.Events)
	}
	if got := app.Settings(); got.Providers == nil || got.OfficialProviders == nil || got.ProviderKinds == nil ||
		got.Permissions.Allow == nil || got.Permissions.Ask == nil || got.Permissions.Deny == nil ||
		got.Sandbox.AllowWrite == nil || got.Sandbox.EffectiveWriteRoots == nil ||
		got.Bot.Allowlist.QQUsers == nil || got.Bot.Allowlist.FeishuUsers == nil || got.Bot.Allowlist.WeixinUsers == nil ||
		got.Bot.Allowlist.QQGroups == nil || got.Bot.Allowlist.FeishuGroups == nil || got.Bot.Allowlist.WeixinGroups == nil {
		t.Fatalf("Settings() contains nil array fields: %+v", got)
	}
	if got := app.DesktopStartupSettings(); got.StatusBarItems == nil ||
		got.Bot.Allowlist.QQUsers == nil || got.Bot.Allowlist.FeishuUsers == nil || got.Bot.Allowlist.WeixinUsers == nil ||
		got.Bot.Allowlist.QQGroups == nil || got.Bot.Allowlist.FeishuGroups == nil || got.Bot.Allowlist.WeixinGroups == nil {
		t.Fatalf("DesktopStartupSettings() contains nil array fields: %+v", got)
	}
}

func assertNonNilSliceJSON(t *testing.T, name string, got any) {
	t.Helper()
	v := reflect.ValueOf(got)
	if v.Kind() != reflect.Slice {
		t.Fatalf("%s returned %T, want slice", name, got)
	}
	if v.IsNil() {
		t.Fatalf("%s returned nil slice; frontend expects []", name)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("%s JSON marshal: %v", name, err)
	}
	if string(raw) == "null" {
		t.Fatalf("%s JSON encoded as null; frontend expects []", name)
	}
}
