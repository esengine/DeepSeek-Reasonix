// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"fmt"
	"io"
	"reflect"
)

// Walk traverses a syntax tree in depth-first order: It starts by calling
// f(node); node must not be nil. If f returns true, Walk invokes f
// recursively for each of the non-nil children of node, followed by
// f(nil).
func Walk(node Node, f func(Node) bool) {
	if !f(node) {
		return
	}

	switch node := node.(type) {
	case *File:
		walkList(node.Stmts, f)
		walkComments(node.Last, f)
	case *Comment:
	case *Stmt:
		for _, c := range node.Comments {
			if !node.End().After(c.Pos()) {
				defer Walk(&c, f)
				break
			}
			Walk(&c, f)
		}
		if node.Cmd != nil {
			Walk(node.Cmd, f)
		}
		walkList(node.Redirs, f)
	case *Assign:
		walkNilable(node.Name, f)
		walkNilable(node.Value, f)
		walkNilable(node.Index, f)
		walkNilable(node.Array, f)
	case *Redirect:
		walkNilable(node.N, f)
		Walk(node.Word, f)
		walkNilable(node.Hdoc, f)
	case *CallExpr:
		walkList(node.Assigns, f)
		walkList(node.Args, f)
	case *Subshell:
		walkList(node.Stmts, f)
		walkComments(node.Last, f)
	case *Block:
		walkList(node.Stmts, f)
		walkComments(node.Last, f)
	case *IfClause:
		walkList(node.Cond, f)
		walkComments(node.CondLast, f)
		walkList(node.Then, f)
		walkComments(node.ThenLast, f)
		walkNilable(node.Else, f)
	case *WhileClause:
		walkList(node.Cond, f)
		walkComments(node.CondLast, f)
		walkList(node.Do, f)
		walkComments(node.DoLast, f)
	case *ForClause:
		Walk(node.Loop, f)
		walkList(node.Do, f)
		walkComments(node.DoLast, f)
	case *WordIter:
		Walk(node.Name, f)
		walkList(node.Items, f)
	case *CStyleLoop:
		walkNilable(node.Init, f)
		walkNilable(node.Cond, f)
		walkNilable(node.Post, f)
	case *BinaryCmd:
		Walk(node.X, f)
		Walk(node.Y, f)
	case *FuncDecl:
		walkNilable(node.Name, f)
		walkList(node.Names, f)
		Walk(node.Body, f)
	case *Word:
		walkList(node.Parts, f)
	case *Lit:
	case *SglQuoted:
	case *DblQuoted:
		walkList(node.Parts, f)
	case *CmdSubst:
		walkList(node.Stmts, f)
		walkComments(node.Last, f)
	case *ParamExp:
		walkNilable(node.Flags, f)
		walkNilable(node.Param, f)
		walkNilable(node.NestedParam, f)
		walkNilable(node.Index, f)
		if node.Slice != nil {
			walkNilable(node.Slice.Offset, f)
			walkNilable(node.Slice.Length, f)
		}
		if node.Repl != nil {
			walkNilable(node.Repl.Orig, f)
			walkNilable(node.Repl.With, f)
		}
		if node.Exp != nil {
			walkNilable(node.Exp.Word, f)
		}
	case *ArithmExp:
		Walk(node.X, f)
	case *ArithmCmd:
		Walk(node.X, f)
	case *BinaryArithm:
		Walk(node.X, f)
		Walk(node.Y, f)
	case *BinaryTest:
		Walk(node.X, f)
		Walk(node.Y, f)
	case *UnaryArithm:
		Walk(node.X, f)
	case *UnaryTest:
		Walk(node.X, f)
	case *ParenArithm:
		Walk(node.X, f)
	case *FlagsArithm:
		Walk(node.Flags, f)
		if node.X != nil {
			Walk(node.X, f)
		}
	case *ParenTest:
		Walk(node.X, f)
	case *CaseClause:
		Walk(node.Word, f)
		walkList(node.Items, f)
		walkComments(node.Last, f)
	case *CaseItem:
		for _, c := range node.Comments {
			if c.Pos().After(node.Pos()) {
				defer Walk(&c, f)
				break
			}
			Walk(&c, f)
		}
		walkList(node.Patterns, f)
		walkList(node.Stmts, f)
		walkComments(node.Last, f)
	case *TestClause:
		Walk(node.X, f)
	case *DeclClause:
		walkList(node.Args, f)
	case *ArrayExpr:
		walkList(node.Elems, f)
		walkComments(node.Last, f)
	case *ArrayElem:
		for _, c := range node.Comments {
			if c.Pos().After(node.Pos()) {
				defer Walk(&c, f)
				break
			}
			Walk(&c, f)
		}
		walkNilable(node.Index, f)
		walkNilable(node.Value, f)
	case *ExtGlob:
		Walk(node.Pattern, f)
	case *ProcSubst:
		walkList(node.Stmts, f)
		walkComments(node.Last, f)
	case *TimeClause:
		walkNilable(node.Stmt, f)
	case *CoprocClause:
		walkNilable(node.Name, f)
		Walk(node.Stmt, f)
	case *LetClause:
		walkList(node.Exprs, f)
	case *TestDecl:
		Walk(node.Description, f)
		Walk(node.Body, f)
	default:
		panic(fmt.Sprintf("syntax.Walk: unexpected node type %T", node))
	}

	f(nil)
}

type nilableNode interface {
	Node
	comparable // pointer nodes, which can be compared to nil
}

func walkNilable[N nilableNode](node N, f func(Node) bool) {
	var zero N // nil
	if node != zero {
		Walk(node, f)
	}
}

func walkList[N Node](list []N, f func(Node) bool) {
	for _, node := range list {
		Walk(node, f)
	}
}

func walkComments(list []Comment, f func(Node) bool) {
	// Note that []Comment does not satisfy the generic constraint []Node.
	for i := range list {
		Walk(&list[i], f)
	}
}

// DebugPrint prints the provided syntax tree, spanning multiple lines and with
// indentation. Can be useful to investigate the content of a syntax tree.
func DebugPrint(w io.Writer, node Node) error {
	p := debugPrinter{out: w}
	p.print(reflect.ValueOf(node))
	p.printf("\n")
	return p.err
}

type debugPrinter struct {
	out   io.Writer
	level int
	err   error
}

func (p *debugPrinter) printf(format string, args ...any) {
	_, err := fmt.Fprintf(p.out, format, args...)
	if err != nil && p.err == nil {
		p.err = err
	}
}

func (p *debugPrinter) newline() {
	p.printf("\n")
	for range p.level {
		p.printf(".  ")
	}
}

func (p *debugPrinter) print(x reflect.Value) {
	switch x.Kind() {
	case reflect.Interface:
		if x.IsNil() {
			p.printf("nil")
			return
		}
		p.print(x.Elem())
	case reflect.Pointer:
		if x.IsNil() {
			p.printf("nil")
			return
		}
		p.printf("*")
		p.print(x.Elem())
	case reflect.Slice:
		p.printf("%s (len = %d) {", x.Type(), x.Len())
		if x.Len() > 0 {
			p.level++
			p.newline()
			for i := range x.Len() {
				p.printf("%d: ", i)
				p.print(x.Index(i))
				if i == x.Len()-1 {
					p.level--
				}
				p.newline()
			}
		}
		p.printf("}")

	case reflect.Struct:
		if v, ok := x.Interface().(Pos); ok {
			if v.IsRecovered() {
				p.printf("<recovered>")
				return
			}
			p.printf("%v:%v", v.Line(), v.Col())
			return
		}
		t := x.Type()
		p.printf("%s {", t)
		p.level++
		p.newline()
		for i := range t.NumField() {
			p.printf("%s: ", t.Field(i).Name)
			p.print(x.Field(i))
			if i == x.NumField()-1 {
				p.level--
			}
			p.newline()
		}
		p.printf("}")
	default:
		if s, ok := x.Interface().(fmt.Stringer); ok && !x.IsZero() {
			p.printf("%#v (%s)", x.Interface(), s)
		} else {
			p.printf("%#v", x.Interface())
		}
	}
}
