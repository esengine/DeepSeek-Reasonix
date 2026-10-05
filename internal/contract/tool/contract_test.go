package tool_test

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	_ "reasonix/internal/tools/builtin"
)

// updateContract rewrites the document instead of failing, for the round that
// changed a description on purpose.
var updateContract = flag.Bool("update-contract", false, "rewrite docs/TOOL_CONTRACT.md from the registry")

const contractPath = "../../../docs/TOOL_CONTRACT.md"

func TestBuiltinToolContractDocumentation(t *testing.T) {
	entries := tool.BuiltinContractEntries()
	if len(entries) == 0 {
		t.Fatal("no built-in tool contract entries")
	}
	for _, e := range entries {
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
	doc, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatalf("read %s: %v", contractPath, err)
	}
	// The document says it is generated from the registry, and a check that only
	// asked whether a row existed let every description drift from the one the
	// model is sent — which is what a reader of this file is here to read.
	next, err := contractWithTable(string(doc), entries)
	if err != nil {
		t.Fatal(err)
	}
	if next == string(doc) {
		return
	}
	if *updateContract {
		if err := os.WriteFile(contractPath, []byte(next), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("rewrote " + contractPath)
		return
	}
	t.Errorf("docs/TOOL_CONTRACT.md no longer says what the registry does.\n"+
		"Regenerate it: go test ./internal/contract/tool -run TestBuiltinToolContractDocumentation -update-contract\n%s",
		firstDifference(string(doc), next))
}

// contractWithTable replaces the document's table with the registry's, leaving
// everything a person wrote around it alone.
func contractWithTable(doc string, entries []tool.ContractEntry) (string, error) {
	lines := strings.Split(doc, "\n")
	head := slices.Index(lines, "| Tool | Read-only | Description |")
	if head < 0 || head+1 >= len(lines) {
		return "", errors.New("docs/TOOL_CONTRACT.md has no tool table to generate into")
	}
	end := head + 2
	for end < len(lines) && strings.HasPrefix(lines[end], "| `") {
		end++
	}
	rows := make([]string, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, fmt.Sprintf("| `%s` | %s | %s |", e.Name, boolString(e.ReadOnly), tableCell(e.Description)))
	}
	return strings.Join(slices.Concat(lines[:head+2], rows, lines[end:]), "\n"), nil
}

// tableCell keeps a description inside one cell of one row.
func tableCell(s string) string {
	return strings.NewReplacer("|", "\\|", "\n", " ", "\r", " ").Replace(strings.TrimSpace(s))
}

func firstDifference(have, want string) string {
	a, b := strings.Split(have, "\n"), strings.Split(want, "\n")
	for i := range max(len(a), len(b)) {
		x, y := "", ""
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return fmt.Sprintf("line %d:\n  have %s\n  want %s", i+1, x, y)
		}
	}
	return ""
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
	"bash_output":    true, // streamed job output; tailing handled by the job, not the snip pass
	"browser_act":    true, // step results lead; the page changes after them are the part a head keeps
	"browser_open":   true, // a snapshot reads top-down, so the head is the top of the page
	"browser_read":   true, // same shape as browser_open, and paged by offset when longer
	"code_index":     true,
	"complete_step":  true,
	"render_chart":   true, // a chart_id and a few summary lines; never large enough to snip
	"computer_act":   true, // step results lead, then the application's tree read top-down
	"computer_read":  true, // an accessibility tree reads top-down, like a page snapshot
	"compress":       true,
	"context_budget": true, // a fixed four-number JSON; never large enough to snip
	"delete_range":   true,
	"delete_symbol":  true,
	"edit_file":      true,
	"kill_shell":     true,
	"move_file":      true,
	"multi_edit":     true,
	"notebook_edit":  true,
	"recall":         true, // recalled transcript: head and tail are both real content, like any read
	"todo_write":     true,
	"update_goal":    true,
	"wait":           true,
	"write_file":     true,
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
