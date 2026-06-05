package inspect

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/command"
	"reasonix/internal/config"
	"reasonix/internal/tool"
	_ "reasonix/internal/tool/builtin" // self-register built-ins for LookupBuiltin
)

// fakeTool is a minimal Tool for registry projection tests. It never implements
// Previewer, so it stands in for a non-writer / MCP tool.
type fakeTool struct {
	name     string
	readOnly bool
}

func (f fakeTool) Name() string                                             { return f.name }
func (f fakeTool) Description() string                                      { return "desc of " + f.name }
func (f fakeTool) Schema() json.RawMessage                                  { return json.RawMessage(`{"type":"object"}`) }
func (f fakeTool) ReadOnly() bool                                           { return f.readOnly }
func (f fakeTool) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }

func TestProviders(t *testing.T) {
	cfg := config.Default()
	// Default model is deepseek-flash; give its key so KeyReady flips true.
	t.Setenv("DEEPSEEK_API_KEY", "sk-test")
	t.Setenv("MIMO_API_KEY", "") // ensure mimo stays not-ready

	got := Providers(cfg)
	if len(got) == 0 {
		t.Fatal("expected default providers")
	}

	var sawDefault, sawReady bool
	for _, p := range got {
		if p.Name == cfg.DefaultModel {
			if !p.IsDefault {
				t.Errorf("provider %q should be flagged default", p.Name)
			}
			sawDefault = true
		}
		if p.APIKeyEnv == "DEEPSEEK_API_KEY" {
			if !p.KeyReady {
				t.Errorf("deepseek provider %q should be key-ready", p.Name)
			}
			if p.Pricing == nil || p.Pricing.Currency == "" {
				t.Errorf("deepseek provider %q should carry pricing", p.Name)
			}
			sawReady = true
		}
		if p.APIKeyEnv == "MIMO_API_KEY" && p.KeyReady {
			t.Errorf("mimo provider %q should not be key-ready", p.Name)
		}
	}
	if !sawDefault {
		t.Error("default provider not found in projection")
	}
	if !sawReady {
		t.Error("no deepseek provider found to check key readiness")
	}
}

func TestTools(t *testing.T) {
	reg := tool.NewRegistry()
	// Real built-ins exercise the Previewer / read-only projection.
	for _, name := range []string{"read_file", "write_file"} {
		if b, ok := tool.LookupBuiltin(name); ok {
			reg.Add(b)
		} else {
			t.Fatalf("built-in %q missing", name)
		}
	}
	// A fake MCP-named tool exercises source classification.
	reg.Add(fakeTool{name: "mcp__demo__echo", readOnly: true})

	got := Tools(reg)
	if len(got) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(got))
	}

	by := map[string]ToolInfo{}
	for _, ti := range got {
		by[ti.Name] = ti
	}

	if rf := by["read_file"]; !rf.ReadOnly || rf.Previewable || rf.Source != "builtin" {
		t.Errorf("read_file projection wrong: %+v", rf)
	}
	if wf := by["write_file"]; wf.ReadOnly || !wf.Previewable || wf.Source != "builtin" {
		t.Errorf("write_file should be a previewable builtin writer: %+v", wf)
	}
	if e := by["mcp__demo__echo"]; e.Source != "mcp:demo" {
		t.Errorf("mcp tool source = %q, want mcp:demo", e.Source)
	}
	if len(by["read_file"].Schema) == 0 {
		t.Error("schema should be carried through")
	}
}

func TestToolSource(t *testing.T) {
	cases := map[string]string{
		"read_file":           "builtin",
		"bash":                "builtin",
		"mcp__stripe__charge": "mcp:stripe",
		"mcp__fs__read":       "mcp:fs",
		"mcp__weird":          "mcp", // malformed (no second __) falls back
	}
	for name, want := range cases {
		if got := toolSource(name); got != want {
			t.Errorf("toolSource(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestCommands(t *testing.T) {
	cmds := []command.Command{
		{Name: "review", Description: "Review the diff", ArgHint: "[area]", Source: "/x/review.md"},
		{Name: "git:commit", Description: "Commit", Source: "/x/git/commit.md"},
	}
	got := Commands(cmds)
	if len(got) != 2 || got[0].Name != "review" || got[0].ArgHint != "[area]" {
		t.Fatalf("command projection wrong: %+v", got)
	}
	if Commands(nil) != nil {
		t.Error("nil commands should project to nil")
	}
}

// TestNilInputsSafe ensures every projector tolerates absent runtime objects —
// a desktop front-end may query capabilities before plugins/registry exist.
func TestNilInputsSafe(t *testing.T) {
	if Providers(nil) != nil || Tools(nil) != nil ||
		Servers(nil) != nil || Prompts(nil) != nil || Resources(nil) != nil {
		t.Error("nil inputs should project to nil slices")
	}
	snap := Capabilities(nil, nil, nil, nil)
	if snap.DefaultModel != "" || snap.Providers != nil || snap.Tools != nil {
		t.Errorf("empty snapshot expected, got %+v", snap)
	}
}
