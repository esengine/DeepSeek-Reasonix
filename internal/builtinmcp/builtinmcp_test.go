package builtinmcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestEntries(t *testing.T) {
	currentExecutable = func() (string, error) { return "reasonix", nil }
	lookPath = func(file string) (string, error) {
		if file == "npx" {
			return "/usr/bin/npx", nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() {
		currentExecutable = executablePathDefault
		lookPath = lookPathDefault
	})

	entries := Entries()
	if len(entries) != 2 {
		t.Fatalf("Entries() length = %d, want 2", len(entries))
	}
	want := map[string][]string{
		TimeName:     []string{"builtin-mcp", "time"},
		Context7Name: []string{"-y", "@upstash/context7-mcp"},
	}
	for _, e := range entries {
		args, ok := want[e.Name]
		if !ok {
			t.Fatalf("unexpected built-in MCP entry: %+v", e)
		}
		wantCommand := map[string]string{
			TimeName:     "reasonix",
			Context7Name: "npx",
		}[e.Name]
		if e.Type != "stdio" || e.Command != wantCommand || e.Tier != "lazy" {
			t.Fatalf("%s type/command/tier = %q/%q/%q, want stdio/%s/lazy", e.Name, e.Type, e.Command, e.Tier, wantCommand)
		}
		if !reflect.DeepEqual(e.Args, args) {
			t.Fatalf("%s args = %+v, want %+v", e.Name, e.Args, args)
		}
		delete(want, e.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing built-in MCP entries: %+v", want)
	}
}

func TestAppendMissingLetsUserConfigWin(t *testing.T) {
	base := Entries()[:1]
	got := AppendMissing(nil, base)
	if len(got) != 1 || got[0].Name != Context7Name {
		t.Fatalf("AppendMissing with configured time = %+v, want only context7", got)
	}
}

func TestAppendMissingLetsReservedNamesWin(t *testing.T) {
	got := AppendMissing(nil, nil, TimeName)
	if len(got) != 1 || got[0].Name != Context7Name {
		t.Fatalf("AppendMissing with reserved time = %+v, want only context7", got)
	}
}

func TestAppendEnabledOnlyAddsEnabledBuiltIns(t *testing.T) {
	got := AppendEnabled(nil, nil, []string{TimeName})
	if len(got) != 1 || got[0].Name != TimeName {
		t.Fatalf("AppendEnabled(time) = %+v, want only time", got)
	}
	if got := AppendEnabled(nil, nil, nil); len(got) != 0 {
		t.Fatalf("AppendEnabled(nil) = %+v, want none", got)
	}
}

func TestContext7CommandFallsBackThroughJSRunners(t *testing.T) {
	lookPath = func(file string) (string, error) {
		if file == "pnpm" {
			return "/usr/bin/pnpm", nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { lookPath = lookPathDefault })

	cmd, args := context7Command()
	if cmd != "pnpm" || !reflect.DeepEqual(args, []string{"dlx", "@upstash/context7-mcp"}) {
		t.Fatalf("context7Command = %q %+v, want pnpm dlx", cmd, args)
	}
}

func TestServeTimeMCPListsTools(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n")
	if err := ServeTimeMCP(in, &out, "test"); err != nil {
		t.Fatalf("ServeTimeMCP: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("response lines = %d (%q), want 2", len(lines), out.String())
	}
	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &resp); err != nil {
		t.Fatalf("decode tools/list response: %v", err)
	}
	got := []string{resp.Result.Tools[0].Name, resp.Result.Tools[1].Name}
	want := []string{"get_current_time", "convert_time"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("time MCP tools = %+v, want %+v", got, want)
	}
}
