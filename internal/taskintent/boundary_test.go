package taskintent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// allowedExports is the package's whole public surface. Adding a name here
// means taskintent is absorbing a new responsibility — read doc.go first:
// complexity, risk, depth, completion, budget and tool-surface decisions
// belong to runtime evidence, not keywords.
var allowedExports = map[string]bool{
	"Intent": true, "Conversation": true, "Advisory": true,
	"ObservableRead": true, "Mutation": true, "PersistentAction": true,
	"Classify": true, "NeedsEvidence": true, "NeedsMutation": true,
	"NeedsPersistentAction": true, "GoalNeedsWriteBudget": true,
	"BudgetClassSimple": true, "BudgetClassWrite": true, "BudgetClassResearch": true,
	"ClassifyGoalBudget": true,
}

// lineBudgets caps the heuristic files: vocabulary growth must displace
// something or justify a deliberate budget bump in review.
var lineBudgets = map[string]int{
	"intent.go":               620,
	"heuristic.go":            180,
	"goal_budget.go":          140,
	"goal_research_budget.go": 120,
}

func TestExportSurfaceIsFrozen(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		{
			for _, decl := range file.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					checkExport(t, d.Name.Name)
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						switch s := spec.(type) {
						case *ast.TypeSpec:
							checkExport(t, s.Name.Name)
						case *ast.ValueSpec:
							for _, name := range s.Names {
								checkExport(t, name.Name)
							}
						}
					}
				}
			}
		}
	}
}

func checkExport(t *testing.T, name string) {
	t.Helper()
	if !ast.IsExported(name) {
		return
	}
	if !allowedExports[name] {
		t.Errorf("new exported name %q: taskintent's surface is frozen (doc.go); classification facts belong in taskcontract signals, not here", name)
	}
}

func TestHeuristicFilesStayWithinBudget(t *testing.T) {
	for name, budget := range lineBudgets {
		data, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		lines := strings.Count(string(data), "\n")
		if lines > budget {
			t.Errorf("%s is %d lines, budget %d: shrink the vocabulary or justify a deliberate budget bump — do not accrete keywords (doc.go)", name, lines, budget)
		}
	}
}
