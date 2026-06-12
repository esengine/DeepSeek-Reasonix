package builtinmcp

import (
	"reflect"
	"testing"
)

func TestEntries(t *testing.T) {
	entries := Entries()
	if len(entries) != 2 {
		t.Fatalf("Entries() length = %d, want 2", len(entries))
	}
	want := map[string][]string{
		TimeName:     []string{"mcp-server-time"},
		Context7Name: []string{"-y", "@upstash/context7-mcp"},
	}
	for _, e := range entries {
		args, ok := want[e.Name]
		if !ok {
			t.Fatalf("unexpected built-in MCP entry: %+v", e)
		}
		wantCommand := map[string]string{
			TimeName:     "uvx",
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
