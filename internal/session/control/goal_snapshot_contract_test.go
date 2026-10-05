package control

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// A goal crosses a session rotation through capture/restore, both copying field
// by field. A field added to goalMachine and forgotten there is no compile
// error — the goal arrives without it, and nothing reports that. Anything
// deliberately left out is named below with why.
var goalSnapshotExempt = map[string]string{
	"mu":                "a lock is not state to carry",
	"writeMu":           "a lock is not state to carry",
	"continuationEpoch": "restore bumps it; carrying one would reinstate a stale epoch",
	"tokenBudget":       "configured when the machine is built, not part of the goal",
	"statePath":         "where this controller persists, not what the goal is",
}

func TestGoalSnapshotCarriesEveryGoalField(t *testing.T) {
	machine := structFieldNames(t, "goal.go", "goalMachine")
	snapshot := structFieldNames(t, "goal_durable.go", "goalMachineSnapshot")
	if len(machine) < 15 || len(snapshot) < 15 {
		t.Fatalf("read %d machine and %d snapshot fields; the parse broke, not the copy", len(machine), len(snapshot))
	}
	for name := range machine {
		if snapshot[name] || goalSnapshotExempt[name] != "" {
			continue
		}
		t.Errorf("goalMachine.%s is missing from goalMachineSnapshot, so a goal loses it across a rotation. Carry it, or name it in goalSnapshotExempt with why.", name)
	}
	for name := range snapshot {
		if !machine[name] {
			t.Errorf("goalMachineSnapshot.%s has nowhere to land: goalMachine has no such field", name)
		}
	}
	for name := range goalSnapshotExempt {
		if !machine[name] {
			t.Errorf("goalSnapshotExempt names %q, which goalMachine no longer has", name)
		}
	}
}

// Declaring the field is half of it: both directions of the copy have to name
// it too, and a forgotten line there loses exactly as much.
func TestGoalSnapshotAndRestoreNameEveryField(t *testing.T) {
	snapshot := structFieldNames(t, "goal_durable.go", "goalMachineSnapshot")
	for _, fn := range []string{"captureLocked", "restore"} {
		named := fieldsNamedIn(t, "goal_durable.go", fn)
		if len(named) < 15 {
			t.Fatalf("%s names %d fields; the parse broke, not the copy", fn, len(named))
		}
		for name := range snapshot {
			if !named[name] {
				t.Errorf("goalMachine.%s never appears in %s(): the field is declared but not copied", name, fn)
			}
		}
	}
}

func structFieldNames(t *testing.T, file, typeName string) map[string]bool {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	out := map[string]bool{}
	ast.Inspect(parsed, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok || spec.Name.Name != typeName {
			return true
		}
		st, ok := spec.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, field := range st.Fields.List {
			for _, name := range field.Names {
				out[name.Name] = true
			}
		}
		return false
	})
	if len(out) == 0 {
		t.Fatalf("%s declares no %s", file, typeName)
	}
	return out
}

// fieldsNamedIn collects every selector this function mentions, which is how a
// copy is spelled either way round: g.x = snapshot.x, or x: g.x.
func fieldsNamedIn(t *testing.T, file, funcName string) map[string]bool {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	out := map[string]bool{}
	ast.Inspect(parsed, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != funcName || fn.Body == nil {
			return true
		}
		ast.Inspect(fn.Body, func(inner ast.Node) bool {
			switch node := inner.(type) {
			case *ast.SelectorExpr:
				out[node.Sel.Name] = true
			case *ast.KeyValueExpr:
				if key, ok := node.Key.(*ast.Ident); ok {
					out[key.Name] = true
				}
			}
			return true
		})
		return false
	})
	if len(out) == 0 {
		t.Fatalf("%s declares no %s()", file, funcName)
	}
	return out
}
