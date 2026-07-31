// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"fmt"

	"mvdan.cc/sh/v3/syntax"
)

const illegalTok = 0

type testParser struct {
	eof bool
	val string
	rem []string

	err func(err error)
}

func (p *testParser) errf(format string, a ...any) {
	p.err(fmt.Errorf(format, a...))
}

func (p *testParser) next() {
	if p.eof || len(p.rem) == 0 {
		p.eof = true
		p.val = ""
		return
	}
	p.val = p.rem[0]
	p.rem = p.rem[1:]
}

func (p *testParser) followWord(fval string) *syntax.Word {
	if p.eof {
		p.errf("%s must be followed by a word", fval)
	}
	w := &syntax.Word{Parts: []syntax.WordPart{
		&syntax.Lit{Value: p.val},
	}}
	p.next()
	return w
}

func (p *testParser) classicTest(fval string, pastAndOr bool) syntax.TestExpr {
	var left syntax.TestExpr
	if pastAndOr {
		left = p.testExprBase(fval)
	} else {
		left = p.classicTest(fval, true)
	}
	if left == nil || p.eof || p.val == ")" {
		return left
	}
	opStr := p.val
	op := testBinaryOp(p.val)
	if op == illegalTok {
		p.errf("not a valid test operator: %#q", p.val)
	}
	b := &syntax.BinaryTest{
		Op: op,
		X:  left,
	}
	p.next()
	switch b.Op {
	case syntax.AndTest, syntax.OrTest:
		if b.Y = p.classicTest(opStr, false); b.Y == nil {
			p.errf("%s must be followed by an expression", opStr)
		}
	default:
		b.Y = p.followWord(opStr)
	}
	return b
}

func (p *testParser) testExprBase(fval string) syntax.TestExpr {
	if p.eof || p.val == ")" {
		return nil
	}
	op := testUnaryOp(p.val)
	switch op {
	case syntax.TsNot:
		u := &syntax.UnaryTest{Op: op}
		p.next()
		u.X = p.classicTest(op.String(), false)
		return u
	case syntax.TsParen:
		pe := &syntax.ParenTest{}
		p.next()
		pe.X = p.classicTest(op.String(), false)
		if p.val != ")" {
			p.errf("reached %s without matching '(' with ')'", p.val)
		}
		p.next()
		return pe
	case illegalTok:
		return p.followWord(fval)
	default:
		u := &syntax.UnaryTest{Op: op}
		p.next()
		if p.eof {
			// make [ -e ] fall back to [ -n -e ], i.e. use
			// the operator as an argument
			return &syntax.Word{Parts: []syntax.WordPart{
				&syntax.Lit{Value: op.String()},
			}}
		}
		u.X = p.followWord(op.String())
		return u
	}
}

// testUnaryOp is an exact copy of syntax's.
func testUnaryOp(val string) syntax.UnTestOperator {
	switch val {
	case "!":
		return syntax.TsNot
	case "(":
		return syntax.TsParen
	case "-e", "-a":
		return syntax.TsExists
	case "-f":
		return syntax.TsRegFile
	case "-d":
		return syntax.TsDirect
	case "-c":
		return syntax.TsCharSp
	case "-b":
		return syntax.TsBlckSp
	case "-p":
		return syntax.TsNmPipe
	case "-S":
		return syntax.TsSocket
	case "-L", "-h":
		return syntax.TsSmbLink
	case "-k":
		return syntax.TsSticky
	case "-g":
		return syntax.TsGIDSet
	case "-u":
		return syntax.TsUIDSet
	case "-G":
		return syntax.TsGrpOwn
	case "-O":
		return syntax.TsUsrOwn
	case "-N":
		return syntax.TsModif
	case "-r":
		return syntax.TsRead
	case "-w":
		return syntax.TsWrite
	case "-x":
		return syntax.TsExec
	case "-s":
		return syntax.TsNoEmpty
	case "-t":
		return syntax.TsFdTerm
	case "-z":
		return syntax.TsEmpStr
	case "-n":
		return syntax.TsNempStr
	case "-o":
		return syntax.TsOptSet
	case "-v":
		return syntax.TsVarSet
	case "-R":
		return syntax.TsRefVar
	default:
		return illegalTok
	}
}

// testBinaryOp is like syntax's, but with -a and -o, and without =~.
func testBinaryOp(val string) syntax.BinTestOperator {
	switch val {
	case "-a":
		return syntax.AndTest
	case "-o":
		return syntax.OrTest
	case "==", "=":
		return syntax.TsMatch
	case "!=":
		return syntax.TsNoMatch
	case "-nt":
		return syntax.TsNewer
	case "-ot":
		return syntax.TsOlder
	case "-ef":
		return syntax.TsDevIno
	case "-eq":
		return syntax.TsEql
	case "-ne":
		return syntax.TsNeq
	case "-le":
		return syntax.TsLeq
	case "-ge":
		return syntax.TsGeq
	case "-lt":
		return syntax.TsLss
	case "-gt":
		return syntax.TsGtr
	default:
		return illegalTok
	}
}
