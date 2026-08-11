package boot

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/tool"
)

// browserToolFixture mirrors the desktop pair shape: two fixed-schema host
// tools under the "browser" source.
func browserToolFixture() []tool.HostTool {
	return []tool.HostTool{
		{
			Name:         "browser_read",
			Description:  "read-only browser access",
			Schema:       json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["list_tabs","snapshot"]}},"required":["action"]}`),
			ReadOnly:     true,
			PlanModeSafe: true,
			HostMutation: true,
			Source:       "browser",
			Execute: func(context.Context, json.RawMessage) (string, error) {
				return "read ok", nil
			},
		},
		{
			Name:    "browser_act",
			Schema:  json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["click"]}},"required":["action"]}`),
			Source:  "browser",
			Execute: func(context.Context, json.RawMessage) (string, error) { return "act ok", nil },
		},
	}
}

func buildWithHostTools(t *testing.T, mode string, tools []tool.HostTool) map[string]tool.ContractEntry {
	t.Helper()
	ctrl, err := Build(context.Background(), Options{
		SessionDir: filepath.Join(t.TempDir(), "sessions"),
		TokenMode:  mode,
		Sink:       event.Discard,
		HostTools:  tools,
	})
	if err != nil {
		t.Fatalf("Build(%s): %v", mode, err)
	}
	defer ctrl.Close()
	entries := map[string]tool.ContractEntry{}
	for _, e := range ctrl.ToolContractEntries() {
		entries[e.Name] = e
	}
	return entries
}

// TestHostToolsInstalledInFullMode: Full/Delivery install the host tools with
// their fixed schemas and read-only flags in stable alphabetical order.
func TestHostToolsInstalledInFullMode(t *testing.T) {
	isolateConfigHome(t)
	t.Chdir(robustTempDir(t))

	for _, mode := range []string{TokenModeFull, TokenModeDelivery} {
		entries := buildWithHostTools(t, mode, browserToolFixture())
		read, ok := entries["browser_read"]
		if !ok {
			t.Fatalf("%s: browser_read missing from tool surface", mode)
		}
		if !read.ReadOnly {
			t.Fatalf("%s: browser_read must be read-only", mode)
		}
		act, ok := entries["browser_act"]
		if !ok {
			t.Fatalf("%s: browser_act missing from tool surface", mode)
		}
		if act.ReadOnly {
			t.Fatalf("%s: browser_act must be a writer", mode)
		}
		var wantSchema, gotSchema any
		if err := json.Unmarshal([]byte(`{"type":"object","properties":{"action":{"type":"string","enum":["list_tabs","snapshot"]}},"required":["action"]}`), &wantSchema); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(read.Schema, &gotSchema); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(wantSchema, gotSchema) {
			t.Fatalf("%s: browser_read schema changed: %s", mode, read.Schema)
		}
	}
}

// TestHostToolOrderStable: the provider-visible tool list stays sorted, and
// the browser tools sit in their deterministic alphabetical positions.
func TestHostToolOrderStable(t *testing.T) {
	isolateConfigHome(t)
	t.Chdir(robustTempDir(t))

	ctrl, err := Build(context.Background(), Options{
		SessionDir: filepath.Join(t.TempDir(), "sessions"),
		TokenMode:  TokenModeFull,
		Sink:       event.Discard,
		HostTools:  browserToolFixture(),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	names := []string{}
	for _, e := range ctrl.ToolContractEntries() {
		names = append(names, e.Name)
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("tool list is not sorted: %v", names)
	}
	// Pin the deterministic neighborhood: alphabetical sort puts both browser
	// tools between "bash" and "edit_file".
	idxRead, idxAct, idxBash, idxEdit := -1, -1, -1, -1
	for i, n := range names {
		switch n {
		case "browser_read":
			idxRead = i
		case "browser_act":
			idxAct = i
		case "bash":
			idxBash = i
		case "edit_file":
			idxEdit = i
		}
	}
	if idxRead < 0 || idxAct < 0 {
		t.Fatalf("browser tools missing from ordered list")
	}
	if idxAct != idxRead-1 {
		t.Fatalf("browser tool order: browser_act at %d, browser_read at %d", idxAct, idxRead)
	}
	if !(idxBash < idxAct && idxRead < idxEdit) {
		t.Fatalf("browser tools must sort between bash(%d) and edit_file(%d), got act=%d read=%d", idxBash, idxEdit, idxAct, idxRead)
	}
}

// TestHostToolsAbsentInEconomyUntilConnected: Economy keeps the browser tools
// out of the default surface; connect_tool_source("browser") installs them.
func TestHostToolsAbsentInEconomyUntilConnected(t *testing.T) {
	isolateConfigHome(t)
	t.Chdir(robustTempDir(t))

	entries := buildWithHostTools(t, TokenModeEconomy, browserToolFixture())
	if _, ok := entries["browser_read"]; ok {
		t.Fatal("economy mode must not install browser tools up front")
	}
	if _, ok := entries["connect_tool_source"]; !ok {
		t.Fatal("economy mode must keep connect_tool_source")
	}
}

// TestToolSourceConnectorBrowserSource: the connector routes source=browser to
// the host-tool installer and advertises it as available.
func TestToolSourceConnectorBrowserSource(t *testing.T) {
	called := false
	conn := &toolSourceConnector{
		browser: func(context.Context) (string, error) {
			called = true
			return "enabled browser_read, browser_act.", nil
		},
	}
	out, err := conn.Execute(context.Background(), json.RawMessage(`{"source":"browser"}`))
	if err != nil || !called {
		t.Fatalf("execute: %q %v (called=%v)", out, err, called)
	}
	if !strings.Contains(out, "browser_read") {
		t.Fatalf("output: %q", out)
	}
	// The source must be advertised in the availability list and the schema.
	available := conn.availableSources()
	found := false
	for _, s := range available {
		if s == "browser" {
			found = true
		}
	}
	if !found {
		t.Fatalf("browser source not advertised: %v", available)
	}
	if !strings.Contains(string(conn.Schema()), "browser") {
		t.Fatalf("connector schema must document the browser source: %s", conn.Schema())
	}
}
