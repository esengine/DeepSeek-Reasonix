package config

import (
	"strings"
	"testing"
)

func TestScanTOMLPathEscapesInvalidEscapeClass(t *testing.T) {
	// `\开` is not a valid TOML escape -> the document cannot parse.
	body := `# project config
command = "D:\开发\VoxelForge-Rebuild\bin\godot-mcp-bridge.exe"

[lsp.servers.go]
command = "C:\开发\tools\gopls.exe"
`
	fixes, err := ScanTOMLPathEscapes(body)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(fixes) != 2 {
		t.Fatalf("got %d fixes, want 2: %+v", len(fixes), fixes)
	}
	for _, fx := range fixes {
		if fx.Reason != "invalid_escape" {
			t.Errorf("fix %s reason = %s, want invalid_escape", fx.Field, fx.Reason)
		}
	}
	if fixes[0].Field != "command" || fixes[0].Line != 2 {
		t.Errorf("fix[0] = %+v, want field command on line 2", fixes[0])
	}
	if fixes[1].Field != "lsp.servers.go.command" {
		t.Errorf("fix[1].Field = %s, want lsp.servers.go.command", fixes[1].Field)
	}
	if !strings.Contains(fixes[0].FixedToken, `D:\\开发\\VoxelForge-Rebuild\\bin\\godot-mcp-bridge.exe`) {
		t.Errorf("fix[0].FixedToken = %s", fixes[0].FixedToken)
	}
}

func TestScanTOMLPathEscapesSemanticClass(t *testing.T) {
	// `\n` and `\t` are valid escapes; the document parses but the value
	// silently changes. The fixed token must decode to the literal path.
	body := `command = "D:\new\tool.exe"`
	fixes, err := ScanTOMLPathEscapes(body)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(fixes) != 1 {
		t.Fatalf("got %d fixes, want 1", len(fixes))
	}
	if fixes[0].Reason != "semantic_escape" {
		t.Errorf("reason = %s, want semantic_escape", fixes[0].Reason)
	}
	fixed, err := ApplyTOMLPathEscapes(body, fixes)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	var cfg struct {
		Command string `toml:"command"`
	}
	if _, err := decodeTOMLBytes([]byte(fixed), &cfg); err != nil {
		t.Fatalf("fixed body does not parse: %v", err)
	}
	if cfg.Command != `D:\new\tool.exe` {
		t.Errorf("decoded command = %q, want literal path", cfg.Command)
	}
}

func TestScanTOMLPathEscapesAlreadyEscapedNotModified(t *testing.T) {
	body := `command = "D:\\开发\\tool.exe"`
	fixes, err := ScanTOMLPathEscapes(body)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(fixes) != 0 {
		t.Errorf("already-escaped path reported %d fixes: %+v", len(fixes), fixes)
	}
	// Legal escaped backslash before a Chinese char must survive unchanged.
	body = "command = \"D:\\\\开\""
	fixes, err = ScanTOMLPathEscapes(body)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(fixes) != 0 {
		t.Errorf("legal \\\\开 reported fixes: %+v", fixes)
	}
}

func TestScanTOMLPathEscapesUNC(t *testing.T) {
	body := `[[remote.hosts]]
name = "dev"
identity_file = "\\server\share\keys\id_rsa"`
	fixes, err := ScanTOMLPathEscapes(body)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(fixes) != 1 {
		t.Fatalf("got %d fixes, want 1: %+v", len(fixes), fixes)
	}
	fixed, err := ApplyTOMLPathEscapes(body, fixes)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	var cfg struct {
		Remote struct {
			Hosts []struct {
				IdentityFile string `toml:"identity_file"`
			} `toml:"hosts"`
		} `toml:"remote"`
	}
	if _, err := decodeTOMLBytes([]byte(fixed), &cfg); err != nil {
		t.Fatalf("fixed body does not parse: %v", err)
	}
	// The UNC prefix stays two literal backslashes: a repaired UNC path must
	// remain a valid network path.
	if cfg.Remote.Hosts[0].IdentityFile != `\\server\share\keys\id_rsa` {
		t.Errorf("identity_file = %q, want valid UNC path", cfg.Remote.Hosts[0].IdentityFile)
	}
}

func TestScanTOMLPathEscapesPluginArgsEnv(t *testing.T) {
	body := `[[plugins]]
name = "godot"
command = "D:\dev\bridge.exe"
args = ["D:\scripts\run.ps1"]
env = { GODOT_PATH = "D:\engine\bin\godot.exe" }
`
	fixes, err := ScanTOMLPathEscapes(body)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(fixes) != 3 {
		t.Fatalf("got %d fixes, want 3: %+v", len(fixes), fixes)
	}
	fields := map[string]bool{}
	for _, fx := range fixes {
		fields[fx.Field] = true
	}
	for _, want := range []string{"plugins[0].command", "plugins[0].args[0]", "plugins[0].env.GODOT_PATH"} {
		if !fields[want] {
			t.Errorf("missing fix for %s; have %v", want, fields)
		}
	}
	fixed, err := ApplyTOMLPathEscapes(body, fixes)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	var cfg struct {
		Plugins []struct {
			Command string            `toml:"command"`
			Args    []string          `toml:"args"`
			Env     map[string]string `toml:"env"`
		} `toml:"plugins"`
	}
	if _, err := decodeTOMLBytes([]byte(fixed), &cfg); err != nil {
		t.Fatalf("fixed body does not parse: %v", err)
	}
	if got := cfg.Plugins[0].Args[0]; got != `D:\scripts\run.ps1` {
		t.Errorf("args[0] = %q", got)
	}
	if got := cfg.Plugins[0].Env["GODOT_PATH"]; got != `D:\engine\bin\godot.exe` {
		t.Errorf("env.GODOT_PATH = %q", got)
	}
	if got := cfg.Plugins[0].Command; got != `D:\dev\bridge.exe` {
		t.Errorf("command = %q", got)
	}
}

func TestScanTOMLPathEscapesRefusesAmbiguousAndNonPath(t *testing.T) {
	cases := []string{
		// trailing backslash before closing quote: boundary ambiguous
		"command = \"D:\\dir\\\"",
		// non-path field without drive/UNC shape: `\n` could be intentional
		"system_prompt = \"say \\n hi\"",
		// multi-line string
		"system_prompt = \"\"\"\nD:\\开发\n\"\"\"",
		// unrelated syntax error elsewhere
		"command = \"D:\\dev\\x.exe\"\n[broken\n",
	}
	for _, body := range cases {
		fixes, err := ScanTOMLPathEscapes(body)
		if err == nil && len(fixes) > 0 {
			t.Errorf("body %q reported fixes %+v, want none/error", body, fixes)
		}
	}
}

func TestScanTOMLPathEscapesSkillsArraysAndSSH(t *testing.T) {
	body := `[skills]
paths = ["D:\skills\custom", "D:\skills\more"]

[[remote.hosts]]
name = "dev"
identity_file = "C:\Users\me\.ssh\id_ed25519"
workspace = "D:\repos\app"

[tools.shell]
path = "C:\Program Files\Git\bin\bash.exe"
`
	fixes, err := ScanTOMLPathEscapes(body)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(fixes) != 5 {
		t.Fatalf("got %d fixes, want 5: %+v", len(fixes), fixes)
	}
	fixed, err := ApplyTOMLPathEscapes(body, fixes)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	var cfg struct {
		Skills struct {
			Paths []string `toml:"paths"`
		} `toml:"skills"`
		Remote struct {
			Hosts []struct {
				IdentityFile string `toml:"identity_file"`
				Workspace    string `toml:"workspace"`
			} `toml:"hosts"`
		} `toml:"remote"`
		Tools struct {
			Shell struct {
				Path string `toml:"path"`
			} `toml:"shell"`
		} `toml:"tools"`
	}
	if _, err := decodeTOMLBytes([]byte(fixed), &cfg); err != nil {
		t.Fatalf("fixed body does not parse: %v", err)
	}
	if got := cfg.Skills.Paths[0]; got != `D:\skills\custom` {
		t.Errorf("skills.paths[0] = %q", got)
	}
	if got := cfg.Remote.Hosts[0].IdentityFile; got != `C:\Users\me\.ssh\id_ed25519` {
		t.Errorf("identity_file = %q", got)
	}
	if got := cfg.Tools.Shell.Path; got != `C:\Program Files\Git\bin\bash.exe` {
		t.Errorf("shell path = %q", got)
	}
}

func TestScanTOMLPathEscapesMixedEscape(t *testing.T) {
	// A token mixing already-escaped and unescaped backslashes is repaired by
	// doubling only the unescaped singles: `\d` is an invalid escape (the
	// document cannot parse) while the existing `\\` pair keeps its TOML
	// meaning. The repaired token must decode to the value as written.
	body := `command = "C:\dir\\file.exe"`
	fixes, err := ScanTOMLPathEscapes(body)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(fixes) != 1 {
		t.Fatalf("got %d fixes, want 1: %+v", len(fixes), fixes)
	}
	fixed, err := ApplyTOMLPathEscapes(body, fixes)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	var cfg struct {
		Command string `toml:"command"`
	}
	if _, err := decodeTOMLBytes([]byte(fixed), &cfg); err != nil {
		t.Fatalf("fixed body does not parse: %v", err)
	}
	if cfg.Command != `C:\dir\file.exe` {
		t.Errorf("decoded command = %q, want C:\\dir\\file.exe", cfg.Command)
	}
}

func TestScanTOMLPathEscapesEmptyAndClean(t *testing.T) {
	for _, body := range []string{
		"",
		"command = \"gopls\"\n",
		"command = \"/usr/local/bin/tool\"\n",
		"# comment with D:\\path\\inside\n",
	} {
		fixes, err := ScanTOMLPathEscapes(body)
		if err != nil {
			t.Fatalf("scan(%q): %v", body, err)
		}
		if len(fixes) != 0 {
			t.Errorf("scan(%q) = %+v, want none", body, fixes)
		}
	}
}

// FuzzScanTOMLPathEscapes guarantees the scanner never crashes on arbitrary
// input and that every accepted fix leaves the document parseable.
func FuzzScanTOMLPathEscapes(f *testing.F) {
	for _, seed := range []string{
		`command = "D:\开发\tool.exe"`,
		`env = { PATH = "C:\Users\me\bin" }`,
		"[[plugins]]\ncommand = \"\\\\server\\share\\x.exe\"\n",
		"# comment only\n",
		"system_prompt = \"\"\"\nD:\\开发\n\"\"\"\n",
		"a = [1, 2, 3]\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body string) {
		fixes, err := ScanTOMLPathEscapes(body)
		if err != nil {
			return // repair refused: caller falls back to quarantine
		}
		if len(fixes) == 0 {
			return // nothing repairable; the document stays untouched
		}
		fixed, err := ApplyTOMLPathEscapes(body, fixes)
		if err != nil {
			t.Fatalf("apply of %d verified fixes failed: %v", len(fixes), err)
		}
		if fixed == body {
			t.Fatalf("no fix applied but %d fixes reported", len(fixes))
		}
		var raw map[string]any
		if _, err := decodeTOMLBytes([]byte(fixed), &raw); err != nil {
			t.Fatalf("fixed document does not parse: %v\nfixed: %q", err, fixed)
		}
	})
}
