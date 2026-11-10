package boot

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/computer"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/safety/permission"
)

// fakeComputerHelperEnv makes the test binary answer as the native helper: one
// application with one text field, whose value a set_value changes.
const fakeComputerHelperEnv = "REASONIX_BOOT_FAKE_COMPUTER_HELPER"

func runFakeComputerHelper(in io.Reader, out io.Writer) {
	value := ""
	launched := false
	enc := json.NewEncoder(out)
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil || req.ID == 0 {
			continue
		}
		var result any = map[string]any{"serverInfo": map[string]any{"version": computer.Contract().Version}}
		if req.Method == "tools/list" {
			tools := []any{}
			for _, name := range []string{"list_apps", "list_windows", "get_window_state", "set_value", "launch_app"} {
				tools = append(tools, map[string]any{"name": name, "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"delivery_mode": map[string]any{"type": "string"}}}})
			}
			result = map[string]any{"tools": tools}
		}
		if req.Method == "tools/call" {
			if path := os.Getenv("REASONIX_BOOT_CUA_REQUEST_LOG"); path != "" {
				f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
				if err == nil {
					_ = json.NewEncoder(f).Encode(req.Params)
					_ = f.Close()
				}
			}
			var data any = map[string]any{"effect": "confirmed", "route": "accessibility", "summary": "fixture field read back", "evidence": []any{map[string]any{"kind": "value_readback"}}}
			switch req.Params.Name {
			case "list_apps":
				apps := []any{map[string]any{"pid": os.Getpid(), "bundle_id": "com.example.Notes", "name": "Notes", "active": true, "running": true}}
				if os.Getenv("REASONIX_BOOT_CUA_REQUEST_LOG") != "" {
					apps = append(apps, map[string]any{"pid": os.Getpid(), "bundle_id": "com.example.Other", "name": "Other", "running": true})
				}
				data = map[string]any{"apps": apps}
				if path := os.Getenv("REASONIX_BOOT_CUA_INSTALLED_APP"); path != "" {
					app := map[string]any{"pid": 0, "bundle_id": path, "launch_path": path, "name": "Notes", "running": false}
					if launched {
						app["pid"] = os.Getpid()
						app["running"] = true
					}
					data = map[string]any{"apps": []any{app}}
				}
			case "launch_app":
				launched = true
				data = map[string]any{"pid": os.Getpid(), "bundle_id": os.Getenv("REASONIX_BOOT_CUA_INSTALLED_APP"), "name": "Notes"}
			case "list_windows":
				data = map[string]any{"windows": []any{map[string]any{"pid": os.Getpid(), "window_id": 1, "title": "Draft", "bounds": map[string]any{"width": 800, "height": 600}}}}
			case "get_window_state":
				data = map[string]any{"pid": os.Getpid(), "window_id": 1, "elements": []any{map[string]any{"element_token": "s00000001:2", "role": "textField", "label": "Body", "value": value, "depth": 0}}}
			case "set_value":
				value, _ = req.Params.Arguments["value"].(string)
			}
			result = map[string]any{"structuredContent": data, "content": []any{}}
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
}

// Through the real assembly: a host with a helper shows the tools, the model's
// calls reach the helper, and what the application shows afterwards comes back.
func TestEffectComputerUseThroughTheRealAssembly(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeComputerHelperEnv, "1")
	log := filepath.Join(testenv.TempDir(t), "native-requests.jsonl")
	t.Setenv("REASONIX_BOOT_CUA_REQUEST_LOG", log)
	driver := computer.NewDriver(exe, os.Environ())
	t.Cleanup(driver.Close)

	reqs := buildBrowserEffect(t, "", []func(string) *provider.ToolCall{
		func(string) *provider.ToolCall {
			return browserCall("apps-1", "computer_read", map[string]any{"what": "apps"})
		},
		func(string) *provider.ToolCall {
			return browserCall("snapshot-1", "computer_read", map[string]any{"what": "snapshot", "app": "com.example.Notes"})
		},
		func(last string) *provider.ToolCall {
			ref := regexp.MustCompile(`\[(r[A-Za-z0-9]+)\]`).FindStringSubmatch(last)
			if len(ref) != 2 {
				t.Fatalf("no snapshot ref: %s", last)
			}
			return browserCall("act-1", "computer_act", map[string]any{"app": "com.example.Notes", "steps": []any{
				map[string]any{"action": "set_value", "ref": ref[1], "text": "hello"},
			}})
		},
	}, Options{ComputerDriver: driver})
	for _, name := range ComputerToolNames() {
		if !toolNames(reqs[0])[name] {
			t.Fatalf("%s missing from the schema of a host with a helper: %v", name, toolSchemaNames(reqs[0].Tools))
		}
	}
	results := effectToolResults(reqs[len(reqs)-1])
	if len(results) != 3 {
		t.Fatalf("tool results = %q", results)
	}
	if !strings.Contains(results[0], `* com.example.Notes — "Notes"`) {
		t.Fatalf("the application list did not reach the model:\n%s", results[0])
	}
	if !strings.Contains(results[2], "Completed 1 of 1 step(s).") || !strings.Contains(results[2], `value="hello"`) {
		t.Fatalf("the act result did not show the application afterwards:\n%s", results[2])
	}
	assertComputerObservationWork(t, log)
}

func assertComputerObservationWork(t *testing.T, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	windows, trees := 0, 0
	for line := range strings.SplitSeq(strings.TrimSpace(string(body)), "\n") {
		var req struct {
			Name string         `json:"name"`
			Args map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			t.Fatal(err)
		}
		switch req.Name {
		case "list_windows":
			windows++
		case "get_window_state":
			trees++
			if req.Args["include_screenshot"] != false || req.Args["include_accessibility_tree"] != true {
				t.Errorf("text result performed unrequested work: %+v", req.Args)
			}
		}
	}
	if windows != 4 || trees != 2 {
		t.Fatalf("native work: windows=%d trees=%d", windows, trees)
	}
}

func TestEffectHostWithoutAHelperLeavesComputerUseOutOfTheSchema(t *testing.T) {
	reqs := buildBrowserEffect(t, "", nil)
	for _, name := range ComputerToolNames() {
		if toolNames(reqs[0])[name] {
			t.Fatalf("%s is in the schema of a host with no helper", name)
		}
	}
}

// The grant group and the mutation classifier recognise computer-use tools by
// name and cannot import them to ask; they have to agree with the tools.
func TestComputerToolIdentityAgreesAcrossTheKernel(t *testing.T) {
	names := ComputerToolNames()
	if len(names) != 2 {
		t.Fatalf("computer tools = %v", names)
	}
	for _, name := range names {
		if !permission.IsComputerTool(name) {
			t.Errorf("%s is not in the computer grant group", name)
		}
	}
	if got := evidence.ToolCallMutationClass("computer_read", json.RawMessage(`{}`), false); got != evidence.MutationNone {
		t.Errorf("reading an application is classified %q", got)
	}
	if got := evidence.ToolCallMutationClass("computer_act", json.RawMessage(`{}`), false); got != evidence.MutationUnknown {
		t.Errorf("operating an application is classified %q, want unknown: it can write where the application may", got)
	}
}
