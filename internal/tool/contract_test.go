package tool_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
	_ "reasonix/internal/tool/builtin"
)

func TestBuiltinToolContractDocumentation(t *testing.T) {
	entries := tool.BuiltinContractEntries()
	if len(entries) == 0 {
		t.Fatal("no built-in tool contract entries")
	}
	doc, err := os.ReadFile("../../docs/TOOL_CONTRACT.md")
	if err != nil {
		t.Fatalf("read docs/TOOL_CONTRACT.md: %v", err)
	}
	text := string(doc)
	for _, e := range entries {
		if !strings.Contains(text, "| `"+e.Name+"` |") {
			t.Errorf("documentation missing table row for %s", e.Name)
		}
		if !strings.Contains(text, "| `"+e.Name+"` | "+boolString(e.ReadOnly)+" |") {
			t.Errorf("documentation missing read-only flag for %s", e.Name)
		}
		if strings.TrimSpace(e.Description) == "" {
			t.Errorf("%s has empty description", e.Name)
		}
		if !json.Valid(e.Schema) {
			t.Errorf("%s schema is invalid JSON: %s", e.Name, e.Schema)
		}
		if got := string(provider.CanonicalizeSchema(e.Schema)); got != string(e.Schema) {
			t.Errorf("%s schema is not canonical", e.Name)
		}
	}
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// acceptsDefaultSnip lists built-in tools that deliberately take the
// ReadOnly-tiered default snip geometry instead of implementing
// tool.SnipHinter. A tool belongs here only if its output has no special shape
// a generic head/tail split would garble — typically tools whose results are
// short (todo_write, complete_step) or already structured small (edit results).
// Membership is an explicit decision, not a fallback: a new or renamed built-in
// that lands in neither this set nor SnipHinter fails TestEveryBuiltinDeclaresSnipStance,
// which is the guard against a context-maintenance strategy silently desyncing
// from the tool surface.
var acceptsDefaultSnip = map[string]bool{
	"bash_output":   true, // streamed job output; tailing handled by the job, not the snip pass
	"code_index":    true,
	"complete_step": true,
	"delete_range":  true,
	"delete_symbol": true,
	"edit_file":     true,
	"kill_shell":    true,
	"move_file":     true,
	"multi_edit":    true,
	"notebook_edit": true,
	"todo_write":    true,
	"wait":          true,
	"write_file":    true,
}

func TestEveryBuiltinDeclaresSnipStance(t *testing.T) {
	for _, b := range tool.Builtins() {
		name := b.Name()
		_, hints := b.(tool.SnipHinter)
		switch {
		case hints && acceptsDefaultSnip[name]:
			t.Errorf("%s both implements SnipHinter and is listed in acceptsDefaultSnip; remove it from the list", name)
		case !hints && !acceptsDefaultSnip[name]:
			t.Errorf("built-in %q declares no snip stance: implement tool.SnipHinter for a tailored geometry, or add it to acceptsDefaultSnip if the ReadOnly-tiered default is right (this guards against a renamed/new tool silently taking a generic default)", name)
		}
	}
}
