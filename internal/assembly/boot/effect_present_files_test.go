package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
	"reasonix/internal/tools/builtin"
)

func TestEffectPresentFilesSurvivesTheRealToolPipeline(t *testing.T) {
	for _, proxy := range []bool{false, true} {
		name := "direct"
		if proxy {
			name = "capability"
		}
		for _, mode := range []string{"default", "off", "on", "forbidden", "disabled", "excluded"} {
			t.Run(name+"/"+mode, func(t *testing.T) { presentFilesEffect(t, proxy, mode) })
		}
	}
}

func presentFilesEffect(t *testing.T, proxy bool, mode string) {
	t.Helper()
	home := filepath.Join(isolateConfigHome(t), ".reasonix")
	t.Setenv("REASONIX_HOME", home)
	settings := "[tools]\npresent_files = true\n"
	switch mode {
	case "default":
		settings = ""
	case "off":
		settings = "[tools]\npresent_files = false\n"
	case "forbidden":
		settings += "[sandbox]\nforbid_read = [\"report.md\"]\n"
	case "disabled":
		settings += "disabled = [\"present_files\"]\n"
	case "excluded":
		settings += "enabled = [\"read_file\"]\n"
	}
	writeFile(t, home, "config.toml", settings)
	delivers := mode == "on" || mode == "default"
	enabled := delivers || mode == "forbidden"
	dir := robustTempDir(t)
	t.Chdir(dir)
	registerBootTokenProfileTestProvider()
	call := provider.ToolCall{ID: "present", Name: "present_files", Arguments: `{"files":[{"path":"report.md","description":"Report"}]}`}
	if proxy {
		call.Name = "use_capability"
		call.Arguments = `{"action":"call","capability_id":"tool:present_files","arguments":{"files":[{"path":"report.md","description":"Report"}]}}`
	}
	prov := testutil.NewMock("present-files",
		testutil.Turn{ToolCalls: []provider.ToolCall{call}},
		testutil.Turn{Text: "Done."},
	)
	setBootTokenProfileTestProvider(t, prov)
	writeFile(t, dir, "reasonix.toml", "default_model = \"test-model\"\n[[providers]]\nname = \"test-model\"\nkind = \"boot-token-profile-test\"\nmodel = \"x\"\n")
	writeFile(t, dir, "report.md", "# Report")
	approveWorkspace(t, dir)
	events := make(chan event.Event, 1000)
	ctrl, err := Build(context.Background(), Options{WorkspaceRoot: dir, Sink: event.FuncSink(func(e event.Event) { events <- e })})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	want := builtin.Workspace{}.PresentFiles()
	count := 0
	for _, entry := range ctrl.AllToolContractEntries() {
		if entry.Name == want.Name() {
			count++
		}
	}
	if (count == 1) != enabled || count > 1 {
		t.Fatalf("registry contribution = %d, enabled = %v", count, enabled)
	}
	if err := ctrl.NewSession(); err != nil {
		t.Fatal(err)
	}
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := ctrl.Run(ctx, "Present the existing report.md file."); err != nil {
		t.Fatal(err)
	}
	found := false
	for len(events) > 0 {
		e := <-events
		if e.Kind == event.ToolResult && e.Tool.ID == "present" {
			wire := eventwire.ToWire(e)
			if wire.Tool == nil {
				t.Fatal("tool result missing wire payload")
			}
			if !delivers {
				if e.Tool.Err == "" || len(wire.Tool.Deliveries) != 0 {
					t.Fatalf("unavailable/forbidden call produced delivery: %+v", e.Tool)
				}
			} else if len(wire.Tool.Deliveries) != 1 {
				t.Fatalf("delivery did not reach UI: %+v, output %q", e.Tool, e.Tool.Output)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("present tool result missing")
	}
	found = false
	for _, message := range ctrl.History() {
		for _, call := range message.ToolCalls {
			if call.ID == "present" && call.CapabilityID == "tool:present_files" {
				found = true
			}
		}
	}
	if delivers && !found {
		t.Fatal("host-confirmed identity was not persisted")
	}
	requests := prov.Requests()
	if len(requests) < 2 || !reflect.DeepEqual(requests[0].Tools, requests[1].Tools) {
		t.Fatal("delivery changed the tool schema within the turn")
	}
	if !enabled {
		first, err := json.Marshal(requests[0])
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(first, []byte(want.Name())) {
			t.Fatal("disabled presenter leaked into the first provider request")
		}
	}
	count = 0
	for _, schema := range requests[0].Tools {
		if schema.Name != want.Name() {
			continue
		}
		count++
		var gotSchema, wantSchema any
		if err := json.Unmarshal(schema.Parameters, &gotSchema); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(provider.CanonicalizeSchema(want.Schema()), &wantSchema); err != nil {
			t.Fatal(err)
		}
		if schema.Description != want.Description() || !reflect.DeepEqual(gotSchema, wantSchema) {
			t.Fatalf("unexpected provider contribution: %+v", schema)
		}
	}
	if (count == 1) != enabled || count > 1 {
		t.Fatalf("provider contribution = %d, enabled = %v", count, enabled)
	}
}
