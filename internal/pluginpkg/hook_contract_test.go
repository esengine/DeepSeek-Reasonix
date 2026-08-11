package pluginpkg

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestHookJSONExecutionFormPresenceMatrix(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantArgsSet bool
		wantArgs    []string
	}{
		{name: "omitted", body: `{"command":"echo ok"}`},
		{name: "explicit empty", body: `{"command":"echo","args":[]}`, wantArgsSet: true, wantArgs: []string{}},
		{name: "case insensitive field", body: `{"command":"echo","Args":[]}`, wantArgsSet: true, wantArgs: []string{}},
		{name: "literal values", body: `{"command":"echo","args":[""," spaced ","$HOME","a && b"]}`, wantArgsSet: true, wantArgs: []string{"", " spaced ", "$HOME", "a && b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Hook
			if err := json.Unmarshal([]byte(tt.body), &got); err != nil {
				t.Fatal(err)
			}
			if got.ArgsSet != tt.wantArgsSet || !reflect.DeepEqual(got.Args, tt.wantArgs) {
				t.Fatalf("hook args = %#v set=%v, want %#v set=%v", got.Args, got.ArgsSet, tt.wantArgs, tt.wantArgsSet)
			}
		})
	}
}

func TestHookJSONRoundTripPreservesExplicitEmptyArgs(t *testing.T) {
	before := Hook{Command: "bin/check", Args: []string{}, ArgsSet: true}
	body, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"args":[]`) {
		t.Fatalf("explicit empty args disappeared from JSON: %s", body)
	}
	var after Hook
	if err := json.Unmarshal(body, &after); err != nil {
		t.Fatal(err)
	}
	if !after.ArgsSet || after.Args == nil || len(after.Args) != 0 {
		t.Fatalf("round-tripped hook = %#v, want explicit empty exec form", after)
	}
}

func TestHookJSONRoundTripKeepsShellFormWithoutArgs(t *testing.T) {
	before := Hook{Command: "echo ok && echo done", ShellCommand: true, Shell: "bash"}
	body, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"args"`) {
		t.Fatalf("shell-form JSON unexpectedly gained args: %s", body)
	}
	var after Hook
	if err := json.Unmarshal(body, &after); err != nil {
		t.Fatal(err)
	}
	if after.ArgsSet || after.Args != nil || after.Shell != "bash" {
		t.Fatalf("round-tripped shell hook = %#v", after)
	}
}

func TestNormalizeHooksExecutionContractMatrix(t *testing.T) {
	got := normalizeHooks(map[string][]Hook{
		" SessionStart ": {
			{Command: "  echo legacy  "},
			{Command: "  echo shell  ", Shell: " PoWeRsHeLl "},
			{Command: "bin/check", Args: []string{}, ArgsSet: true, Shell: "BASH"},
			{ContextFile: "  CLAUDE.md  "},
			{Command: "   "},
		},
	})
	hooks := got["SessionStart"]
	if len(hooks) != 4 {
		t.Fatalf("normalized hooks = %#v, want four non-empty hooks", hooks)
	}
	if hooks[0].Command != "echo legacy" || hooks[0].ShellCommand {
		t.Fatalf("legacy hook changed mode: %#v", hooks[0])
	}
	if hooks[1].Command != "echo shell" || hooks[1].Shell != "powershell" || !hooks[1].ShellCommand {
		t.Fatalf("shell hook normalization = %#v", hooks[1])
	}
	if !hooks[2].ArgsSet || hooks[2].ShellCommand || hooks[2].Shell != "bash" {
		t.Fatalf("exec hook did not take precedence over shell: %#v", hooks[2])
	}
	if hooks[3].ContextFile != "CLAUDE.md" {
		t.Fatalf("context hook normalization = %#v", hooks[3])
	}
}

func TestValidHookShellMatrix(t *testing.T) {
	for _, shell := range []string{"", "auto", "AUTO", "bash", "powershell", "pwsh", "cmd", " CMD "} {
		if !validHookShell(shell) {
			t.Errorf("validHookShell(%q) = false", shell)
		}
	}
	for _, shell := range []string{"sh", "zsh", "fish", "cmd.exe", "powershell.exe"} {
		if validHookShell(shell) {
			t.Errorf("validHookShell(%q) = true", shell)
		}
	}
}

func TestNativeManifestRejectsUnsupportedHookShell(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, NativeManifest)
	writeTestFile(t, path, `{
  "apiVersion":"reasonix.io/plugin/v2",
  "name":"bad-shell",
  "hooks":{"SessionStart":[{"command":"echo ok","shell":"fish"}]}
}`)
	if _, _, err := parseNative(path, root); err == nil || !strings.Contains(err.Error(), `hook shell "fish" is not supported`) {
		t.Fatalf("parseNative unsupported shell error = %v", err)
	}
}

func FuzzHookJSONExecFormRoundTrip(f *testing.F) {
	for _, seed := range []string{"", " spaced ", "$HOME", `%PATH%`, `a && b | c`, `quote"'`, "中文🧪"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, arg string) {
		if !utf8.ValidString(arg) {
			t.Skip()
		}
		before := Hook{Command: "tool", Args: []string{arg, "", "tail"}, ArgsSet: true}
		body, err := json.Marshal(before)
		if err != nil {
			t.Fatal(err)
		}
		var after Hook
		if err := json.Unmarshal(body, &after); err != nil {
			t.Fatal(err)
		}
		if !after.ArgsSet || !reflect.DeepEqual(after.Args, before.Args) {
			t.Fatalf("exec-form args changed:\n got %#v\nwant %#v\njson %s", after.Args, before.Args, body)
		}
	})
}
