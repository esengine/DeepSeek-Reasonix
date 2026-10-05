package builtin

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"reasonix/internal/base/tokencount"
	"reasonix/internal/contract/tool"
)

// The largest single item in a first request — 3,995 of one measured run's
// 5,577 prompt tokens — and nothing was watching it. A ratchet rather than a
// ceiling, because a number nobody can move is one people route around:
// shrinking is free, growing is a line in the diff with a reason beside it.
const schemaBudgetPath = "schema_budget.json"

var (
	updateSchemaBudget = flag.Bool("update-schema-budget", false, "rewrite schema_budget.json from this tree")
	allowSchemaWiden   = flag.Bool("allow-schema-widen", false, "let the rewrite raise the budget: what every session pays before it does anything, which is the diff a reviewer has to be shown on purpose")
)

type schemaRow struct {
	name string
	tok  int
}

type schemaBudget struct {
	// TotalTokens is the whole built-in set, so an allow-list edit does not
	// move it; what a session is billed for is the visible surface, which boot
	// guards separately.
	TotalTokens int `json:"totalTokens"`
}

func readSchemaBudget(t *testing.T) schemaBudget {
	t.Helper()
	raw, err := os.ReadFile(schemaBudgetPath)
	if err != nil {
		t.Fatalf("read %s: %v", schemaBudgetPath, err)
	}
	var b schemaBudget
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("parse %s: %v", schemaBudgetPath, err)
	}
	if b.TotalTokens <= 0 {
		t.Fatalf("%s records no budget; the gate would pass anything", schemaBudgetPath)
	}
	return b
}

func writeSchemaBudget(t *testing.T, b schemaBudget) {
	t.Helper()
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(schemaBudgetPath, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuiltinSchemaSurfaceStaysWithinBudget(t *testing.T) {
	r := tool.NewRegistry()
	for _, x := range tool.Builtins() {
		r.Add(x)
	}
	var rows []schemaRow
	total := 0
	for _, s := range r.Schemas() {
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("%s: %v", s.Name, err)
		}
		n := tokencount.Text(string(b))
		rows = append(rows, schemaRow{s.Name, n})
		total += n
	}
	// A registry that came up empty would pass any ceiling.
	if len(rows) < 15 {
		t.Fatalf("only %d built-in schemas were read; the registry is not being populated here", len(rows))
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].tok > rows[j].tok })

	budget := readSchemaBudget(t)
	if *updateSchemaBudget {
		if total > budget.TotalTokens && !*allowSchemaWiden {
			t.Fatalf("the rewrite would raise the budget from %d to %d.\n%s",
				budget.TotalTokens, total, schemaWidenHint(rows))
		}
		writeSchemaBudget(t, schemaBudget{TotalTokens: total})
		t.Logf("schema budget recorded at %d tokens", total)
		return
	}
	if total > budget.TotalTokens {
		t.Errorf("built-in tool schemas are %d tokens, over the %d budget by %d.\n%s",
			total, budget.TotalTokens, total-budget.TotalTokens, schemaWidenHint(rows))
	}
}

func schemaWidenHint(rows []schemaRow) string {
	var b strings.Builder
	b.WriteString("Trim a description, or run `go test ./internal/tools/builtin -run TestBuiltinSchemaSurface" +
		" -update-schema-budget -allow-schema-widen` and say why in the pull request:" +
		" every session pays this before it does anything.\nLargest:")
	for _, x := range rows[:min(len(rows), 8)] {
		fmt.Fprintf(&b, "\n  %5d  %s", x.tok, x.name)
	}
	return b.String()
}
