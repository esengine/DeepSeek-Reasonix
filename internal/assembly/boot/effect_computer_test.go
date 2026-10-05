package boot

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/safety/permission"
)

// fakeComputerHelperEnv makes the test binary answer as the native helper: one
// application with one text field, whose value a set_value changes.
const fakeComputerHelperEnv = "REASONIX_BOOT_FAKE_COMPUTER_HELPER"

func runFakeComputerHelper(in io.Reader, out io.Writer) {
	value := ""
	enc := json.NewEncoder(out)
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		var req struct {
			ID     int64          `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil {
			continue
		}
		var result any = map[string]any{}
		switch req.Method {
		case "apps":
			result = map[string]any{"apps": []any{map[string]any{
				"pid": 42, "bundle": "com.example.Notes", "name": "Notes", "active": true,
				"windows": []any{map[string]any{"id": 1, "title": "Draft", "bounds": map[string]any{"x": 0, "y": 0, "width": 800, "height": 600}}},
			}}}
		case "snapshot":
			result = map[string]any{"lines": []string{`- window "Draft" [a1]`, `  - textField "Body" [a2] value="` + value + `"`}}
		case "set_value":
			value, _ = req.Params["text"].(string)
		}
		_ = enc.Encode(map[string]any{"id": req.ID, "result": result})
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
	SetComputerHelper(exe)
	t.Cleanup(func() { SetComputerHelper("") })

	reqs := buildBrowserEffect(t, "", []func(string) *provider.ToolCall{
		func(string) *provider.ToolCall {
			return browserCall("apps-1", "computer_read", map[string]any{"what": "apps"})
		},
		func(string) *provider.ToolCall {
			return browserCall("act-1", "computer_act", map[string]any{"app": "com.example.Notes", "steps": []any{
				map[string]any{"action": "set_value", "ref": "a2", "text": "hello"},
			}})
		},
	})
	for _, name := range ComputerToolNames() {
		if !toolNames(reqs[0])[name] {
			t.Fatalf("%s missing from the schema of a host with a helper: %v", name, toolSchemaNames(reqs[0].Tools))
		}
	}
	results := effectToolResults(reqs[len(reqs)-1])
	if len(results) != 2 {
		t.Fatalf("tool results = %q", results)
	}
	if !strings.Contains(results[0], "* com.example.Notes — Notes") {
		t.Fatalf("the application list did not reach the model:\n%s", results[0])
	}
	if !strings.Contains(results[1], "Completed 1 of 1 step(s).") || !strings.Contains(results[1], `[a2] value="hello"`) {
		t.Fatalf("the act result did not show the application afterwards:\n%s", results[1])
	}
}

func TestEffectHostWithoutAHelperLeavesComputerUseOutOfTheSchema(t *testing.T) {
	SetComputerHelper("")
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
