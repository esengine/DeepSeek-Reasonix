package main

import (
	"encoding/json"
	"reflect"
	"strings"
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
		{"ListDirForTab", app.ListDirForTab("missing", "")},
		{"SearchFileRefsForTab", app.SearchFileRefsForTab("missing", "file")},
		{"ListTabs", app.ListTabs()},
		{"ListProjectTree", app.ListProjectTree()},
		{"AvailableSubagentTools", app.AvailableSubagentTools()},
		{"AutoResearchList", app.AutoResearchList("missing")},
		{"AutoResearchFindings", app.AutoResearchFindings("missing", 10)},
		{"MCPServers", app.MCPServers()},
		{"Plugins", app.Plugins()},
		{"HeartbeatListTasks", app.HeartbeatListTasks()},
		{"HeartbeatReloadTasks", app.HeartbeatReloadTasks()},
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
	if got := app.Settings(); got.Providers == nil || got.OfficialProviders == nil || got.ProviderPresets == nil || got.ProviderKinds == nil ||
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

	boundPayloads := []struct {
		name string
		got  any
	}{
		{"CapabilityDiagnostics", app.CapabilityDiagnostics(false)},
		{"Capabilities", app.Capabilities()},
		{"SkillsSettings", app.SkillsSettings()},
		{"HistoryPage", app.HistoryPage(0, 20)},
		{"Effort", app.Effort()},
		{"Memory", app.Memory()},
		{"MemorySuggestions", app.MemorySuggestions()},
	}
	for _, tc := range boundPayloads {
		assertRequiredJSONSlicesNonNil(t, tc.name, reflect.ValueOf(tc.got))
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

func assertRequiredJSONSlicesNonNil(t *testing.T, path string, value reflect.Value) {
	t.Helper()
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}

	switch value.Kind() {
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			t.Fatalf("%s is a nil slice; JSON contract requires []", path)
		}
		for i := 0; i < value.Len(); i++ {
			assertRequiredJSONSlicesNonNil(t, path, value.Index(i))
		}
	case reflect.Struct:
		typ := value.Type()
		for i := 0; i < value.NumField(); i++ {
			fieldType := typ.Field(i)
			if fieldType.PkgPath != "" {
				continue
			}
			jsonTag := fieldType.Tag.Get("json")
			if jsonTag == "-" || strings.Contains(jsonTag, ",omitempty") {
				continue
			}
			fieldPath := path + "." + fieldType.Name
			assertRequiredJSONSlicesNonNil(t, fieldPath, value.Field(i))
		}
	}
}
