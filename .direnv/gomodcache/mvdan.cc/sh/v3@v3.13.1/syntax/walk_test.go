// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"reflect"
	"strings"
	"testing"
)

func TestWalk(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{
		"*syntax.File":         false,
		"*syntax.Comment":      false,
		"*syntax.Stmt":         false,
		"*syntax.Assign":       false,
		"*syntax.Redirect":     false,
		"*syntax.CallExpr":     false,
		"*syntax.Subshell":     false,
		"*syntax.Block":        false,
		"*syntax.IfClause":     false,
		"*syntax.WhileClause":  false,
		"*syntax.ForClause":    false,
		"*syntax.WordIter":     false,
		"*syntax.CStyleLoop":   false,
		"*syntax.BinaryCmd":    false,
		"*syntax.FuncDecl":     false,
		"*syntax.Word":         false,
		"*syntax.Lit":          false,
		"*syntax.SglQuoted":    false,
		"*syntax.DblQuoted":    false,
		"*syntax.CmdSubst":     false,
		"*syntax.ParamExp":     false,
		"*syntax.ArithmExp":    false,
		"*syntax.ArithmCmd":    false,
		"*syntax.BinaryArithm": false,
		"*syntax.UnaryArithm":  false,
		"*syntax.ParenArithm":  false,
		"*syntax.CaseClause":   false,
		"*syntax.CaseItem":     false,
		"*syntax.TestClause":   false,
		"*syntax.BinaryTest":   false,
		"*syntax.UnaryTest":    false,
		"*syntax.ParenTest":    false,
		"*syntax.DeclClause":   false,
		"*syntax.ArrayExpr":    false,
		"*syntax.ArrayElem":    false,
		"*syntax.ExtGlob":      false,
		"*syntax.ProcSubst":    false,
		"*syntax.TimeClause":   false,
		"*syntax.CoprocClause": false,
		"*syntax.LetClause":    false,
	}
	parser := NewParser(KeepComments(true))
	var allStrs []string
	for _, c := range fileTests {
		allStrs = append(allStrs, c.inputs[0])
	}
	for _, c := range printTests {
		allStrs = append(allStrs, c.in)
	}
	countRan := 0
	for _, in := range allStrs {
		t.Run("", func(t *testing.T) {
			t.Logf("input: %s", in)
			countRan++
			prog, err := parser.Parse(strings.NewReader(in), "")
			if err != nil {
				// good enough for now, as the bash parser
				// ignoring errors covers what we need.
				return
			}
			lastOffs := uint(0)
			Walk(prog, func(node Node) bool {
				if node == nil {
					return false
				}
				tstr := reflect.TypeOf(node).String()
				if _, ok := seen[tstr]; !ok {
					t.Errorf("unexpected type: %s", tstr)
				} else {
					seen[tstr] = true
				}
				switch node.(type) {
				case *Lit:
					return false
				case *Comment:
				default:
					return true
				}
				offs := node.Pos().Offset()
				if offs >= lastOffs {
					lastOffs = offs
				} else {
					t.Errorf("comment offset goes back")
				}
				return true
			})
		})
	}
	// If we're running a subset of the tests,
	// we can't expect to have seen all node types.
	if countRan == len(allStrs) {
		for tstr, tseen := range seen {
			if !tseen {
				t.Errorf("type not seen: %s", tstr)
			}
		}
	}
}

type newNode struct{}

func (newNode) Pos() Pos { return Pos{} }
func (newNode) End() Pos { return Pos{} }

func TestWalkUnexpectedType(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("did not panic")
		}
	}()
	Walk(newNode{}, func(node Node) bool {
		return true
	})
}
