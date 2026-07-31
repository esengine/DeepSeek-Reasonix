// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import "strings"

// Simplify modifies a node to remove redundant pieces of syntax, and returns
// whether any changes were made.
//
// The changes currently applied are:
//
//	Remove clearly useless parentheses       $(( (expr) ))
//	Remove dollars from vars in exprs        (($var))
//	Remove duplicate subshells               $( (stmts) )
//	Remove redundant quotes                  [[ "$var" == str ]]
//	Merge negations with unary operators     [[ ! -n $var ]]
//	Use single quotes to shorten literals    "\$foo"
func Simplify(n Node) bool {
	s := simplifier{}
	Walk(n, s.visit)
	return s.modified
}

type simplifier struct {
	modified bool
}

func (s *simplifier) visit(node Node) bool {
	switch node := node.(type) {
	case *Assign:
		node.Index = s.removeParensArithm(node.Index)
		// Don't inline params, as x[i] and x[$i] mean
		// different things when x is an associative
		// array; the first means "i", the second "$i".
	case *ParamExp:
		node.Index = s.removeParensArithm(node.Index)
		// don't inline params - same as above.

		if node.Slice == nil {
			break
		}
		node.Slice.Offset = s.removeParensArithm(node.Slice.Offset)
		node.Slice.Offset = s.inlineSimpleParams(node.Slice.Offset)
		node.Slice.Length = s.removeParensArithm(node.Slice.Length)
		node.Slice.Length = s.inlineSimpleParams(node.Slice.Length)
	case *ArithmExp:
		node.X = s.removeParensArithm(node.X)
		node.X = s.inlineSimpleParams(node.X)
	case *ArithmCmd:
		node.X = s.removeParensArithm(node.X)
		node.X = s.inlineSimpleParams(node.X)
	case *ParenArithm:
		node.X = s.removeParensArithm(node.X)
		node.X = s.inlineSimpleParams(node.X)
	case *BinaryArithm:
		node.X = s.inlineSimpleParams(node.X)
		node.Y = s.inlineSimpleParams(node.Y)
	case *CmdSubst:
		node.Stmts = s.inlineSubshell(node.Stmts)
	case *Subshell:
		node.Stmts = s.inlineSubshell(node.Stmts)
	case *Word:
		node.Parts = s.simplifyWord(node.Parts)
	case *TestClause:
		node.X = s.removeParensTest(node.X)
		node.X = s.removeNegateTest(node.X)
	case *ParenTest:
		node.X = s.removeParensTest(node.X)
		node.X = s.removeNegateTest(node.X)
	case *BinaryTest:
		node.X = s.unquoteParams(node.X)
		node.X = s.removeNegateTest(node.X)
		if node.Op == TsMatchShort {
			s.modified = true
			node.Op = TsMatch
		}
		switch node.Op {
		case TsMatch, TsNoMatch:
			// unquoting enables globbing
		default:
			node.Y = s.unquoteParams(node.Y)
		}
		node.Y = s.removeNegateTest(node.Y)
	case *UnaryTest:
		node.X = s.unquoteParams(node.X)
	}
	return true
}

func (s *simplifier) simplifyWord(wps []WordPart) []WordPart {
parts:
	for i, wp := range wps {
		dq, _ := wp.(*DblQuoted)
		if dq == nil || len(dq.Parts) != 1 {
			break
		}
		lit, _ := dq.Parts[0].(*Lit)
		if lit == nil {
			break
		}
		var sb strings.Builder
		escaped := false
		for _, r := range lit.Value {
			switch r {
			case '\\':
				escaped = !escaped
				if escaped {
					continue
				}
			case '\'':
				continue parts
			case '$', '"', '`':
				escaped = false
			default:
				if escaped {
					continue parts
				}
				escaped = false
			}
			sb.WriteRune(r)
		}
		newVal := sb.String()
		if newVal == lit.Value {
			break
		}
		s.modified = true
		wps[i] = &SglQuoted{
			Left:   dq.Pos(),
			Right:  dq.End(),
			Dollar: dq.Dollar,
			Value:  newVal,
		}
	}
	return wps
}

func (s *simplifier) removeParensArithm(x ArithmExpr) ArithmExpr {
	for {
		par, _ := x.(*ParenArithm)
		if par == nil {
			return x
		}
		s.modified = true
		x = par.X
	}
}

func (s *simplifier) inlineSimpleParams(x ArithmExpr) ArithmExpr {
	w, _ := x.(*Word)
	if w == nil || len(w.Parts) != 1 {
		return x
	}
	pe, _ := w.Parts[0].(*ParamExp)
	if pe == nil || pe.Param == nil || !ValidName(pe.Param.Value) {
		// Not a POSIX-like parameter expansion, or not a valid name without `$`, like $3.
		return x
	}
	if !pe.simple() {
		// A complex parameter expansion can't be simplified.
		//
		// Note that index expressions can't generally be simplified
		// either. It's fine to turn ${a[0]} into a[0], but others like
		// a[*] are invalid in many shells including Bash.
		return x
	}
	s.modified = true
	return &Word{Parts: []WordPart{pe.Param}}
}

func (s *simplifier) inlineSubshell(stmts []*Stmt) []*Stmt {
	for len(stmts) == 1 {
		st := stmts[0]
		if st.Negated || st.Background || st.Coprocess || st.Disown ||
			len(st.Redirs) > 0 {
			break
		}
		sub, _ := st.Cmd.(*Subshell)
		if sub == nil {
			break
		}
		s.modified = true
		stmts = sub.Stmts
	}
	return stmts
}

func (s *simplifier) unquoteParams(x TestExpr) TestExpr {
	w, _ := x.(*Word)
	if w == nil || len(w.Parts) != 1 {
		return x
	}
	dq, _ := w.Parts[0].(*DblQuoted)
	if dq == nil || len(dq.Parts) != 1 {
		return x
	}
	if _, ok := dq.Parts[0].(*ParamExp); !ok {
		return x
	}
	s.modified = true
	w.Parts = dq.Parts
	return w
}

func (s *simplifier) removeParensTest(x TestExpr) TestExpr {
	for {
		par, _ := x.(*ParenTest)
		if par == nil {
			return x
		}
		s.modified = true
		x = par.X
	}
}

func (s *simplifier) removeNegateTest(x TestExpr) TestExpr {
	u, _ := x.(*UnaryTest)
	if u == nil || u.Op != TsNot {
		return x
	}
	switch y := u.X.(type) {
	case *UnaryTest:
		switch y.Op {
		case TsEmpStr:
			y.Op = TsNempStr
			s.modified = true
			return y
		case TsNempStr:
			y.Op = TsEmpStr
			s.modified = true
			return y
		case TsNot:
			s.modified = true
			return y.X
		}
	case *BinaryTest:
		switch y.Op {
		case TsMatch:
			y.Op = TsNoMatch
			s.modified = true
			return y
		case TsNoMatch:
			y.Op = TsMatch
			s.modified = true
			return y
		}
	}
	return x
}
