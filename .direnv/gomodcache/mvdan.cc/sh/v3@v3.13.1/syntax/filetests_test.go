// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"reflect"
	"strings"
	"testing"
)

func lit(s string) *Lit { return &Lit{Value: s} }
func lits(strs ...string) []*Lit {
	l := make([]*Lit, 0, len(strs))
	for _, s := range strs {
		l = append(l, lit(s))
	}
	return l
}
func word(ps ...WordPart) *Word { return &Word{Parts: ps} }
func litWord(s string) *Word    { return word(lit(s)) }
func litWords(strs ...string) []*Word {
	l := make([]*Word, 0, len(strs))
	for _, s := range strs {
		l = append(l, litWord(s))
	}
	return l
}

func litAssigns(pairs ...string) []*Assign {
	l := make([]*Assign, len(pairs))
	for i, pair := range pairs {
		name, val, ok := strings.Cut(pair, "=")
		if !ok {
			l[i] = &Assign{Naked: true, Name: lit(name)}
		} else if val == "" {
			l[i] = &Assign{Name: lit(name)}
		} else {
			l[i] = &Assign{Name: lit(name), Value: litWord(val)}
		}
	}
	return l
}

func call(words ...*Word) *CallExpr    { return &CallExpr{Args: words} }
func litCall(strs ...string) *CallExpr { return call(litWords(strs...)...) }

func stmt(cmd Command) *Stmt { return &Stmt{Cmd: cmd} }
func stmts(cmds ...Command) []*Stmt {
	l := make([]*Stmt, len(cmds))
	for i, cmd := range cmds {
		l[i] = stmt(cmd)
	}
	return l
}

func litStmt(strs ...string) *Stmt { return stmt(litCall(strs...)) }
func litStmts(strs ...string) []*Stmt {
	l := make([]*Stmt, len(strs))
	for i, s := range strs {
		l[i] = litStmt(s)
	}
	return l
}

func sglQuoted(s string) *SglQuoted        { return &SglQuoted{Value: s} }
func sglDQuoted(s string) *SglQuoted       { return &SglQuoted{Dollar: true, Value: s} }
func dblQuoted(ps ...WordPart) *DblQuoted  { return &DblQuoted{Parts: ps} }
func dblDQuoted(ps ...WordPart) *DblQuoted { return &DblQuoted{Dollar: true, Parts: ps} }
func block(sts ...*Stmt) *Block            { return &Block{Stmts: sts} }
func subshell(sts ...*Stmt) *Subshell      { return &Subshell{Stmts: sts} }
func arithmExp(e ArithmExpr) *ArithmExp    { return &ArithmExp{X: e} }
func arithmExpBr(e ArithmExpr) *ArithmExp  { return &ArithmExp{Bracket: true, X: e} }
func arithmCmd(e ArithmExpr) *ArithmCmd    { return &ArithmCmd{X: e} }
func parenArit(e ArithmExpr) *ParenArithm  { return &ParenArithm{X: e} }
func parenTest(e TestExpr) *ParenTest      { return &ParenTest{X: e} }

func cmdSubst(sts ...*Stmt) *CmdSubst { return &CmdSubst{Stmts: sts} }
func litParamExp(s string) *ParamExp {
	return &ParamExp{Short: true, Param: lit(s)}
}

func letClause(exps ...ArithmExpr) *LetClause {
	return &LetClause{Exprs: exps}
}

func arrValues(words ...*Word) *ArrayExpr {
	ae := &ArrayExpr{}
	for _, w := range words {
		ae.Elems = append(ae.Elems, &ArrayElem{Value: w})
	}
	return ae
}

func fullProg(v any) *File {
	f := &File{}
	switch v := v.(type) {
	case *File:
		return v
	case []*Stmt:
		f.Stmts = v
		return f
	case *Stmt:
		f.Stmts = append(f.Stmts, v)
		return f
	case []Command:
		for _, cmd := range v {
			f.Stmts = append(f.Stmts, stmt(cmd))
		}
		return f
	case *Word:
		return fullProg(call(v))
	case WordPart:
		return fullProg(word(v))
	case Command:
		return fullProg(stmt(v))
	case nil:
	default:
		panic(reflect.TypeOf(v))
	}
	return nil
}

type fileTestCase struct {
	inputs []string // input sources; the first is the canonical formatting

	// Each language in [langResolvedVariants] has an entry:
	// - nil:    nothing to test
	// - *File:  parse as the given syntax tree
	// - string: parse error with the given string, substituting LANG
	byLangIndex [langResolvedVariantsCount]any

	// The real shells where testing the input succeeds or fails in the opposite way.
	flipConfirmSet LangVariant
}

func flipConfirm2(langSet LangVariant) func(*fileTestCase) {
	return func(c *fileTestCase) { c.flipConfirmSet = langSet }
}

func (c *fileTestCase) setForLangs(val any, langSets ...LangVariant) {
	// The parameter is a slice to allow omitting the argument.
	switch len(langSets) {
	case 0:
		for i := range c.byLangIndex {
			c.byLangIndex[i] = val
		}
		return
	case 1:
		for lang := range langSets[0].bits() {
			c.byLangIndex[lang.index()] = val
		}
	default:
		panic("use a LangVariant bitset")
	}
}

func fileTest(in []string, opts ...func(*fileTestCase)) fileTestCase {
	c := fileTestCase{inputs: in}
	for _, o := range opts {
		o(&c)
	}
	return c
}

func langSkip(langSets ...LangVariant) func(*fileTestCase) {
	return func(c *fileTestCase) { c.setForLangs(nil, langSets...) }
}

func langFile(wantNode any, langSets ...LangVariant) func(*fileTestCase) {
	return func(c *fileTestCase) {
		c.setForLangs(fullProg(wantNode), langSets...)
	}
}

func langErr2(wantErr string, langSets ...LangVariant) func(*fileTestCase) {
	return func(c *fileTestCase) { c.setForLangs(wantErr, langSets...) }
}

var fileTests = []fileTestCase{
	fileTest(
		[]string{"", " ", "\t", "\n \n", "\r \r\n"},
		langFile(&File{}),
	),
	fileTest(
		[]string{"", "# foo", "# foo ( bar", "# foo'bar"},
		langFile(&File{}),
	),
	fileTest(
		[]string{"foo", "foo ", " foo", "foo # bar"},
		langFile(litWord("foo")),
	),
	fileTest(
		[]string{`\`},
		langFile(litWord(`\`)),
	),
	fileTest(
		[]string{`foo\`, "f\\\noo\\"},
		langFile(litWord(`foo\`)),
	),
	fileTest(
		[]string{`foo\a`, "f\\\noo\\a"},
		langFile(litWord(`foo\a`)),
	),
	fileTest(
		[]string{
			"foo\nbar",
			"foo; bar;",
			"foo;bar;",
			"\nfoo\nbar\n",
			"foo\r\nbar\r\n",
		},
		langFile(litStmts("foo", "bar")),
	),
	fileTest(
		[]string{"foo a b", " foo  a  b ", "foo \\\n a b", "foo \\\r\n a b"},
		langFile(litCall("foo", "a", "b")),
	),
	fileTest(
		[]string{"foobar", "foo\\\nbar", "foo\\\nba\\\nr"},
		langFile(litWord("foobar")),
	),
	fileTest(
		[]string{"foo", "foo \\\n", "foo \\\r\n"},
		langFile(litWord("foo")),
	),
	fileTest(
		[]string{"foo'bar'"},
		langFile(word(lit("foo"), sglQuoted("bar"))),
	),
	fileTest(
		[]string{"(foo)", "(foo;)", "(\nfoo\n)"},
		langFile(subshell(litStmt("foo"))),
	),
	fileTest(
		[]string{"(\n\tfoo\n\tbar\n)", "(foo; bar)"},
		langFile(subshell(litStmt("foo"), litStmt("bar"))),
	),
	fileTest(
		[]string{"{ foo; }", "{\nfoo\n}"},
		langFile(block(litStmt("foo"))),
	),
	fileTest(
		[]string{
			"{ if a; then b; fi; }",
			"{ if a; then b; fi }",
		},
		langFile(block(stmt(&IfClause{
			Cond: litStmts("a"),
			Then: litStmts("b"),
		}))),
	),
	fileTest(
		[]string{
			"if a; then b; fi",
			"if a\nthen\nb\nfi",
			"if a;\nthen\nb\nfi",
			"if a \nthen\nb\nfi",
			"if\x00 a; th\x00en b; \x00fi",
		},
		langFile(&IfClause{
			Cond: litStmts("a"),
			Then: litStmts("b"),
		}),
	),
	fileTest(
		[]string{
			"if a; then b; else c; fi",
			"if a\nthen b\nelse\nc\nfi",
		},
		langFile(&IfClause{
			Cond: litStmts("a"),
			Then: litStmts("b"),
			Else: &IfClause{
				Then: litStmts("c"),
			},
		}),
	),
	fileTest(
		[]string{
			"if a; then a; elif b; then b; else c; fi",
		},
		langFile(&IfClause{
			Cond: litStmts("a"),
			Then: litStmts("a"),
			Else: &IfClause{
				Cond: litStmts("b"),
				Then: litStmts("b"),
				Else: &IfClause{
					Then: litStmts("c"),
				},
			},
		}),
	),
	fileTest(
		[]string{
			"if a; then a; elif b; then b; elif c; then c; else d; fi",
			"if a\nthen a\nelif b\nthen b\nelif c\nthen c\nelse\nd\nfi",
		},
		langFile(&IfClause{
			Cond: litStmts("a"),
			Then: litStmts("a"),
			Else: &IfClause{
				Cond: litStmts("b"),
				Then: litStmts("b"),
				Else: &IfClause{
					Cond: litStmts("c"),
					Then: litStmts("c"),
					Else: &IfClause{
						Then: litStmts("d"),
					},
				},
			},
		}),
	),
	fileTest(
		[]string{
			"if\n\ta1\n\ta2 foo\n\ta3 bar\nthen b; fi",
			"if a1; a2 foo; a3 bar; then b; fi",
		},
		langFile(&IfClause{
			Cond: []*Stmt{
				litStmt("a1"),
				litStmt("a2", "foo"),
				litStmt("a3", "bar"),
			},

			Then: litStmts("b"),
		}),
	),
	fileTest(
		[]string{`((a == 2))`},
		langFile(arithmCmd(&BinaryArithm{
			Op: Eql,
			X:  litWord("a"),
			Y:  litWord("2"),
		}), LangBash|LangMirBSDKorn|LangZsh),
		langFile(subshell(stmt(subshell(litStmt("a", "==", "2")))), LangPOSIX),
	),
	fileTest(
		[]string{"if (($# > 2)); then b; fi"},
		langFile(&IfClause{
			Cond: stmts(arithmCmd(&BinaryArithm{
				Op: Gtr,
				X:  word(litParamExp("#")),
				Y:  litWord("2"),
			})),
			Then: litStmts("b"),
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{
			"(($(date -u) > DATE))",
			"((`date -u` > DATE))",
		},
		langFile(arithmCmd(&BinaryArithm{
			Op: Gtr,
			X:  word(cmdSubst(litStmt("date", "-u"))),
			Y:  litWord("DATE"),
		}), LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{": $((0x$foo == 10))"},
		langFile(call(
			litWord(":"),
			word(arithmExp(&BinaryArithm{
				Op: Eql,
				X:  word(lit("0x"), litParamExp("foo")),
				Y:  litWord("10"),
			})),
		)),
	),
	fileTest(
		[]string{"((# 1 + 2))", "(( # 1 + 2 ))"},
		langFile(&ArithmCmd{
			X: &BinaryArithm{
				Op: Add,
				X:  litWord("1"),
				Y:  litWord("2"),
			},
			Unsigned: true,
		}, LangMirBSDKorn),
		langErr2("1:1: unsigned expressions are a mksh feature; tried parsing as LANG", LangBash),
	),
	fileTest(
		[]string{"$((# 1 + 2))", "$(( # 1 + 2 ))"},
		langFile(&ArithmExp{
			X: &BinaryArithm{
				Op: Add,
				X:  litWord("1"),
				Y:  litWord("2"),
			},
			Unsigned: true,
		}, LangMirBSDKorn),
		langErr2("1:1: unsigned expressions are a mksh feature; tried parsing as LANG", LangBash),
	),
	fileTest(
		[]string{"((3#20))"},
		langFile(arithmCmd(litWord("3#20")), LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"((1.2 > 0.3))"},
		langFile(arithmCmd(&BinaryArithm{
			Op: Gtr,
			X:  litWord("1.2"),
			Y:  litWord("0.3"),
		}), LangZsh),
		langErr2("1:4: floating point arithmetic is a zsh feature; tried parsing as LANG", LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{
			"while a; do b; done",
			"wh\\\nile a; do b; done",
			"wh\\\r\nile a; do b; done",
			"while a\ndo\nb\ndone",
			"while a;\ndo\nb\ndone",
		},
		langFile(&WhileClause{
			Cond: litStmts("a"),
			Do:   litStmts("b"),
		}),
	),
	fileTest(
		[]string{"while { a; }; do b; done", "while { a; } do b; done"},
		langFile(&WhileClause{
			Cond: stmts(block(litStmt("a"))),
			Do:   litStmts("b"),
		}),
	),
	fileTest(
		[]string{"while (a); do b; done", "while (a) do b; done"},
		langFile(&WhileClause{
			Cond: stmts(subshell(litStmt("a"))),
			Do:   litStmts("b"),
		}),
	),
	fileTest(
		[]string{"while ((1 > 2)); do b; done"},
		langFile(&WhileClause{
			Cond: stmts(arithmCmd(&BinaryArithm{
				Op: Gtr,
				X:  litWord("1"),
				Y:  litWord("2"),
			})),
			Do: litStmts("b"),
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"until a; do b; done", "until a\ndo\nb\ndone"},
		langFile(&WhileClause{
			Until: true,
			Cond:  litStmts("a"),
			Do:    litStmts("b"),
		}),
	),
	fileTest(
		[]string{
			"for i; do foo; done",
			"for i do foo; done",
			"for i\ndo foo\ndone",
			"for i;\ndo foo\ndone",
			"for i in; do foo; done",
		},
		langFile(&ForClause{
			Loop: &WordIter{Name: lit("i")},
			Do:   litStmts("foo"),
		}),
	),
	fileTest(
		[]string{
			"for i in 1 2 3; do echo $i; done",
			"for i in 1 2 3\ndo echo $i\ndone",
			"for i in 1 2 3;\ndo echo $i\ndone",
			"for i in 1 2 3 #foo\ndo echo $i\ndone",
		},
		langFile(&ForClause{
			Loop: &WordIter{
				Name:  lit("i"),
				Items: litWords("1", "2", "3"),
			},
			Do: stmts(call(
				litWord("echo"),
				word(litParamExp("i")),
			)),
		}),
	),
	fileTest(
		[]string{
			"for i in \\\n\t1 2 3; do #foo\n\techo $i\ndone",
			"for i #foo\n\tin 1 2 3; do\n\techo $i\ndone",
		},
		langFile(&ForClause{
			Loop: &WordIter{
				Name:  lit("i"),
				Items: litWords("1", "2", "3"),
			},
			Do: stmts(call(
				litWord("echo"),
				word(litParamExp("i")),
			)),
		}),
	),
	fileTest(
		[]string{
			"for i; do foo; done",
			"for i; { foo; }",
		},
		langFile(&ForClause{
			Loop: &WordIter{Name: lit("i")},
			Do:   litStmts("foo"),
		}, LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{
			"for i in 1 2 3; do echo $i; done",
			"for i in 1 2 3; { echo $i; }",
		},
		langFile(&ForClause{
			Loop: &WordIter{
				Name:  lit("i"),
				Items: litWords("1", "2", "3"),
			},
			Do: stmts(call(
				litWord("echo"),
				word(litParamExp("i")),
			)),
		}, LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{
			"for ((i = 0; i < 10; i++)); do echo $i; done",
			"for ((i=0;i<10;i++)) do echo $i; done",
			"for (( i = 0 ; i < 10 ; i++ ))\ndo echo $i\ndone",
			"for (( i = 0 ; i < 10 ; i++ ));\ndo echo $i\ndone",
		},
		langFile(&ForClause{
			Loop: &CStyleLoop{
				Init: &BinaryArithm{
					Op: Assgn,
					X:  litWord("i"),
					Y:  litWord("0"),
				},
				Cond: &BinaryArithm{
					Op: Lss,
					X:  litWord("i"),
					Y:  litWord("10"),
				},
				Post: &UnaryArithm{
					Op:   Inc,
					Post: true,
					X:    litWord("i"),
				},
			},
			Do: stmts(call(
				litWord("echo"),
				word(litParamExp("i")),
			)),
		}, LangBash|LangZsh),
		langErr2("1:5: c-style fors are a bash/zsh feature; tried parsing as LANG", LangPOSIX|LangMirBSDKorn),
	),
	fileTest(
		[]string{
			"for (( ; ; )); do foo; done",
			"for ((;;)); do foo; done",
		},
		langFile(&ForClause{
			Loop: &CStyleLoop{},
			Do:   litStmts("foo"),
		}, LangBash),
	),
	fileTest(
		[]string{
			"for ((i = 0; ; )); do foo; done",
			"for ((i = 0;;)); do foo; done",
		},
		langFile(&ForClause{
			Loop: &CStyleLoop{
				Init: &BinaryArithm{
					Op: Assgn,
					X:  litWord("i"),
					Y:  litWord("0"),
				},
			},
			Do: litStmts("foo"),
		}, LangBash),
	),
	fileTest(
		[]string{
			"select i; do foo; done",
			"select i in; do foo; done",
		},
		langFile(&ForClause{
			Select: true,
			Loop:   &WordIter{Name: lit("i")},
			Do:     litStmts("foo"),
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{
			"select i in 1 2 3; do echo $i; done",
			"select i in 1 2 3\ndo echo $i\ndone",
			"select i in 1 2 3 #foo\ndo echo $i\ndone",
		},
		langFile(&ForClause{
			Select: true,
			Loop: &WordIter{
				Name:  lit("i"),
				Items: litWords("1", "2", "3"),
			},
			Do: stmts(call(
				litWord("echo"),
				word(litParamExp("i")),
			)),
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"select foo bar"},
		langFile(litStmt("select", "foo", "bar"), LangPOSIX),
	),
	fileTest(
		[]string{`' ' "foo bar"`},
		langFile(call(
			word(sglQuoted(" ")),
			word(dblQuoted(lit("foo bar"))),
		)),
	),
	fileTest(
		[]string{`"foo \" bar"`},
		langFile(word(dblQuoted(lit(`foo \" bar`)))),
	),
	fileTest(
		[]string{"\">foo\" \"\nbar\""},
		langFile(call(
			word(dblQuoted(lit(">foo"))),
			word(dblQuoted(lit("\nbar"))),
		)),
	),
	fileTest(
		[]string{`foo \" bar`},
		langFile(litCall(`foo`, `\"`, `bar`)),
	),
	fileTest(
		[]string{`'"'`},
		langFile(sglQuoted(`"`)),
	),
	fileTest(
		[]string{"'`'"},
		langFile(sglQuoted("`")),
	),
	fileTest(
		[]string{`"'"`},
		langFile(dblQuoted(lit("'"))),
	),
	fileTest(
		[]string{`""`},
		langFile(dblQuoted()),
	),
	fileTest(
		[]string{"=a s{s s=s"},
		langFile(litCall("=a", "s{s", "s=s")),
		langSkip(LangZsh),
	),
	fileTest(
		[]string{"foo && bar", "foo&&bar", "foo &&\nbar"},
		langFile(&BinaryCmd{
			Op: AndStmt,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		}),
	),
	fileTest(
		[]string{"foo &&\n\tbar"},
		langFile(&BinaryCmd{
			Op: AndStmt,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		}),
	),
	fileTest(
		[]string{"foo || bar", "foo||bar", "foo ||\nbar"},
		langFile(&BinaryCmd{
			Op: OrStmt,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		}),
	),
	fileTest(
		[]string{"if a; then b; fi || while a; do b; done"},
		langFile(&BinaryCmd{
			Op: OrStmt,
			X: stmt(&IfClause{
				Cond: litStmts("a"),
				Then: litStmts("b"),
			}),
			Y: stmt(&WhileClause{
				Cond: litStmts("a"),
				Do:   litStmts("b"),
			}),
		}),
	),
	fileTest(
		[]string{"foo && bar1 || bar2"},
		langFile(&BinaryCmd{
			Op: OrStmt,
			X: stmt(&BinaryCmd{
				Op: AndStmt,
				X:  litStmt("foo"),
				Y:  litStmt("bar1"),
			}),
			Y: litStmt("bar2"),
		}),
	),
	fileTest(
		[]string{"a || b || c || d"},
		langFile(&BinaryCmd{
			Op: OrStmt,
			X: stmt(&BinaryCmd{
				Op: OrStmt,
				X: stmt(&BinaryCmd{
					Op: OrStmt,
					X:  litStmt("a"),
					Y:  litStmt("b"),
				}),
				Y: litStmt("c"),
			}),
			Y: litStmt("d"),
		}),
	),
	fileTest(
		[]string{"foo | bar", "foo|bar", "foo |\n#etc\nbar"},
		langFile(&BinaryCmd{
			Op: Pipe,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		}),
	),
	fileTest(
		[]string{"foo | bar | extra"},
		langFile(&BinaryCmd{
			Op: Pipe,
			X: stmt(&BinaryCmd{
				Op: Pipe,
				X:  litStmt("foo"),
				Y:  litStmt("bar"),
			}),
			Y: litStmt("extra"),
		}),
	),
	fileTest(
		[]string{"foo | a=b bar"},
		langFile(&BinaryCmd{
			Op: Pipe,
			X:  litStmt("foo"),
			Y: stmt(&CallExpr{
				Assigns: litAssigns("a=b"),
				Args:    litWords("bar"),
			}),
		}),
	),
	fileTest(
		[]string{"foo |&"},
		langFile(&Stmt{Cmd: litCall("foo"), Coprocess: true}, LangMirBSDKorn),
	),
	fileTest(
		[]string{"foo \\\n\t|&"},
		langFile(&Stmt{Cmd: litCall("foo"), Coprocess: true}, LangMirBSDKorn),
	),
	fileTest(
		[]string{"foo |& bar", "foo|&bar"},
		langFile(&BinaryCmd{
			Op: PipeAll,
			X:  litStmt("foo"),
			Y:  litStmt("bar"),
		}, LangBash|LangZsh),
		langFile([]*Stmt{
			{Cmd: litCall("foo"), Coprocess: true},
			litStmt("bar"),
		}, LangMirBSDKorn),
	),
	fileTest(
		[]string{"foo &|", "foo &!"},
		langFile(&Stmt{Cmd: litCall("foo"), Disown: true}, LangZsh),
	),
	fileTest(
		[]string{"foo &|\nbar"},
		langFile([]*Stmt{
			{Cmd: litCall("foo"), Disown: true},
			litStmt("bar"),
		}, LangZsh),
	),
	fileTest(
		[]string{
			"foo() {\n\ta\n\tb\n}",
			"foo() { a; b; }",
			"foo ( ) {\na\nb\n}",
			"foo()\n{\na\nb\n}",
		},
		langFile(&FuncDecl{
			Parens: true,
			Name:   lit("foo"),
			Body:   stmt(block(litStmt("a"), litStmt("b"))),
		}),
		langSkip(LangZsh), // fails on foo ( )
	),
	fileTest(
		[]string{"foo() { a; }\nbar", "foo() {\na\n}; bar"},
		langFile([]Command{
			&FuncDecl{
				Parens: true,
				Name:   lit("foo"),
				Body:   stmt(block(litStmt("a"))),
			},
			litCall("bar"),
		}),
	),
	fileTest(
		[]string{"foO_123() { a; }"},
		langFile(&FuncDecl{
			Parens: true,
			Name:   lit("foO_123"),
			Body:   stmt(block(litStmt("a"))),
		}),
	),
	fileTest(
		[]string{"-foo_.,+-bar() { a; }"},
		langFile(&FuncDecl{
			Parens: true,
			Name:   lit("-foo_.,+-bar"),
			Body:   stmt(block(litStmt("a"))),
		}, LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{
			"function foo() {\n\ta\n\tb\n}",
			"function foo() { a; b; }",
		},
		langFile(&FuncDecl{
			RsrvWord: true,
			Parens:   true,
			Name:     lit("foo"),
			Body:     stmt(block(litStmt("a"), litStmt("b"))),
		}, LangBash|LangMirBSDKorn|LangZsh),
		langErr2("1:13: the `function` builtin is a bash feature; tried parsing as LANG", LangPOSIX),
	),
	fileTest(
		[]string{
			"function foo {\n\ta\n\tb\n}",
			"function foo { a; b; }",
		},
		langFile(&FuncDecl{
			RsrvWord: true,
			Name:     lit("foo"),
			Body:     stmt(block(litStmt("a"), litStmt("b"))),
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"function foo() (a)"},
		langFile(&FuncDecl{
			RsrvWord: true,
			Parens:   true,
			Name:     lit("foo"),
			Body:     stmt(subshell(litStmt("a"))),
		}, LangBash),
		langErr2("1:13: the `function` builtin is a bash feature; tried parsing as LANG", LangPOSIX),
	),
	fileTest(
		[]string{"function f1 f2 f3() {\n\ta\n}"},
		langFile(&FuncDecl{
			RsrvWord: true,
			Parens:   true,
			Names:    lits("f1", "f2", "f3"),
			Body:     stmt(block(litStmt("a"))),
		}, LangZsh),
	),
	fileTest(
		[]string{"function f1 f2 f3() {\n\ta\n}"},
		langFile(&FuncDecl{
			RsrvWord: true,
			Parens:   true,
			Names:    lits("f1", "f2", "f3"),
			Body:     stmt(block(litStmt("a"))),
		}, LangZsh),
		langErr2("1:1: multi-name functions are a zsh feature; tried parsing as LANG", LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{"function {\n\ta\n}"},
		langFile(&FuncDecl{
			RsrvWord: true,
			Body:     stmt(block(litStmt("a"))),
		}, LangZsh),
		langErr2("1:1: anonymous functions are a zsh feature; tried parsing as LANG", LangBash|LangMirBSDKorn),
	),
	// Note that zsh also supports `f1 f2 f3 () { body; }`,
	// but it seems rare and hard to implement well,
	// so leave it out for now.
	fileTest(
		[]string{"() {\n\ta\n}"},
		langFile(&FuncDecl{
			Parens: true,
			Body:   stmt(block(litStmt("a"))),
		}, LangZsh),
		langErr2("1:1: anonymous functions are a zsh feature; tried parsing as LANG", LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{"a=b foo=$bar foo=start$bar"},
		langFile(&CallExpr{
			Assigns: []*Assign{
				{Name: lit("a"), Value: litWord("b")},
				{Name: lit("foo"), Value: word(litParamExp("bar"))},
				{Name: lit("foo"), Value: word(
					lit("start"),
					litParamExp("bar"),
				)},
			},
		}),
	),
	fileTest(
		[]string{"a=\"\nbar\""},
		langFile(&CallExpr{
			Assigns: []*Assign{{
				Name:  lit("a"),
				Value: word(dblQuoted(lit("\nbar"))),
			}},
		}),
	),
	fileTest(
		[]string{"A_3a= foo"},
		langFile(&CallExpr{
			Assigns: litAssigns("A_3a="),
			Args:    litWords("foo"),
		}),
	),
	fileTest(
		[]string{"a=b=c"},
		langFile(&CallExpr{
			Assigns: litAssigns("a=b=c"),
		}),
	),
	fileTest(
		[]string{"à=b foo"},
		langFile(litStmt("à=b", "foo")),
	),
	fileTest(
		[]string{
			"foo >a >>b <c",
			"foo > a >> b < c",
			">a >>b <c foo",
		},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("a")},
				{Op: AppOut, Word: litWord("b")},
				{Op: RdrIn, Word: litWord("c")},
			},
		}),
	),
	fileTest(
		[]string{
			"foo bar >a",
			"foo >a bar",
		},
		langFile(&Stmt{
			Cmd: litCall("foo", "bar"),
			Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("a")},
			},
		}),
	),
	fileTest(
		[]string{`>a >\b`},
		langFile(&Stmt{
			Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("a")},
				{Op: RdrOut, Word: litWord(`\b`)},
			},
		}),
	),
	fileTest(
		[]string{">a\n>b", ">a; >b"},
		langFile([]*Stmt{
			{Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("a")},
			}},
			{Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("b")},
			}},
		}),
	),
	fileTest(
		[]string{"foo1\nfoo2 >r2", "foo1; >r2 foo2"},
		langFile([]*Stmt{
			litStmt("foo1"),
			{
				Cmd: litCall("foo2"),
				Redirs: []*Redirect{
					{Op: RdrOut, Word: litWord("r2")},
				},
			},
		}),
	),
	fileTest(
		[]string{"foo >bar$(etc)", "foo >b\\\nar`etc`"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{Op: RdrOut, Word: word(
					lit("bar"),
					cmdSubst(litStmt("etc")),
				)},
			},
		}),
	),
	fileTest(
		[]string{
			"a=b c=d foo >x <y",
			"a=b c=d >x <y foo",
			">x a=b c=d <y foo",
			">x <y a=b c=d foo",
			"a=b >x c=d foo <y",
		},
		langFile(&Stmt{
			Cmd: &CallExpr{
				Assigns: litAssigns("a=b", "c=d"),
				Args:    litWords("foo"),
			},
			Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("x")},
				{Op: RdrIn, Word: litWord("y")},
			},
		}),
	),
	fileTest(
		[]string{
			"foo <<EOF\nbar\nEOF",
			"foo <<EOF \nbar\nEOF",
			"foo <<EOF\t\nbar\nEOF",
			"foo <<EOF\r\nbar\r\nEOF\r\n",
		},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("bar\n"),
			}},
		}),
	),
	fileTest(
		[]string{"foo <<EOF\n\nbar\nEOF"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("\nbar\n"),
			}},
		}),
	),
	fileTest(
		[]string{"foo <<EOF\nbar\n\nEOF"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("bar\n\n"),
			}},
		}),
	),
	fileTest(
		[]string{"foo <<EOF\n1\n2\n3\nEOF"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("1\n2\n3\n"),
			}},
		}),
	),
	fileTest(
		[]string{"a <<EOF\nfoo$bar\nEOF"},
		langFile(&Stmt{
			Cmd: litCall("a"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: word(
					lit("foo"),
					litParamExp("bar"),
					lit("\n"),
				),
			}},
		}),
	),
	fileTest(
		[]string{"a <<EOF\n\"$bar\"\nEOF"},
		langFile(&Stmt{
			Cmd: litCall("a"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: word(
					lit(`"`),
					litParamExp("bar"),
					lit("\"\n"),
				),
			}},
		}),
	),
	fileTest(
		[]string{"a <<EOF\n$''$bar\nEOF"},
		langFile(&Stmt{
			Cmd: litCall("a"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: word(
					lit("$"),
					lit("''"),
					litParamExp("bar"),
					lit("\n"),
				),
			}},
		}, LangBash),
	),
	fileTest(
		[]string{
			"a <<EOF\n$(b)\nc\nEOF",
			"a <<EOF\n`b`\nc\nEOF",
		},
		langFile(&Stmt{
			Cmd: litCall("a"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: word(
					cmdSubst(litStmt("b")),
					lit("\nc\n"),
				),
			}},
		}),
	),
	fileTest(
		[]string{
			"a <<EOF\nfoo$(bar)baz\nEOF",
			"a <<EOF\nfoo`bar`baz\nEOF",
		},
		langFile(&Stmt{
			Cmd: litCall("a"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: word(
					lit("foo"),
					cmdSubst(litStmt("bar")),
					lit("baz\n"),
				),
			}},
		}),
	),
	fileTest(
		[]string{"a <<EOF\n\\${\nEOF"},
		langFile(&Stmt{
			Cmd: litCall("a"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("\\${\n"),
			}},
		}),
	),
	fileTest(
		[]string{
			"{\n\tfoo <<EOF\nbar\nEOF\n}",
			"{ foo <<EOF\nbar\nEOF\n}",
		},
		langFile(block(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("bar\n"),
			}},
		})),
	),
	fileTest(
		[]string{
			"$(\n\tfoo <<EOF\nbar\nEOF\n)",
			"$(foo <<EOF\nbar\nEOF\n)",
			"`\nfoo <<EOF\nbar\nEOF\n`",
			"`foo <<EOF\nbar\nEOF`",
		},
		langFile(cmdSubst(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("bar\n"),
			}},
		})),
	),
	fileTest(
		[]string{
			"foo <<EOF\nbar\nEOF$(oops)\nEOF",
			"foo <<EOF\nbar\nEOF`oops`\nEOF",
		},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: word(
					lit("bar\nEOF"),
					cmdSubst(litStmt("oops")),
					lit("\n"),
				),
			}},
		}),
	),
	fileTest(
		[]string{
			"foo <<EOF\nbar\nNOTEOF$(oops)\nEOF",
			"foo <<EOF\nbar\nNOTEOF`oops`\nEOF",
		},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: word(
					lit("bar\nNOTEOF"),
					cmdSubst(litStmt("oops")),
					lit("\n"),
				),
			}},
		}),
	),
	fileTest(
		[]string{
			"$(\n\tfoo <<'EOF'\nbar\nEOF\n)",
			"$(foo <<'EOF'\nbar\nEOF\n)",
			"`\nfoo <<'EOF'\nbar\nEOF\n`",
			"`foo <<'EOF'\nbar\nEOF`",
		},
		langFile(cmdSubst(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: word(sglQuoted("EOF")),
				Hdoc: litWord("bar\n"),
			}},
		})),
	),
	fileTest(
		[]string{"foo <<'EOF'\nbar\nEOF`oops`\nEOF"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: word(sglQuoted("EOF")),
				Hdoc: litWord("bar\nEOF`oops`\n"),
			}},
		}),
	),
	fileTest(
		[]string{"foo <<'EOF'\nbar\nNOTEOF`oops`\nEOF"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: word(sglQuoted("EOF")),
				Hdoc: litWord("bar\nNOTEOF`oops`\n"),
			}},
		}),
	),
	fileTest(
		[]string{"$(<foo)", "`<foo`"},
		langFile(cmdSubst(&Stmt{
			Redirs: []*Redirect{{
				Op:   RdrIn,
				Word: litWord("foo"),
			}},
		})),
		langSkip(LangZsh), // actually tries to read foo when confirming
	),
	fileTest(
		[]string{"foo <<EOF >f\nbar\nEOF"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{
					Op:   Hdoc,
					Word: litWord("EOF"),
					Hdoc: litWord("bar\n"),
				},
				{Op: RdrOut, Word: litWord("f")},
			},
		}),
	),
	fileTest(
		[]string{"foo <<EOF && {\nbar\nEOF\n\tetc\n}"},
		langFile(&BinaryCmd{
			Op: AndStmt,
			X: &Stmt{
				Cmd: litCall("foo"),
				Redirs: []*Redirect{{
					Op:   Hdoc,
					Word: litWord("EOF"),
					Hdoc: litWord("bar\n"),
				}},
			},
			Y: stmt(block(litStmt("etc"))),
		}),
	),
	fileTest(
		[]string{
			"$(\n\tfoo\n) <<EOF\nbar\nEOF",
			"<<EOF $(\n\tfoo\n)\nbar\nEOF",
		},
		// note that dash won't accept the second one
		langFile(&Stmt{
			Cmd: call(word(cmdSubst(litStmt("foo")))),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("bar\n"),
			}},
		}, LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{
			"$(\n\tfoo\n) <<EOF\nbar\nEOF",
			"`\n\tfoo\n` <<EOF\nbar\nEOF",
			"<<EOF `\n\tfoo\n`\nbar\nEOF",
		},
		langFile(&Stmt{
			Cmd: call(word(cmdSubst(litStmt("foo")))),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("bar\n"),
			}},
		}),
	),
	fileTest(
		[]string{
			"$((foo)) <<EOF\nbar\nEOF",
			"<<EOF $((\n\tfoo\n))\nbar\nEOF",
		},
		langFile(&Stmt{
			Cmd: call(word(arithmExp(litWord("foo")))),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("bar\n"),
			}},
		}),
	),
	fileTest(
		[]string{"if true; then\n\tfoo <<-EOF\n\t\tbar\n\tEOF\nfi"},
		langFile(&IfClause{
			Cond: litStmts("true"),
			Then: []*Stmt{{
				Cmd: litCall("foo"),
				Redirs: []*Redirect{{
					Op:   DashHdoc,
					Word: litWord("EOF"),
					Hdoc: litWord("\t\tbar\n\t"),
				}},
			}},
		}),
	),
	fileTest(
		[]string{"if true; then\n\tfoo <<-EOF\n\tEOF\nfi"},
		langFile(&IfClause{
			Cond: litStmts("true"),
			Then: []*Stmt{{
				Cmd: litCall("foo"),
				Redirs: []*Redirect{{
					Op:   DashHdoc,
					Word: litWord("EOF"),
					Hdoc: litWord("\t"),
				}},
			}},
		}),
	),
	fileTest(
		[]string{"foo <<EOF\nEOF_body\nEOF\nfoo2"},
		langFile([]*Stmt{
			{
				Cmd: litCall("foo"),
				Redirs: []*Redirect{{
					Op:   Hdoc,
					Word: litWord("EOF"),
					Hdoc: litWord("EOF_body\n"),
				}},
			},
			litStmt("foo2"),
		}),
	),
	fileTest(
		[]string{"foo <<FOOBAR\nbar\nFOOBAR"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("FOOBAR"),
				Hdoc: litWord("bar\n"),
			}},
		}),
	),
	fileTest(
		[]string{"foo <<\"EOF\"\nbar\nEOF"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: word(dblQuoted(lit("EOF"))),
				Hdoc: litWord("bar\n"),
			}},
		}),
	),
	fileTest(
		[]string{"foo <<'EOF'\nEOF_body\nEOF\nfoo2"},
		langFile([]*Stmt{
			{
				Cmd: litCall("foo"),
				Redirs: []*Redirect{{
					Op:   Hdoc,
					Word: word(sglQuoted("EOF")),
					Hdoc: litWord("EOF_body\n"),
				}},
			},
			litStmt("foo2"),
		}),
	),
	fileTest(
		[]string{"foo <<'EOF'\n${\nEOF"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: word(sglQuoted("EOF")),
				Hdoc: litWord("${\n"),
			}},
		}),
	),
	fileTest(
		[]string{"foo <<'EOF'\nEOF"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: word(sglQuoted("EOF")),
			}},
		}),
	),
	fileTest(
		[]string{"foo <<\"EOF\"2\nbar\nEOF2"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: word(dblQuoted(lit("EOF")), lit("2")),
				Hdoc: litWord("bar\n"),
			}},
		}),
	),
	fileTest(
		[]string{"foo <<\\EOF\nbar\nEOF"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("\\EOF"),
				Hdoc: litWord("bar\n"),
			}},
		}),
	),
	fileTest(
		[]string{"foo <<EOF\nbar\\\nbaz\nEOF"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: word(lit("bar"), lit("baz\n")),
			}},
		}),
	),
	fileTest(
		[]string{
			"foo <<'EOF'\nbar\\\nEOF",
			"foo <<'EOF'\nbar\\\r\nEOF",
			"foo <<'EOF'\nbar\\\r\nEOF\r\n",
		},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: word(sglQuoted("EOF")),
				Hdoc: litWord("bar\\\n"),
			}},
		}),
	),
	fileTest(
		[]string{
			"foo <<-EOF\n\tbar\nEOF",
			"foo <<-EOF\r\n\tbar\r\nEOF\r\n",
		},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   DashHdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("\tbar\n"),
			}},
		}),
	),
	fileTest(
		[]string{"foo <<EOF\nEOF"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
			}},
		}),
	),
	fileTest(
		[]string{"foo <<-EOF\nEOF"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   DashHdoc,
				Word: litWord("EOF"),
			}},
		}),
	),
	fileTest(
		[]string{"foo <<-EOF\n\tbar\nEOF"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   DashHdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("\tbar\n"),
			}},
		}),
	),
	fileTest(
		[]string{"foo <<-'EOF'\n\tbar\nEOF"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   DashHdoc,
				Word: word(sglQuoted("EOF")),
				Hdoc: litWord("\tbar\n"),
			}},
		}),
	),
	fileTest(
		[]string{
			"f1 <<EOF1\nh1\nEOF1\nf2 <<EOF2\nh2\nEOF2",
			"f1 <<EOF1; f2 <<EOF2\nh1\nEOF1\nh2\nEOF2",
		},
		langFile([]*Stmt{
			{
				Cmd: litCall("f1"),
				Redirs: []*Redirect{{
					Op:   Hdoc,
					Word: litWord("EOF1"),
					Hdoc: litWord("h1\n"),
				}},
			},
			{
				Cmd: litCall("f2"),
				Redirs: []*Redirect{{
					Op:   Hdoc,
					Word: litWord("EOF2"),
					Hdoc: litWord("h2\n"),
				}},
			},
		}),
	),
	fileTest(
		[]string{
			"a <<EOF\nfoo\nEOF\nb\nb\nb\nb\nb\nb\nb\nb\nb",
			"a <<EOF;b;b;b;b;b;b;b;b;b\nfoo\nEOF",
		},
		langFile([]*Stmt{
			{
				Cmd: litCall("a"),
				Redirs: []*Redirect{{
					Op:   Hdoc,
					Word: litWord("EOF"),
					Hdoc: litWord("foo\n"),
				}},
			},
			litStmt("b"), litStmt("b"), litStmt("b"),
			litStmt("b"), litStmt("b"), litStmt("b"),
			litStmt("b"), litStmt("b"), litStmt("b"),
		}),
	),
	fileTest(
		[]string{
			"foo \"\narg\" <<EOF\nbar\nEOF",
			"foo <<EOF \"\narg\"\nbar\nEOF",
		},
		langFile(&Stmt{
			Cmd: call(
				litWord("foo"),
				word(dblQuoted(lit("\narg"))),
			),
			Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("bar\n"),
			}},
		}),
	),
	fileTest(
		[]string{"foo >&2 <&0 2>file 345>file <>f2"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{Op: DplOut, Word: litWord("2")},
				{Op: DplIn, Word: litWord("0")},
				{Op: RdrOut, N: lit("2"), Word: litWord("file")},
				{Op: RdrOut, N: lit("345"), Word: litWord("file")},
				{Op: RdrInOut, Word: litWord("f2")},
			},
		}),
	),
	fileTest(
		[]string{
			"foo bar >file",
			"foo bar>file",
		},
		langFile(&Stmt{
			Cmd: litCall("foo", "bar"),
			Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("file")},
			},
		}),
	),
	fileTest(
		[]string{"true &>a"},
		langFile(&Stmt{
			Cmd: litCall("true"),
			Redirs: []*Redirect{
				{Op: RdrAll, Word: litWord("a")},
			},
		}, LangBash|LangMirBSDKorn|LangZsh),
		langErr2("1:6: `&>` redirects are a bash/mksh/zsh feature; tried parsing as LANG", LangPOSIX),
		flipConfirm2(LangPOSIX), // POSIX shells tend to parse &> as & > hence it runs as two commands
	),
	fileTest(
		[]string{"true &>>b"},
		langFile(&Stmt{
			Cmd: litCall("true"),
			Redirs: []*Redirect{
				{Op: AppAll, Word: litWord("b")},
			},
		}, LangBash|LangMirBSDKorn|LangZsh),
		langErr2("1:6: `&>>` redirects are a bash/mksh/zsh feature; tried parsing as LANG", LangPOSIX),
		flipConfirm2(LangPOSIX), // POSIX shells tend to parse &> as & > hence it runs as two commands
	),
	fileTest(
		[]string{"foo 2>file bar", "2>file foo bar"},
		langFile(&Stmt{
			Cmd: litCall("foo", "bar"),
			Redirs: []*Redirect{
				{Op: RdrOut, N: lit("2"), Word: litWord("file")},
			},
		}),
	),
	fileTest(
		[]string{"a >f1\nb >f2", "a >f1; b >f2"},
		langFile([]*Stmt{
			{
				Cmd:    litCall("a"),
				Redirs: []*Redirect{{Op: RdrOut, Word: litWord("f1")}},
			},
			{
				Cmd:    litCall("b"),
				Redirs: []*Redirect{{Op: RdrOut, Word: litWord("f2")}},
			},
		}),
	),
	fileTest(
		[]string{"foo >|bar"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{Op: RdrClob, Word: litWord("bar")},
			},
		}),
	),
	fileTest(
		[]string{"foo >>|a &>|b &>>|c"},
		langErr2("1:5: `>>|` redirects are a zsh feature; tried parsing as LANG"),
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{Op: AppClob, Word: litWord("a")},
				{Op: RdrAllClob, Word: litWord("b")},
				{Op: AppAllClob, Word: litWord("c")},
			},
		}, LangZsh),
	),
	fileTest(
		[]string{
			"foo >|a >>|b &>|c &>>|d",
			"foo >!a >>!b &>!c &>>!d",
		},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{Op: RdrClob, Word: litWord("a")},
				{Op: AppClob, Word: litWord("b")},
				{Op: RdrAllClob, Word: litWord("c")},
				{Op: AppAllClob, Word: litWord("d")},
			},
		}, LangZsh),
	),
	fileTest(
		[]string{
			"foo &>|a &>>c &>>|d",
			"foo >&|a >>&c >>&|d",
			"foo &>!a >>&c >>&!d",
			"foo >&!a >>&c >>&!d",
		},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{Op: RdrAllClob, Word: litWord("a")},
				{Op: AppAll, Word: litWord("c")},
				{Op: AppAllClob, Word: litWord("d")},
			},
		}, LangZsh),
	),
	fileTest(
		[]string{
			"foo <<<input",
			"foo <<< input",
		},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   WordHdoc,
				Word: litWord("input"),
			}},
		}, LangBash|LangMirBSDKorn|LangZsh),
		langErr2("1:5: herestrings are a bash/mksh/zsh feature; tried parsing as LANG", LangPOSIX),
	),
	fileTest(
		[]string{
			`foo <<<"spaced input"`,
			`foo <<< "spaced input"`,
		},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op:   WordHdoc,
				Word: word(dblQuoted(lit("spaced input"))),
			}},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"foo >(foo)"},
		langFile(call(
			litWord("foo"),
			word(&ProcSubst{
				Op:    CmdOut,
				Stmts: litStmts("foo"),
			}),
		), LangBash),
	),
	fileTest(
		[]string{"foo < <(foo)"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{{
				Op: RdrIn,
				Word: word(&ProcSubst{
					Op:    CmdIn,
					Stmts: litStmts("foo"),
				}),
			}},
		}, LangBash),
	),
	fileTest(
		[]string{"a<(b) c>(d)"},
		langFile(call(
			word(lit("a"), &ProcSubst{
				Op:    CmdIn,
				Stmts: litStmts("b"),
			}),
			word(lit("c"), &ProcSubst{
				Op:    CmdOut,
				Stmts: litStmts("d"),
			}),
		), LangBash|LangZsh),
	),
	fileTest(
		[]string{"foo =(bar)"},
		langErr2("1:5: `=(` process substitutions are a zsh feature; tried parsing as LANG"),
		langFile(call(
			litWord("foo"), word(&ProcSubst{
				Op:    CmdInTemp,
				Stmts: litStmts("bar"),
			}),
		), LangZsh),
	),
	fileTest(
		[]string{"foo {fd}<f"},
		langFile(&Stmt{
			Cmd: litCall("foo"),
			Redirs: []*Redirect{
				{Op: RdrIn, N: lit("{fd}"), Word: litWord("f")},
			},
		}, LangBash),
	),
	fileTest(
		[]string{"! foo"},
		langFile(&Stmt{
			Negated: true,
			Cmd:     litCall("foo"),
		}),
		langSkip(LangZsh), // fails to confirm?
	),
	fileTest(
		[]string{"foo &\nbar", "foo & bar", "foo&bar"},
		langFile([]*Stmt{
			{Cmd: litCall("foo"), Background: true},
			litStmt("bar"),
		}),
	),
	fileTest(
		[]string{
			"! if foo; then bar; fi >/dev/null &",
			"! if foo; then bar; fi>/dev/null&",
		},
		langFile(&Stmt{
			Negated: true,
			Cmd: &IfClause{
				Cond: litStmts("foo"),
				Then: litStmts("bar"),
			},
			Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("/dev/null")},
			},
			Background: true,
		}),
	),
	fileTest(
		[]string{
			// TODO: should we allow formatting a redirect at the start?
			"if foo; then bar; fi >/dev/null",
			">/dev/null if foo; then bar; fi",
		},
		langFile(&Stmt{
			Cmd: &IfClause{
				Cond: litStmts("foo"),
				Then: litStmts("bar"),
			},
			Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("/dev/null")},
			},
		}, LangZsh),
	),
	fileTest(
		[]string{"! foo && bar"},
		langFile(&BinaryCmd{
			Op: AndStmt,
			X: &Stmt{
				Cmd:     litCall("foo"),
				Negated: true,
			},
			Y: litStmt("bar"),
		}),
		langSkip(LangZsh), // fails to confirm?
	),
	fileTest(
		[]string{"! foo | bar"},
		langFile(&Stmt{
			Cmd: &BinaryCmd{
				Op: Pipe,
				X:  litStmt("foo"),
				Y:  litStmt("bar"),
			},
			Negated: true,
		}),
		langSkip(LangZsh), // fails to confirm?
	),
	fileTest(
		[]string{
			"a && b &\nc",
			"a && b & c",
		},
		langFile([]*Stmt{
			{
				Cmd: &BinaryCmd{
					Op: AndStmt,
					X:  litStmt("a"),
					Y:  litStmt("b"),
				},
				Background: true,
			},
			litStmt("c"),
		}),
	),
	fileTest(
		[]string{"a | b &"},
		langFile(&Stmt{
			Cmd: &BinaryCmd{
				Op: Pipe,
				X:  litStmt("a"),
				Y:  litStmt("b"),
			},
			Background: true,
		}),
	),
	fileTest(
		[]string{"foo#bar"},
		langFile(litWord("foo#bar")),
	),
	fileTest(
		[]string{"$foo#bar foo#$bar"},
		langFile(call(
			word(litParamExp("foo"), lit("#bar")),
			word(lit("foo#"), litParamExp("bar")),
		)),
	),
	fileTest(
		[]string{"$(foo)#bar", "`foo`#bar"},
		langFile(call(
			word(cmdSubst(litStmt("foo")), lit("#bar")),
		)),
	),
	fileTest(
		[]string{"{ foo } }; }"},
		langFile(block(litStmt("foo", "}", "}"))),
		// TODO: turn these nil files into error tests
		langSkip(LangZsh),
	),
	fileTest(
		[]string{"foo {"},
		langFile(litStmt("foo", "{")),
	),
	fileTest(
		[]string{"foo }"},
		langFile(litStmt("foo", "}"), LangBash),
	),
	fileTest(
		// TODO: should shfmt lean towards no semicolons in Zsh mode?
		// Note that zsh seems to support "{foo}" too, but that is undocumented,
		// and hopefully noone actually relies on that.
		[]string{"{ foo; }", "{ foo }"},
		langFile(block(litStmt("foo")), LangZsh),
	),
	fileTest(
		[]string{"{ }"},
		langErr2("1:1: `{` must be followed by a statement list"),
		langFile(block(), LangZsh|LangMirBSDKorn),
	),
	fileTest(
		[]string{"{ }", "{}"}, // Note that "{}" is a command in POSIX/Bash.
		langFile(block(), LangZsh),
	),
	fileTest(
		[]string{"( )"},
		langErr2("1:1: `(` must be followed by a statement list"),
		langFile(subshell(), LangZsh|LangMirBSDKorn),
	),
	fileTest(
		[]string{"if; then; fi"},
		langErr2("1:1: `if` must be followed by a statement list"),
		langFile(&IfClause{}, LangZsh|LangMirBSDKorn),
		langSkip(LangMirBSDKorn), // only allows empty lists with newlines?
	),
	fileTest(
		[]string{"if foo; then; fi"},
		langErr2("1:9: `then` must be followed by a statement list"),
		langFile(&IfClause{Cond: litStmts("foo")}, LangZsh|LangMirBSDKorn),
		langSkip(LangMirBSDKorn), // only allows empty lists with newlines?
	),
	fileTest(
		[]string{"while; do exit; done", "while\ndo exit\ndone"},
		langErr2("1:1: `while` must be followed by a statement list"),
		langFile(&WhileClause{Do: litStmts("exit")}, LangZsh|LangMirBSDKorn),
		langSkip(LangMirBSDKorn), // only allows empty lists with newlines?
	),
	fileTest(
		[]string{"while false; do; done"},
		langErr2("1:14: `do` must be followed by a statement list"),
		langFile(&WhileClause{Cond: litStmts("false")}, LangZsh|LangMirBSDKorn),
		langSkip(LangMirBSDKorn), // only allows empty lists with newlines?
	),
	fileTest(
		[]string{"$({ foo; })"},
		langFile(cmdSubst(stmt(
			block(litStmt("foo")),
		))),
	),
	fileTest(
		[]string{
			"$( (echo foo bar))",
			"$( (echo foo bar) )",
			"`(echo foo bar)`",
		},
		langFile(cmdSubst(stmt(
			subshell(litStmt("echo", "foo", "bar")),
		))),
	),
	fileTest(
		[]string{"$()"},
		langFile(cmdSubst()),
	),
	fileTest(
		[]string{
			"$(\n\t(a)\n\tb\n)",
			"$( (a); b)",
			"`(a); b`",
		},
		langFile(cmdSubst(
			stmt(subshell(litStmt("a"))),
			litStmt("b"),
		)),
	),
	fileTest(
		[]string{
			`$(echo \')`,
			"`" + `echo \\'` + "`",
		},
		langFile(cmdSubst(litStmt("echo", `\'`))),
	),
	fileTest(
		[]string{
			`$(echo \\)`,
			"`" + `echo \\\\` + "`",
		},
		langFile(cmdSubst(litStmt("echo", `\\`))),
	),
	fileTest(
		[]string{
			`$(echo '\' 'a\b' "\\" "a\a")`,
			"`" + `echo '\' 'a\\b' "\\\\" "a\a"` + "`",
		},
		langFile(cmdSubst(stmt(call(
			litWord("echo"),
			word(sglQuoted(`\`)),
			word(sglQuoted(`a\b`)),
			word(dblQuoted(lit(`\\`))),
			word(dblQuoted(lit(`a\a`))),
		)))),
	),
	fileTest(
		[]string{
			"$(echo $(x))",
			"`echo \\`x\\``",
		},
		langFile(cmdSubst(stmt(call(
			litWord("echo"),
			word(cmdSubst(litStmt("x"))),
		)))),
	),
	fileTest(
		[]string{
			"$($(foo bar))",
			"`\\`foo bar\\``",
		},
		langFile(cmdSubst(stmt(call(
			word(cmdSubst(litStmt("foo", "bar"))),
		)))),
	),
	fileTest(
		[]string{"$( (a) | b)"},
		langFile(cmdSubst(
			stmt(&BinaryCmd{
				Op: Pipe,
				X:  stmt(subshell(litStmt("a"))),
				Y:  litStmt("b"),
			}),
		)),
	),
	fileTest(
		[]string{`"$( (foo))"`},
		langFile(dblQuoted(cmdSubst(stmt(
			subshell(litStmt("foo")),
		)))),
	),
	fileTest(
		[]string{"\"foo\\\nbar\""},
		langFile(dblQuoted(lit("foo"), lit("bar"))),
	),
	fileTest(
		[]string{"'foo\\\nbar'", "'foo\\\r\nbar'"},
		langFile(sglQuoted("foo\\\nbar")),
	),
	fileTest(
		[]string{"$({ echo; })", "`{ echo; }`"},
		langFile(cmdSubst(stmt(
			block(litStmt("echo")),
		))),
	),
	fileTest(
		[]string{`{foo}`},
		langFile(litWord(`{foo}`)),
	),
	fileTest(
		[]string{`{"foo"`},
		langFile(word(lit("{"), dblQuoted(lit("foo")))),
		langSkip(LangZsh),
	),
	fileTest(
		[]string{`foo"bar"`, "fo\\\no\"bar\"", "fo\\\r\no\"bar\""},
		langFile(word(lit("foo"), dblQuoted(lit("bar")))),
	),
	fileTest(
		[]string{`!foo`},
		langFile(litWord(`!foo`)),
	),
	fileTest(
		[]string{"$(foo bar)", "`foo bar`"},
		langFile(cmdSubst(litStmt("foo", "bar"))),
	),
	fileTest(
		[]string{"$(foo | bar)", "`foo | bar`"},
		langFile(cmdSubst(
			stmt(&BinaryCmd{
				Op: Pipe,
				X:  litStmt("foo"),
				Y:  litStmt("bar"),
			}),
		)),
	),
	fileTest(
		[]string{"$(foo | >f)", "`foo | >f`"},
		langFile(cmdSubst(
			stmt(&BinaryCmd{
				Op: Pipe,
				X:  litStmt("foo"),
				Y: &Stmt{Redirs: []*Redirect{{
					Op:   RdrOut,
					Word: litWord("f"),
				}}},
			}),
		)),
	),
	fileTest(
		[]string{"$(foo $(b1 b2))"},
		langFile(cmdSubst(stmt(call(
			litWord("foo"),
			word(cmdSubst(litStmt("b1", "b2"))),
		)))),
	),
	fileTest(
		[]string{`"$(foo "bar")"`},
		langFile(dblQuoted(cmdSubst(stmt(call(
			litWord("foo"),
			word(dblQuoted(lit("bar"))),
		))))),
	),
	fileTest(
		[]string{"$(foo)", "`fo\\\no`"},
		langFile(cmdSubst(litStmt("foo"))),
	),
	fileTest(
		[]string{"foo $(bar)", "foo `bar`"},
		langFile(call(
			litWord("foo"),
			word(cmdSubst(litStmt("bar"))),
		)),
	),
	fileTest(
		[]string{"$(foo 'bar')", "`foo 'bar'`"},
		langFile(cmdSubst(stmt(call(
			litWord("foo"),
			word(sglQuoted("bar")),
		)))),
	),
	fileTest(
		[]string{`$(foo "bar")`, "`foo \"bar\"`"},
		langFile(cmdSubst(stmt(call(
			litWord("foo"),
			word(dblQuoted(lit("bar"))),
		)))),
	),
	fileTest(
		[]string{`"$(foo "bar")"`, "\"`foo \"bar\"`\""},
		langFile(dblQuoted(cmdSubst(stmt(call(
			litWord("foo"),
			word(dblQuoted(lit("bar"))),
		))))),
	),
	fileTest(
		[]string{"${ foo;}", "${\nfoo; }", "${\n\tfoo; }", "${\tfoo;}"},
		langFile(&CmdSubst{
			Stmts:    litStmts("foo"),
			TempFile: true,
		}, LangBash|LangMirBSDKorn),
		langErr2("1:1: `${ stmts;}` is a bash/mksh feature; tried parsing as LANG", LangPOSIX),
	),
	fileTest(
		[]string{"${\n\tfoo\n\tbar\n}", "${ foo; bar;}"},
		langFile(&CmdSubst{
			Stmts:    litStmts("foo", "bar"),
			TempFile: true,
		}, LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{"${|foo;}", "${| foo; }"},
		langFile(&CmdSubst{
			Stmts:    litStmts("foo"),
			ReplyVar: true,
		}, LangBash|LangMirBSDKorn),
		langErr2("1:1: `${|stmts;}` is a bash/mksh feature; tried parsing as LANG", LangPOSIX),
	),
	fileTest(
		[]string{"${|\n\tfoo\n\tbar\n}", "${|foo; bar;}"},
		langFile(&CmdSubst{
			Stmts:    litStmts("foo", "bar"),
			ReplyVar: true,
		}, LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{`"$foo"`},
		langFile(dblQuoted(litParamExp("foo"))),
	),
	fileTest(
		[]string{`"#foo"`},
		langFile(dblQuoted(lit("#foo"))),
	),
	fileTest(
		[]string{`$2foo $3[bar] "ba$4z"`},
		langFile(call(
			word(litParamExp("2"), lit("foo")),
			word(litParamExp("3"), lit("[bar]")),
			word(dblQuoted(lit("ba"), litParamExp("4"), lit("z"))),
		)),
	),
	fileTest(
		[]string{`$@a $*a $#a $$a $?a $!a $-a $0a $30a $_a`},
		langFile(call(
			word(litParamExp("@"), lit("a")),
			word(litParamExp("*"), lit("a")),
			word(litParamExp("#"), lit("a")),
			word(litParamExp("$"), lit("a")),
			word(litParamExp("?"), lit("a")),
			word(litParamExp("!"), lit("a")),
			word(litParamExp("-"), lit("a")),
			word(litParamExp("0"), lit("a")),
			word(litParamExp("3"), lit("0a")),
			word(litParamExp("_a")),
		)),
		langSkip(LangZsh), // TODO: $#a parses as ParamExp, but $!a does not
	),
	fileTest(
		[]string{`"$!"`},
		langFile(dblQuoted(litParamExp("!"))),
	),
	fileTest(
		[]string{`$`, `$ #`},
		langFile(litWord("$")),
	),
	fileTest(
		[]string{`${@} ${*} ${#} ${$} ${?} ${!} ${0} ${29} ${-}`},
		langFile(call(
			word(&ParamExp{Param: lit("@")}),
			word(&ParamExp{Param: lit("*")}),
			word(&ParamExp{Param: lit("#")}),
			word(&ParamExp{Param: lit("$")}),
			word(&ParamExp{Param: lit("?")}),
			word(&ParamExp{Param: lit("!")}),
			word(&ParamExp{Param: lit("0")}),
			word(&ParamExp{Param: lit("29")}),
			word(&ParamExp{Param: lit("-")}),
		)),
	),
	fileTest(
		[]string{`${#$} ${#@} ${#*} ${##}`},
		langFile(call(
			word(&ParamExp{Length: true, Param: lit("$")}),
			word(&ParamExp{Length: true, Param: lit("@")}),
			word(&ParamExp{Length: true, Param: lit("*")}),
			word(&ParamExp{Length: true, Param: lit("#")}),
		)),
	),
	fileTest(
		[]string{`${foo}`},
		langFile(&ParamExp{Param: lit("foo")}),
	),
	fileTest(
		[]string{`${}`},
		langErr2("1:3: invalid parameter name"),
		langFile(&ParamExp{}, LangZsh),
	),
	fileTest(
		[]string{`${foo}"bar"`},
		langFile(word(
			&ParamExp{Param: lit("foo")},
			dblQuoted(lit("bar")),
		)),
	),
	fileTest(
		[]string{`$a/b $a-b $a:b $a}b $a]b $a.b $a,b $a*b $a_b $a2b`},
		langFile(call(
			word(litParamExp("a"), lit("/b")),
			word(litParamExp("a"), lit("-b")),
			word(litParamExp("a"), lit(":b")),
			word(litParamExp("a"), lit("}b")),
			word(litParamExp("a"), lit("]b")),
			word(litParamExp("a"), lit(".b")),
			word(litParamExp("a"), lit(",b")),
			word(litParamExp("a"), lit("*b")),
			word(litParamExp("a_b")),
			word(litParamExp("a2b")),
		)),
	),
	fileTest(
		[]string{`$aàb $àb $,b`},
		langFile(call(
			word(litParamExp("a"), lit("àb")),
			word(lit("$"), lit("àb")),
			word(lit("$"), lit(",b")),
		)),
	),
	fileTest(
		[]string{"$à", "$\\\nà", "$\\\r\nà"},
		langFile(word(lit("$"), lit("à"))),
	),
	fileTest(
		[]string{"$foobar", "$foo\\\nbar"},
		langFile(call(
			word(litParamExp("foobar")),
		)),
	),
	fileTest(
		[]string{"$foo\\bar"},
		langFile(call(
			word(litParamExp("foo"), lit("\\bar")),
		)),
	),
	fileTest(
		[]string{`echo -e "$foo\nbar"`},
		langFile(call(
			litWord("echo"), litWord("-e"),
			word(dblQuoted(
				litParamExp("foo"), lit(`\nbar`),
			)),
		)),
	),
	fileTest(
		[]string{`${foo-bar}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op:   DefaultUnset,
				Word: litWord("bar"),
			},
		}),
	),
	fileTest(
		[]string{`${foo+}"bar"`},
		langFile(word(
			&ParamExp{
				Param: lit("foo"),
				Exp:   &Expansion{Op: AlternateUnset},
			},
			dblQuoted(lit("bar")),
		)),
	),
	fileTest(
		[]string{`${foo:=<"bar"}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op:   AssignUnsetOrNull,
				Word: word(lit("<"), dblQuoted(lit("bar"))),
			},
		}),
	),
	fileTest(
		[]string{
			"${foo:=b${c}$(d)}",
			"${foo:=b${c}`d`}",
		},
		langFile(&ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op: AssignUnsetOrNull,
				Word: word(
					lit("b"),
					&ParamExp{Param: lit("c")},
					cmdSubst(litStmt("d")),
				),
			},
		}),
	),
	fileTest(
		[]string{`${foo?"${bar}"}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op: ErrorUnset,
				Word: word(dblQuoted(
					&ParamExp{Param: lit("bar")},
				)),
			},
		}),
	),
	fileTest(
		[]string{`${foo:?bar1 bar2}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op:   ErrorUnsetOrNull,
				Word: litWord("bar1 bar2"),
			},
		}),
	),
	fileTest(
		[]string{`${a:+b}${a:-b}${a=b}`},
		langFile(word(
			&ParamExp{
				Param: lit("a"),
				Exp: &Expansion{
					Op:   AlternateUnsetOrNull,
					Word: litWord("b"),
				},
			},
			&ParamExp{
				Param: lit("a"),
				Exp: &Expansion{
					Op:   DefaultUnsetOrNull,
					Word: litWord("b"),
				},
			},
			&ParamExp{
				Param: lit("a"),
				Exp: &Expansion{
					Op:   AssignUnset,
					Word: litWord("b"),
				},
			},
		)),
	),
	fileTest(
		[]string{`${3:-'$x'}`},
		langFile(&ParamExp{
			Param: lit("3"),
			Exp: &Expansion{
				Op:   DefaultUnsetOrNull,
				Word: word(sglQuoted("$x")),
			},
		}),
	),
	fileTest(
		[]string{`${@:-$x}`},
		langFile(&ParamExp{
			Param: lit("@"),
			Exp: &Expansion{
				Op:   DefaultUnsetOrNull,
				Word: word(litParamExp("x")),
			},
		}),
	),
	fileTest(
		[]string{`${var#*'="'}`},
		langFile(&ParamExp{
			Param: lit("var"),
			Exp: &Expansion{
				Op:   RemSmallPrefix,
				Word: word(lit("*"), sglQuoted(`="`)),
			},
		}),
	),
	fileTest(
		[]string{`${var/'a'/b'c'd}`},
		langFile(&ParamExp{
			Param: lit("var"),
			Repl: &Replace{
				Orig: word(sglQuoted("a")),
				With: word(lit("b"), sglQuoted("c"), lit("d")),
			},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`${foo%bar}${foo%%bar*}`},
		langFile(word(
			&ParamExp{
				Param: lit("foo"),
				Exp: &Expansion{
					Op:   RemSmallSuffix,
					Word: litWord("bar"),
				},
			},
			&ParamExp{
				Param: lit("foo"),
				Exp: &Expansion{
					Op:   RemLargeSuffix,
					Word: litWord("bar*"),
				},
			},
		)),
	),
	fileTest(
		[]string{`${3#bar}${-##bar*}`},
		langFile(word(
			&ParamExp{
				Param: lit("3"),
				Exp: &Expansion{
					Op:   RemSmallPrefix,
					Word: litWord("bar"),
				},
			},
			&ParamExp{
				Param: lit("-"),
				Exp: &Expansion{
					Op:   RemLargePrefix,
					Word: litWord("bar*"),
				},
			},
		)),
	),
	fileTest(
		[]string{`${foo%?}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Exp: &Expansion{
				Op:   RemSmallSuffix,
				Word: litWord("?"),
			},
		}),
	),
	fileTest(
		[]string{"foo=force_expansion\necho ${foo:#bar}"},
		langErr2("2:11: ${name:#arg} is a zsh feature; tried parsing as LANG"),
		langFile(stmts(
			&CallExpr{Assigns: litAssigns("foo=force_expansion")},
			call(litWord("echo"), word(&ParamExp{
				Param: lit("foo"),
				Exp: &Expansion{
					Op:   MatchEmpty,
					Word: litWord("bar"),
				},
			})),
		), LangZsh),
		flipConfirm2(LangMirBSDKorn), // TODO: why is this a valid substitution in mksh?
	),
	fileTest(
		[]string{"foo=force_expansion\necho ${foo:|bar}"},
		langErr2("2:11: ${name:|arg} is a zsh feature; tried parsing as LANG"),
		langFile(stmts(
			&CallExpr{Assigns: litAssigns("foo=force_expansion")},
			call(litWord("echo"), word(&ParamExp{
				Param: lit("foo"),
				Exp: &Expansion{
					Op:   ArrayExclude,
					Word: litWord("bar"),
				},
			})),
		), LangZsh),
	),
	fileTest(
		[]string{"foo=force_expansion\necho ${foo:*bar}"},
		langErr2("2:11: ${name:*arg} is a zsh feature; tried parsing as LANG"),
		langFile(stmts(
			&CallExpr{Assigns: litAssigns("foo=force_expansion")},
			call(litWord("echo"), word(&ParamExp{
				Param: lit("foo"),
				Exp: &Expansion{
					Op:   ArrayIntersect,
					Word: litWord("bar"),
				},
			})),
		), LangZsh),
	),
	fileTest(
		[]string{
			`${foo[1]}`,
			`${foo[ 1 ]}`,
		},
		langFile(&ParamExp{
			Param: lit("foo"),
			Index: litWord("1"),
		}, LangBash|LangMirBSDKorn|LangZsh),
		langErr2("1:6: arrays are a bash/mksh/zsh feature; tried parsing as LANG", LangPOSIX),
	),
	fileTest(
		[]string{`$foo[1]`},
		langFile(&ParamExp{
			Short: true,
			Param: lit("foo"),
			Index: litWord("1"),
		}, LangZsh),
	),
	fileTest(
		[]string{`a$foo[1]b`},
		langFile(word(lit("a"), litParamExp("foo"), lit("[1]b"))),
		langFile(word(
			lit("a"),
			&ParamExp{
				Short: true,
				Param: lit("foo"),
				Index: litWord("1"),
			},
			lit("b"),
		), LangZsh),
	),
	fileTest(
		[]string{`"a$foo[1]b"`},
		langFile(dblQuoted(
			lit("a"),
			&ParamExp{
				Short: true,
				Param: lit("foo"),
				Index: litWord("1"),
			},
			lit("b"),
		), LangZsh),
	),
	fileTest(
		[]string{`${foo[1,3]}`, `${foo[ 1 , 3 ]}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Index: &BinaryArithm{
				Op: Comma,
				X:  litWord("1"),
				Y:  litWord("3"),
			},
		}, LangBash|LangMirBSDKorn|LangZsh),
		langErr2("1:6: arrays are a bash/mksh/zsh feature; tried parsing as LANG", LangPOSIX),
	),
	fileTest(
		[]string{`${foo[1,-1]}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Index: &BinaryArithm{
				Op: Comma,
				X:  litWord("1"),
				Y: &UnaryArithm{
					Op: Minus,
					X:  litWord("1"),
				},
			},
		}, LangBash|LangMirBSDKorn|LangZsh),
		langErr2("1:6: arrays are a bash/mksh/zsh feature; tried parsing as LANG", LangPOSIX),
	),
	fileTest(
		[]string{`${@[-1]}`},
		langFile(&ParamExp{
			Param: lit("@"),
			Index: &UnaryArithm{
				Op: Minus,
				X:  litWord("1"),
			},
		}, LangZsh),
	),
	fileTest(
		[]string{`${*[-1]}`},
		langFile(&ParamExp{
			Param: lit("*"),
			Index: &UnaryArithm{
				Op: Minus,
				X:  litWord("1"),
			},
		}, LangZsh),
	),
	fileTest(
		[]string{`$foo[1,3]`},
		langFile(&ParamExp{
			Short: true,
			Param: lit("foo"),
			Index: &BinaryArithm{
				Op: Comma,
				X:  litWord("1"),
				Y:  litWord("3"),
			},
		}, LangZsh),
	),
	fileTest(
		[]string{`${signals[(i)QUIT]}`},
		langFile(&ParamExp{
			Param: lit("signals"),
			Index: &FlagsArithm{
				Flags: lit("i"),
				X:     litWord("QUIT"),
			},
		}, LangZsh),
		langErr2("1:11: subscript flags are a zsh feature; tried parsing as LANG", LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{`${ZSH_VERSION[(s:.:w)2]}`},
		langFile(&ParamExp{
			Param: lit("ZSH_VERSION"),
			Index: &FlagsArithm{
				Flags: lit("s:.:w"),
				X:     litWord("2"),
			},
		}, LangZsh),
	),
	fileTest(
		[]string{`$foo[(r)pattern]`},
		langFile(&ParamExp{
			Short: true,
			Param: lit("foo"),
			Index: &FlagsArithm{
				Flags: lit("r"),
				X:     litWord("pattern"),
			},
		}, LangZsh),
	),
	fileTest(
		[]string{`${array[(i)]}`},
		langFile(&ParamExp{
			Param: lit("array"),
			Index: &FlagsArithm{
				Flags: lit("i"),
			},
		}, LangZsh),
	),
	fileTest(
		[]string{`${foo[(r)ab,(r)cd]}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Index: &BinaryArithm{
				Op: Comma,
				X: &FlagsArithm{
					Flags: lit("r"),
					X:     litWord("ab"),
				},
				Y: &FlagsArithm{
					Flags: lit("r"),
					X:     litWord("cd"),
				},
			},
		}, LangZsh),
	),
	fileTest(
		[]string{`${foo[-1]}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Index: &UnaryArithm{
				Op: Minus,
				X:  litWord("1"),
			},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`${foo[@]}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Index: litWord("@"),
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`${foo[*]-etc}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Index: litWord("*"),
			Exp: &Expansion{
				Op:   DefaultUnset,
				Word: litWord("etc"),
			},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`${foo[bar]}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Index: litWord("bar"),
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`${foo[$bar]}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Index: word(litParamExp("bar")),
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`${foo[${bar}]}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Index: word(&ParamExp{Param: lit("bar")}),
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`${foo:1}`, `${foo: 1 }`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Slice: &Slice{Offset: litWord("1")},
		}, LangBash|LangMirBSDKorn|LangZsh),
		langErr2("1:6: slicing is a bash/mksh/zsh feature; tried parsing as LANG", LangPOSIX),
	),
	fileTest(
		[]string{`${foo:1:2}`, `${foo: 1 : 2 }`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Slice: &Slice{
				Offset: litWord("1"),
				Length: litWord("2"),
			},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`${foo:a:b}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Slice: &Slice{
				Offset: litWord("a"),
				Length: litWord("b"),
			},
		}, LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{`${foo:1:-2}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Slice: &Slice{
				Offset: litWord("1"),
				Length: &UnaryArithm{Op: Minus, X: litWord("2")},
			},
		}, LangBash|LangMirBSDKorn), // TODO: zsh -n is buggy here
	),
	fileTest(
		[]string{`${foo::+3}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Slice: &Slice{
				Length: &UnaryArithm{Op: Plus, X: litWord("3")},
			},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`${foo: -1}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Slice: &Slice{
				Offset: &UnaryArithm{Op: Minus, X: litWord("1")},
			},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`${foo: +2+3}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Slice: &Slice{
				Offset: &BinaryArithm{
					Op: Add,
					X:  &UnaryArithm{Op: Plus, X: litWord("2")},
					Y:  litWord("3"),
				},
			},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`${foo:a?1:2:3}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Slice: &Slice{
				Offset: &BinaryArithm{
					Op: TernQuest,
					X:  litWord("a"),
					Y: &BinaryArithm{
						Op: TernColon,
						X:  litWord("1"),
						Y:  litWord("2"),
					},
				},
				Length: litWord("3"),
			},
		}, LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{`${foo/a/b}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Repl:  &Replace{Orig: litWord("a"), With: litWord("b")},
		}, LangBash|LangMirBSDKorn|LangZsh),
		langErr2("1:6: search and replace is a bash/mksh/zsh feature; tried parsing as LANG", LangPOSIX),
	),
	fileTest(
		[]string{"${foo/ /\t}"},
		langFile(&ParamExp{
			Param: lit("foo"),
			Repl:  &Replace{Orig: litWord(" "), With: litWord("\t")},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`${foo/[/]-}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Repl:  &Replace{Orig: litWord("["), With: litWord("]-")},
		}, LangBash|LangMirBSDKorn), // TODO: zsh parses as a pattern?
	),
	fileTest(
		[]string{`${foo/bar/b/a/r}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Repl: &Replace{
				Orig: litWord("bar"),
				With: litWord("b/a/r"),
			},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`${foo/$a/$'\''}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Repl: &Replace{
				Orig: word(litParamExp("a")),
				With: word(sglDQuoted(`\'`)),
			},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`${foo//b1/b2}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Repl: &Replace{
				All:  true,
				Orig: litWord("b1"),
				With: litWord("b2"),
			},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`${foo///}`, `${foo//}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Repl:  &Replace{All: true},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`${foo/-//}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Repl:  &Replace{Orig: litWord("-"), With: litWord("/")},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`${foo//#/}`, `${foo//#}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Repl:  &Replace{All: true, Orig: litWord("#")},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`${foo//[42]/}`},
		langFile(&ParamExp{
			Param: lit("foo"),
			Repl:  &Replace{All: true, Orig: litWord("[42]")},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`${a^b} ${a^^b} ${a,b} ${a,,b}`},
		langFile(call(
			word(&ParamExp{
				Param: lit("a"),
				Exp: &Expansion{
					Op:   UpperFirst,
					Word: litWord("b"),
				},
			}),
			word(&ParamExp{
				Param: lit("a"),
				Exp: &Expansion{
					Op:   UpperAll,
					Word: litWord("b"),
				},
			}),
			word(&ParamExp{
				Param: lit("a"),
				Exp: &Expansion{
					Op:   LowerFirst,
					Word: litWord("b"),
				},
			}),
			word(&ParamExp{
				Param: lit("a"),
				Exp: &Expansion{
					Op:   LowerAll,
					Word: litWord("b"),
				},
			}),
		), LangBash),
	),
	fileTest(
		[]string{`${a@E} ${b@a} ${@@Q} ${!ref@P}`},
		langFile(call(
			word(&ParamExp{
				Param: lit("a"),
				Exp: &Expansion{
					Op:   OtherParamOps,
					Word: litWord("E"),
				},
			}),
			word(&ParamExp{
				Param: lit("b"),
				Exp: &Expansion{
					Op:   OtherParamOps,
					Word: litWord("a"),
				},
			}),
			word(&ParamExp{
				Param: lit("@"),
				Exp: &Expansion{
					Op:   OtherParamOps,
					Word: litWord("Q"),
				},
			}),
			word(&ParamExp{
				Excl:  true,
				Param: lit("ref"),
				Exp: &Expansion{
					Op:   OtherParamOps,
					Word: litWord("P"),
				},
			}),
		), LangBash),
	),
	fileTest(
		[]string{`${a@K} ${b@k}`},
		langFile(call(
			word(&ParamExp{
				Param: lit("a"),
				Exp: &Expansion{
					Op:   OtherParamOps,
					Word: litWord("K"),
				},
			}),
			word(&ParamExp{
				Param: lit("b"),
				Exp: &Expansion{
					Op:   OtherParamOps,
					Word: litWord("k"),
				},
			}),
		), LangBash),
	),
	fileTest(
		[]string{`${a@Q} ${b@#}`},
		langFile(call(
			word(&ParamExp{
				Param: lit("a"),
				Exp: &Expansion{
					Op:   OtherParamOps,
					Word: litWord("Q"),
				},
			}),
			word(&ParamExp{
				Param: lit("b"),
				Exp: &Expansion{
					Op:   OtherParamOps,
					Word: litWord("#"),
				},
			}),
		), LangMirBSDKorn),
	),
	fileTest(
		[]string{`${#foo}`},
		langFile(&ParamExp{
			Length: true,
			Param:  lit("foo"),
		}),
	),
	fileTest(
		[]string{`${%foo}`},
		langFile(&ParamExp{
			Width: true,
			Param: lit("foo"),
		}, LangMirBSDKorn),
	),
	fileTest(
		[]string{`${!foo} ${!bar[@]}`},
		langFile(call(
			word(&ParamExp{
				Excl:  true,
				Param: lit("foo"),
			}),
			word(&ParamExp{
				Excl:  true,
				Param: lit("bar"),
				Index: litWord("@"),
			}),
		), LangBash|LangMirBSDKorn),
		langErr2("1:1: `${!foo}` is a bash/mksh feature; tried parsing as LANG", LangPOSIX),
	),
	fileTest(
		[]string{`${!foo*} ${!bar@}`},
		langFile(call(
			word(&ParamExp{
				Excl:  true,
				Param: lit("foo"),
				Names: NamesPrefix,
			}),
			word(&ParamExp{
				Excl:  true,
				Param: lit("bar"),
				Names: NamesPrefixWords,
			}),
		), LangBash),
		langErr2("1:1: `${!foo}` is a bash/mksh feature; tried parsing as LANG", LangPOSIX),
		langErr2("1:1: `${!foo*}` is a bash feature; tried parsing as LANG", LangMirBSDKorn),
	),
	fileTest(
		[]string{`${#?}`},
		langFile(call(
			word(&ParamExp{Length: true, Param: lit("?")}),
		)),
	),
	fileTest(
		[]string{`${#-foo} ${#?bar}`},
		langFile(call(
			word(&ParamExp{
				Param: lit("#"),
				Exp: &Expansion{
					Op:   DefaultUnset,
					Word: litWord("foo"),
				},
			}),
			word(&ParamExp{
				Param: lit("#"),
				Exp: &Expansion{
					Op:   ErrorUnset,
					Word: litWord("bar"),
				},
			}),
		)),
		langSkip(LangZsh),
	),
	fileTest(
		[]string{`"${foo}"`},
		langFile(dblQuoted(&ParamExp{Param: lit("foo")})),
	),
	fileTest(
		[]string{`"(foo)"`},
		langFile(dblQuoted(lit("(foo)"))),
	),
	fileTest(
		[]string{`"${foo}>"`},
		langFile(dblQuoted(
			&ParamExp{Param: lit("foo")},
			lit(">"),
		)),
	),
	fileTest(
		[]string{`"$(foo)"`, "\"`foo`\""},
		langFile(dblQuoted(cmdSubst(litStmt("foo")))),
	),
	fileTest(
		[]string{
			`"$(foo bar)"`,
			`"$(foo  bar)"`,
			"\"`foo bar`\"",
			"\"`foo  bar`\"",
		},
		langFile(dblQuoted(cmdSubst(litStmt("foo", "bar")))),
	),
	fileTest(
		[]string{`'${foo}'`},
		langFile(sglQuoted("${foo}")),
	),
	fileTest(
		[]string{"$((1))"},
		langFile(arithmExp(litWord("1"))),
	),
	fileTest(
		[]string{"$((1 + 3))", "$((1+3))"},
		langFile(arithmExp(&BinaryArithm{
			Op: Add,
			X:  litWord("1"),
			Y:  litWord("3"),
		})),
	),
	fileTest(
		[]string{`"$((foo))"`},
		langFile(dblQuoted(arithmExp(
			litWord("foo"),
		))),
	),
	fileTest(
		[]string{`$((a)) b`},
		langFile(call(
			word(arithmExp(litWord("a"))),
			litWord("b"),
		)),
	),
	fileTest(
		[]string{`$((arr[0]++))`},
		langFile(arithmExp(&UnaryArithm{
			Op: Inc, Post: true,
			X: word(&ParamExp{
				Short: true,
				Param: lit("arr"),
				Index: litWord("0"),
			}),
		}), LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`$((++arr[0]))`},
		langFile(arithmExp(&UnaryArithm{
			Op: Inc,
			X: word(&ParamExp{
				Short: true,
				Param: lit("arr"),
				Index: litWord("0"),
			}),
		}), LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`$((${a:-1}))`},
		langFile(arithmExp(word(&ParamExp{
			Param: lit("a"),
			Exp: &Expansion{
				Op:   DefaultUnsetOrNull,
				Word: litWord("1"),
			},
		})), LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"$((5 * 2 - 1))", "$((5*2-1))"},
		langFile(arithmExp(&BinaryArithm{
			Op: Sub,
			X: &BinaryArithm{
				Op: Mul,
				X:  litWord("5"),
				Y:  litWord("2"),
			},
			Y: litWord("1"),
		})),
	),
	fileTest(
		[]string{"$((i | 13))"},
		langFile(arithmExp(&BinaryArithm{
			Op: Or,
			X:  litWord("i"),
			Y:  litWord("13"),
		})),
	),
	fileTest(
		[]string{
			"$(((a) + ((b))))",
			"$((\n(a) + \n(\n(b)\n)\n))",
		},
		langFile(arithmExp(&BinaryArithm{
			Op: Add,
			X:  parenArit(litWord("a")),
			Y:  parenArit(parenArit(litWord("b"))),
		})),
	),
	fileTest(
		[]string{
			"$((3 % 7))",
			"$((3\n% 7))",
			"$((3\\\n % 7))",
			"$((3\\\r\n % 7))",
		},
		langFile(arithmExp(&BinaryArithm{
			Op: Rem,
			X:  litWord("3"),
			Y:  litWord("7"),
		})),
	),
	fileTest(
		[]string{`"$((1 / 3))"`},
		langFile(dblQuoted(arithmExp(&BinaryArithm{
			Op: Quo,
			X:  litWord("1"),
			Y:  litWord("3"),
		}))),
	),
	fileTest(
		[]string{"$((2 ** 10))"},
		langFile(arithmExp(&BinaryArithm{
			Op: Pow,
			X:  litWord("2"),
			Y:  litWord("10"),
		})),
	),
	fileTest(
		[]string{`$(((1) ^ 3))`},
		langFile(arithmExp(&BinaryArithm{
			Op: Xor,
			X:  parenArit(litWord("1")),
			Y:  litWord("3"),
		})),
	),
	fileTest(
		[]string{`$((1 >> (3 << 2)))`},
		langFile(arithmExp(&BinaryArithm{
			Op: Shr,
			X:  litWord("1"),
			Y: parenArit(&BinaryArithm{
				Op: Shl,
				X:  litWord("3"),
				Y:  litWord("2"),
			}),
		})),
	),
	fileTest(
		[]string{`$((-(1)))`},
		langFile(arithmExp(&UnaryArithm{
			Op: Minus,
			X:  parenArit(litWord("1")),
		})),
	),
	fileTest(
		[]string{`$((i++))`},
		langFile(arithmExp(&UnaryArithm{
			Op:   Inc,
			Post: true,
			X:    litWord("i"),
		})),
	),
	fileTest(
		[]string{`$((--i))`},
		langFile(arithmExp(&UnaryArithm{Op: Dec, X: litWord("i")})),
	),
	fileTest(
		[]string{`$((!i))`},
		langFile(arithmExp(&UnaryArithm{Op: Not, X: litWord("i")})),
	),
	fileTest(
		[]string{`$((~i))`},
		langFile(arithmExp(&UnaryArithm{Op: BitNegation, X: litWord("i")})),
	),
	fileTest(
		[]string{`$((-!+i))`},
		langFile(arithmExp(&UnaryArithm{
			Op: Minus,
			X: &UnaryArithm{
				Op: Not,
				X:  &UnaryArithm{Op: Plus, X: litWord("i")},
			},
		})),
	),
	fileTest(
		[]string{`$((!!i))`},
		langFile(arithmExp(&UnaryArithm{
			Op: Not,
			X:  &UnaryArithm{Op: Not, X: litWord("i")},
		})),
	),
	fileTest(
		[]string{`$((~~i))`},
		langFile(arithmExp(&UnaryArithm{
			Op: BitNegation,
			X:  &UnaryArithm{Op: BitNegation, X: litWord("i")},
		})),
	),
	fileTest(
		[]string{`$((1 < 3))`},
		langFile(arithmExp(&BinaryArithm{
			Op: Lss,
			X:  litWord("1"),
			Y:  litWord("3"),
		})),
	),
	fileTest(
		[]string{`$((i = 2))`, `$((i=2))`},
		langFile(arithmExp(&BinaryArithm{
			Op: Assgn,
			X:  litWord("i"),
			Y:  litWord("2"),
		})),
	),
	fileTest(
		[]string{`((a[i] = 4))`, `((a[i]=4))`},
		langFile(arithmCmd(&BinaryArithm{
			Op: Assgn,
			X: word(&ParamExp{
				Short: true,
				Param: lit("a"),
				Index: litWord("i"),
			}),
			Y: litWord("4"),
		}), LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"$((a += 2, b -= 3))"},
		langFile(arithmExp(&BinaryArithm{
			Op: Comma,
			X: &BinaryArithm{
				Op: AddAssgn,
				X:  litWord("a"),
				Y:  litWord("2"),
			},
			Y: &BinaryArithm{
				Op: SubAssgn,
				X:  litWord("b"),
				Y:  litWord("3"),
			},
		})),
	),
	fileTest(
		[]string{"$((a >>= 2, b <<= 3))"},
		langFile(arithmExp(&BinaryArithm{
			Op: Comma,
			X: &BinaryArithm{
				Op: ShrAssgn,
				X:  litWord("a"),
				Y:  litWord("2"),
			},
			Y: &BinaryArithm{
				Op: ShlAssgn,
				X:  litWord("b"),
				Y:  litWord("3"),
			},
		})),
	),
	fileTest(
		[]string{"$((a ^^ 2))"},
		langFile(arithmExp(&BinaryArithm{
			Op: XorBool,
			X:  litWord("a"),
			Y:  litWord("2"),
		}), LangZsh),
	),
	fileTest(
		[]string{"$((a ^^= 2, b **= 3))"},
		langFile(arithmExp(&BinaryArithm{
			Op: Comma,
			X: &BinaryArithm{
				Op: XorBoolAssgn,
				X:  litWord("a"),
				Y:  litWord("2"),
			},
			Y: &BinaryArithm{
				Op: PowAssgn,
				X:  litWord("b"),
				Y:  litWord("3"),
			},
		}), LangZsh),
	),
	fileTest(
		[]string{"$((a &&= 2, b ||= 3))"},
		langFile(arithmExp(&BinaryArithm{
			Op: Comma,
			X: &BinaryArithm{
				Op: AndBoolAssgn,
				X:  litWord("a"),
				Y:  litWord("2"),
			},
			Y: &BinaryArithm{
				Op: OrBoolAssgn,
				X:  litWord("b"),
				Y:  litWord("3"),
			},
		}), LangZsh),
	),
	fileTest(
		[]string{"$((a == b && c > d))"},
		langFile(arithmExp(&BinaryArithm{
			Op: AndArit,
			X: &BinaryArithm{
				Op: Eql,
				X:  litWord("a"),
				Y:  litWord("b"),
			},
			Y: &BinaryArithm{
				Op: Gtr,
				X:  litWord("c"),
				Y:  litWord("d"),
			},
		})),
	),
	fileTest(
		[]string{"$((a != b))"},
		langFile(arithmExp(&BinaryArithm{
			Op: Neq,
			X:  litWord("a"),
			Y:  litWord("b"),
		})),
	),
	fileTest(
		[]string{"$((a &= b))"},
		langFile(arithmExp(&BinaryArithm{
			Op: AndAssgn,
			X:  litWord("a"),
			Y:  litWord("b"),
		})),
	),
	fileTest(
		[]string{"$((a |= b))"},
		langFile(arithmExp(&BinaryArithm{
			Op: OrAssgn,
			X:  litWord("a"),
			Y:  litWord("b"),
		})),
	),
	fileTest(
		[]string{"$((a %= b))"},
		langFile(arithmExp(&BinaryArithm{
			Op: RemAssgn,
			X:  litWord("a"),
			Y:  litWord("b"),
		})),
	),
	fileTest(
		[]string{"$((a /= b))", "$((a/=b))"},
		langFile(arithmExp(&BinaryArithm{
			Op: QuoAssgn,
			X:  litWord("a"),
			Y:  litWord("b"),
		})),
	),
	fileTest(
		[]string{"$((a ^= b))"},
		langFile(arithmExp(&BinaryArithm{
			Op: XorAssgn,
			X:  litWord("a"),
			Y:  litWord("b"),
		})),
	),
	fileTest(
		[]string{"$((i *= 3))"},
		langFile(arithmExp(&BinaryArithm{
			Op: MulAssgn,
			X:  litWord("i"),
			Y:  litWord("3"),
		})),
	),
	fileTest(
		[]string{"$((2 >= 10))"},
		langFile(arithmExp(&BinaryArithm{
			Op: Geq,
			X:  litWord("2"),
			Y:  litWord("10"),
		})),
	),
	fileTest(
		[]string{"$((foo ? b1 : b2))"},
		langFile(arithmExp(&BinaryArithm{
			Op: TernQuest,
			X:  litWord("foo"),
			Y: &BinaryArithm{
				Op: TernColon,
				X:  litWord("b1"),
				Y:  litWord("b2"),
			},
		})),
	),
	fileTest(
		[]string{`$((a <= (1 || 2)))`},
		langFile(arithmExp(&BinaryArithm{
			Op: Leq,
			X:  litWord("a"),
			Y: parenArit(&BinaryArithm{
				Op: OrArit,
				X:  litWord("1"),
				Y:  litWord("2"),
			}),
		})),
	),
	fileTest(
		[]string{"foo$", "foo$\n"},
		langFile(word(lit("foo"), lit("$"))),
	),
	fileTest(
		[]string{"foo$", "foo$\\\n", "foo$\\\r\n"},
		langFile(word(lit("foo"), lit("$"))),
	),
	fileTest(
		[]string{`$''`},
		langFile(sglDQuoted(""), LangBash|LangMirBSDKorn|LangZsh),
		langFile(word(lit("$"), sglQuoted("")), LangPOSIX),
	),
	fileTest(
		[]string{`$""`},
		langFile(dblDQuoted(), LangBash|LangMirBSDKorn),
		langFile(word(lit("$"), dblQuoted()), LangPOSIX|LangZsh),
	),
	fileTest(
		[]string{`$'foo'`},
		langFile(sglDQuoted("foo"), LangBash|LangMirBSDKorn|LangZsh),
		langFile(word(lit("$"), sglQuoted("foo")), LangPOSIX),
	),
	fileTest(
		[]string{`$'f+oo${'`},
		langFile(sglDQuoted("f+oo${"), LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"$'foo bar`'"},
		langFile(sglDQuoted("foo bar`"), LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"$'a ${b} c'"},
		langFile(sglDQuoted("a ${b} c"), LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`$"a ${b} c"`},
		langFile(dblDQuoted(
			lit("a "),
			&ParamExp{Param: lit("b")},
			lit(" c"),
		), LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{`"a $b c"`},
		langFile(dblQuoted(lit("a "), litParamExp("b"), lit(" c"))),
	),
	fileTest(
		[]string{`$"a $b c"`},
		langFile(dblDQuoted(
			lit("a "),
			litParamExp("b"),
			lit(" c"),
		), LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{"$'f\\'oo\n'"},
		langFile(sglDQuoted("f\\'oo\n"), LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`$"foo"`},
		langFile(dblDQuoted(lit("foo")), LangBash|LangMirBSDKorn),
		langFile(word(lit("$"), dblQuoted(lit("foo"))), LangPOSIX),
	),
	fileTest(
		[]string{`$"foo$"`},
		langFile(dblDQuoted(lit("foo"), lit("$")), LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{`$"foo bar"`},
		langFile(dblDQuoted(lit("foo bar")), LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{`$'f\'oo'`},
		langFile(sglDQuoted(`f\'oo`), LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`$"f\"oo"`},
		langFile(dblDQuoted(lit(`f\"oo`)), LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{`"foo$"`},
		langFile(dblQuoted(lit("foo"), lit("$"))),
	),
	fileTest(
		[]string{`"foo$$"`},
		langFile(dblQuoted(lit("foo"), litParamExp("$"))),
	),
	fileTest(
		[]string{`"a $\"b\" c"`},
		langFile(dblQuoted(lit(`a `), lit(`$`), lit(`\"b\" c`))),
	),
	fileTest(
		[]string{"$(foo$)", "`foo$`"},
		langFile(cmdSubst(
			stmt(call(word(lit("foo"), lit("$")))),
		)),
	),
	fileTest(
		[]string{"foo$bar"},
		langFile(word(lit("foo"), litParamExp("bar"))),
	),
	fileTest(
		[]string{"foo$(bar)"},
		langFile(word(lit("foo"), cmdSubst(litStmt("bar")))),
	),
	fileTest(
		[]string{"foo${bar}"},
		langFile(word(lit("foo"), &ParamExp{Param: lit("bar")})),
	),
	fileTest(
		[]string{"'foo${bar'"},
		langFile(sglQuoted("foo${bar")),
	),
	fileTest(
		[]string{"(foo)\nbar", "(foo); bar"},
		langFile([]Command{
			subshell(litStmt("foo")),
			litCall("bar"),
		}),
	),
	fileTest(
		[]string{"foo\n(bar)", "foo; (bar)"},
		langFile([]Command{
			litCall("foo"),
			subshell(litStmt("bar")),
		}),
	),
	fileTest(
		[]string{"foo\n(bar)", "foo; (bar)"},
		langFile([]Command{
			litCall("foo"),
			subshell(litStmt("bar")),
		}),
	),
	fileTest(
		[]string{
			"case $i in 1) foo ;; 2 | 3*) bar ;; esac",
			"case $i in 1) foo;; 2 | 3*) bar; esac",
			"case $i in (1) foo;; 2 | 3*) bar;; esac",
			"case $i\nin\n#etc\n1)\nfoo\n;;\n2 | 3*)\nbar\n;;\nesac",
		},
		langFile(&CaseClause{
			Word: word(litParamExp("i")),
			Items: []*CaseItem{
				{
					Op:       Break,
					Patterns: litWords("1"),
					Stmts:    litStmts("foo"),
				},
				{
					Op:       Break,
					Patterns: litWords("2", "3*"),
					Stmts:    litStmts("bar"),
				},
			},
		}),
	),
	fileTest(
		[]string{"case i in 1) a ;& 2) ;; esac"},
		langFile(&CaseClause{
			Word: litWord("i"),
			Items: []*CaseItem{
				{
					Op:       Fallthrough,
					Patterns: litWords("1"),
					Stmts:    litStmts("a"),
				},
				{Op: Break, Patterns: litWords("2")},
			},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{
			"case i in 1) a ;; esac",
			"case i { 1) a ;; }",
			"case i {\n1) a ;;\n}",
		},
		langFile(&CaseClause{
			Word: litWord("i"),
			Items: []*CaseItem{{
				Op:       Break,
				Patterns: litWords("1"),
				Stmts:    litStmts("a"),
			}},
		}, LangMirBSDKorn),
	),
	fileTest(
		[]string{"case i in 1) a ;;& 2) b ;; esac"},
		langFile(&CaseClause{
			Word: litWord("i"),
			Items: []*CaseItem{
				{
					Op:       Resume,
					Patterns: litWords("1"),
					Stmts:    litStmts("a"),
				},
				{
					Op:       Break,
					Patterns: litWords("2"),
					Stmts:    litStmts("b"),
				},
			},
		}, LangBash),
	),
	fileTest(
		[]string{"case i in 1) a ;| 2) b ;; esac"},
		langFile(&CaseClause{
			Word: litWord("i"),
			Items: []*CaseItem{
				{
					Op:       ResumeKorn,
					Patterns: litWords("1"),
					Stmts:    litStmts("a"),
				},
				{
					Op:       Break,
					Patterns: litWords("2"),
					Stmts:    litStmts("b"),
				},
			},
		}, LangMirBSDKorn),
	),
	fileTest(
		[]string{"case $i in 1) cat <<EOF ;;\nfoo\nEOF\nesac"},
		langFile(&CaseClause{
			Word: word(litParamExp("i")),
			Items: []*CaseItem{{
				Op:       Break,
				Patterns: litWords("1"),
				Stmts: []*Stmt{{
					Cmd: litCall("cat"),
					Redirs: []*Redirect{{
						Op:   Hdoc,
						Word: litWord("EOF"),
						Hdoc: litWord("foo\n"),
					}},
				}},
			}},
		}),
	),
	fileTest(
		[]string{"foo | while read a; do b; done"},
		langFile(&BinaryCmd{
			Op: Pipe,
			X:  litStmt("foo"),
			Y: stmt(&WhileClause{
				Cond: []*Stmt{litStmt("read", "a")},

				Do: litStmts("b"),
			}),
		}),
	),
	fileTest(
		[]string{"while read l; do foo || bar; done"},
		langFile(&WhileClause{
			Cond: []*Stmt{litStmt("read", "l")},
			Do: stmts(&BinaryCmd{
				Op: OrStmt,
				X:  litStmt("foo"),
				Y:  litStmt("bar"),
			}),
		}),
	),
	fileTest(
		[]string{"echo if while"},
		langFile(litCall("echo", "if", "while")),
	),
	fileTest(
		[]string{"${foo}if"},
		langFile(word(&ParamExp{Param: lit("foo")}, lit("if"))),
	),
	fileTest(
		[]string{"$if'|'"},
		langFile(word(litParamExp("if"), sglQuoted("|"))),
	),
	fileTest(
		[]string{"if a; then b=; fi", "if a; then b=\nfi"},
		langFile(&IfClause{
			Cond: litStmts("a"),
			Then: stmts(&CallExpr{Assigns: litAssigns("b=")}),
		}),
	),
	fileTest(
		[]string{"if a; then >f; fi", "if a; then >f\nfi"},
		langFile(&IfClause{
			Cond: litStmts("a"),
			Then: []*Stmt{{
				Redirs: []*Redirect{
					{Op: RdrOut, Word: litWord("f")},
				},
			}},
		}),
	),
	fileTest(
		[]string{"if a; then (a); fi", "if a; then (a) fi"},
		langFile(&IfClause{
			Cond: litStmts("a"),
			Then: stmts(subshell(litStmt("a"))),
		}),
	),
	fileTest(
		[]string{"a=b\nc=d", "a=b; c=d"},
		langFile([]Command{
			&CallExpr{Assigns: litAssigns("a=b")},
			&CallExpr{Assigns: litAssigns("c=d")},
		}),
	),
	fileTest(
		[]string{"foo && write | read"},
		langFile(&BinaryCmd{
			Op: AndStmt,
			X:  litStmt("foo"),
			Y: stmt(&BinaryCmd{
				Op: Pipe,
				X:  litStmt("write"),
				Y:  litStmt("read"),
			}),
		}),
	),
	fileTest(
		[]string{"write | read && bar"},
		langFile(&BinaryCmd{
			Op: AndStmt,
			X: stmt(&BinaryCmd{
				Op: Pipe,
				X:  litStmt("write"),
				Y:  litStmt("read"),
			}),
			Y: litStmt("bar"),
		}),
	),
	fileTest(
		[]string{"foo >f | bar"},
		langFile(&BinaryCmd{
			Op: Pipe,
			X: &Stmt{
				Cmd: litCall("foo"),
				Redirs: []*Redirect{
					{Op: RdrOut, Word: litWord("f")},
				},
			},
			Y: litStmt("bar"),
		}),
	),
	fileTest(
		[]string{"(foo) >f | bar"},
		langFile(&BinaryCmd{
			Op: Pipe,
			X: &Stmt{
				Cmd: subshell(litStmt("foo")),
				Redirs: []*Redirect{
					{Op: RdrOut, Word: litWord("f")},
				},
			},
			Y: litStmt("bar"),
		}),
	),
	fileTest(
		[]string{"foo | >f"},
		langFile(&BinaryCmd{
			Op: Pipe,
			X:  litStmt("foo"),
			Y: &Stmt{Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("f")},
			}},
		}),
	),
	fileTest(
		[]string{"[[ a ]]"},
		langFile(&TestClause{X: litWord("a")}, LangBash|LangMirBSDKorn|LangZsh),
		langFile(litStmt("[[", "a", "]]"), LangPOSIX),
	),
	fileTest(
		[]string{"[[ a ]]\nb"},
		langFile(stmts(
			&TestClause{X: litWord("a")},
			litCall("b"),
		), LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ a > b ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: TsAfter,
			X:  litWord("a"),
			Y:  litWord("b"),
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ 1 -nt 2 ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: TsNewer,
			X:  litWord("1"),
			Y:  litWord("2"),
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ 1 -eq 2 ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: TsEql,
			X:  litWord("1"),
			Y:  litWord("2"),
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ -1 -eq -1 ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: TsEql,
			// TODO: parse as unary expressions
			X: litWord("-1"),
			Y: litWord("-1"),
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ +3 -eq 1+2 ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: TsEql,
			// TODO: parse as unary and binary expressions
			X: litWord("+3"),
			Y: litWord("1+2"),
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{
			"[[ -R a ]]",
			"[[\n-R a\n]]",
		},
		langFile(&TestClause{X: &UnaryTest{
			Op: TsRefVar,
			X:  litWord("a"),
		}}, LangBash),
	),
	fileTest(
		[]string{"[[ a =~ b ]]", "[[ a =~ b ]];"},
		langFile(&TestClause{X: &BinaryTest{
			Op: TsReMatch,
			X:  litWord("a"),
			Y:  litWord("b"),
		}}, LangBash),
	),
	fileTest(
		[]string{`[[ a =~ " foo "$bar ]]`},
		langFile(&TestClause{X: &BinaryTest{
			Op: TsReMatch,
			X:  litWord("a"),
			Y: word(
				dblQuoted(lit(" foo ")),
				litParamExp("bar"),
			),
		}}, LangBash),
	),
	fileTest(
		[]string{`[[ a =~ foo"bar" ]]`},
		langFile(&TestClause{X: &BinaryTest{
			Op: TsReMatch,
			X:  litWord("a"),
			Y: word(
				lit("foo"),
				dblQuoted(lit("bar")),
			),
		}}, LangBash),
	),
	fileTest(
		[]string{`[[ a =~ [ab](c |d) ]]`},
		langFile(&TestClause{X: &BinaryTest{
			Op: TsReMatch,
			X:  litWord("a"),
			Y:  litWord("[ab](c |d)"),
		}}, LangBash),
	),
	fileTest(
		[]string{`[[ a =~ ( ]]<>;&) ]]`},
		langFile(&TestClause{X: &BinaryTest{
			Op: TsReMatch,
			X:  litWord("a"),
			Y:  litWord("( ]]<>;&)"),
		}}, LangBash),
	),
	fileTest(
		[]string{`[[ a =~ ($foo) ]]`},
		langFile(&TestClause{X: &BinaryTest{
			Op: TsReMatch,
			X:  litWord("a"),
			Y:  word(lit("("), litParamExp("foo"), lit(")")),
		}}, LangBash),
	),
	fileTest(
		[]string{`[[ a =~ b\ c|d ]]`},
		langFile(&TestClause{X: &BinaryTest{
			Op: TsReMatch,
			X:  litWord("a"),
			Y:  litWord(`b\ c|d`),
		}}, LangBash),
	),
	fileTest(
		[]string{`[[ a == -n ]]`},
		langFile(&TestClause{X: &BinaryTest{
			Op: TsMatch,
			X:  litWord("a"),
			Y:  litWord("-n"),
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`[[ a == (b|c)* ]]`},
		langFile(&TestClause{X: &BinaryTest{
			Op: TsMatch,
			X:  litWord("a"),
			Y:  word(lit("(b|c)"), lit("*")),
		}}, LangZsh),
	),
	fileTest(
		[]string{`[[ a == (#i)bar ]]`},
		langFile(&TestClause{X: &BinaryTest{
			Op: TsMatch,
			X:  litWord("a"),
			Y:  word(lit("(#i)"), lit("bar")),
		}}, LangZsh),
	),
	fileTest(
		[]string{`[[ a =~ -n ]]`},
		langFile(&TestClause{X: &BinaryTest{
			Op: TsReMatch,
			X:  litWord("a"),
			Y:  litWord("-n"),
		}}, LangBash),
	),
	fileTest(
		[]string{"[[ a =~ b$ || c =~ d$ ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: OrTest,
			X: &BinaryTest{
				Op: TsReMatch,
				X:  litWord("a"),
				Y:  word(lit("b"), lit("$")),
			},
			Y: &BinaryTest{
				Op: TsReMatch,
				X:  litWord("c"),
				Y:  word(lit("d"), lit("$")),
			},
		}}, LangBash),
	),
	fileTest(
		[]string{"[[ -n $a ]]"},
		langFile(&TestClause{
			X: &UnaryTest{Op: TsNempStr, X: word(litParamExp("a"))},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ ! $a < 'b' ]]"},
		langFile(&TestClause{X: &UnaryTest{
			Op: TsNot,
			X: &BinaryTest{
				Op: TsBefore,
				X:  word(litParamExp("a")),
				Y:  word(sglQuoted("b")),
			},
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{
			"[[ ! -e $a ]]",
			"[[ ! -a $a ]]",
			"[[\n!\n-a $a\n]]",
		},
		langFile(&TestClause{X: &UnaryTest{
			Op: TsNot,
			X:  &UnaryTest{Op: TsExists, X: word(litParamExp("a"))},
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{
			"[[ a && b ]]",
			"[[\na &&\nb ]]",
			"[[\n\na &&\n\nb ]]",
		},
		langFile(&TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  litWord("a"),
			Y:  litWord("b"),
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ (a && b) ]]"},
		langFile(&TestClause{X: parenTest(&BinaryTest{
			Op: AndTest,
			X:  litWord("a"),
			Y:  litWord("b"),
		})}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{
			"[[ a && (b) ]]",
			"[[ a &&\n(\nb) ]]",
		},
		langFile(&TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  litWord("a"),
			Y:  parenTest(litWord("b")),
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ (a && b) || -f c ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: OrTest,
			X: parenTest(&BinaryTest{
				Op: AndTest,
				X:  litWord("a"),
				Y:  litWord("b"),
			}),
			Y: &UnaryTest{Op: TsRegFile, X: litWord("c")},
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{
			"[[ -S a && -L b ]]",
			"[[ -S a && -h b ]]",
		},
		langFile(&TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsSocket, X: litWord("a")},
			Y:  &UnaryTest{Op: TsSmbLink, X: litWord("b")},
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ -k a && -N b ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsSticky, X: litWord("a")},
			Y:  &UnaryTest{Op: TsModif, X: litWord("b")},
		}}, LangBash),
	),
	fileTest(
		[]string{"[[ -G a && -O b ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsGrpOwn, X: litWord("a")},
			Y:  &UnaryTest{Op: TsUsrOwn, X: litWord("b")},
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ -d a && -c b ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsDirect, X: litWord("a")},
			Y:  &UnaryTest{Op: TsCharSp, X: litWord("b")},
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ -b a && -p b ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsBlckSp, X: litWord("a")},
			Y:  &UnaryTest{Op: TsNmPipe, X: litWord("b")},
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ -g a && -u b ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsGIDSet, X: litWord("a")},
			Y:  &UnaryTest{Op: TsUIDSet, X: litWord("b")},
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ -r a && -w b ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsRead, X: litWord("a")},
			Y:  &UnaryTest{Op: TsWrite, X: litWord("b")},
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ -x a && -s b ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsExec, X: litWord("a")},
			Y:  &UnaryTest{Op: TsNoEmpty, X: litWord("b")},
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ -t a && -z b ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsFdTerm, X: litWord("a")},
			Y:  &UnaryTest{Op: TsEmpStr, X: litWord("b")},
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ -o a && -v b ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: AndTest,
			X:  &UnaryTest{Op: TsOptSet, X: litWord("a")},
			Y:  &UnaryTest{Op: TsVarSet, X: litWord("b")},
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ a -ot b && c -ef d ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: AndTest,
			X: &BinaryTest{
				Op: TsOlder,
				X:  litWord("a"),
				Y:  litWord("b"),
			},
			Y: &BinaryTest{
				Op: TsDevIno,
				X:  litWord("c"),
				Y:  litWord("d"),
			},
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ a = b && c != d ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: AndTest,
			X: &BinaryTest{
				Op: TsMatchShort,
				X:  litWord("a"),
				Y:  litWord("b"),
			},
			Y: &BinaryTest{
				Op: TsNoMatch,
				X:  litWord("c"),
				Y:  litWord("d"),
			},
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ a -ne b && c -le d ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: AndTest,
			X: &BinaryTest{
				Op: TsNeq,
				X:  litWord("a"),
				Y:  litWord("b"),
			},
			Y: &BinaryTest{
				Op: TsLeq,
				X:  litWord("c"),
				Y:  litWord("d"),
			},
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ c -ge d ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: TsGeq,
			X:  litWord("c"),
			Y:  litWord("d"),
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ a -lt b && c -gt d ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: AndTest,
			X: &BinaryTest{
				Op: TsLss,
				X:  litWord("a"),
				Y:  litWord("b"),
			},
			Y: &BinaryTest{
				Op: TsGtr,
				X:  litWord("c"),
				Y:  litWord("d"),
			},
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"[[ 1.2 -gt 0.3 ]]"},
		langFile(&TestClause{X: &BinaryTest{
			Op: TsGtr,
			X:  litWord("1.2"),
			Y:  litWord("0.3"),
		}}, LangZsh),
		// TODO: reject floating point here, just like with arithmetic expressions
		// langErr2(``, LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{"declare -f func"},
		langFile(litStmt("declare", "-f", "func")),
		langFile(&DeclClause{
			Variant: lit("declare"),
			Args: []*Assign{
				{Naked: true, Value: litWord("-f")},
				{Naked: true, Name: lit("func")},
			},
		}, langBashLike|LangZsh),
	),
	fileTest(
		[]string{"(local bar)"},
		langFile(subshell(stmt(&DeclClause{
			Variant: lit("local"),
			Args:    litAssigns("bar"),
		})), LangBash|LangMirBSDKorn|LangZsh),
		langFile(subshell(litStmt("local", "bar")), LangPOSIX),
	),
	fileTest(
		[]string{"local {a,b}_c=1"},
		langFile(&DeclClause{
			Variant: lit("local"),
			Args:    []*Assign{{Naked: true, Value: litWord("{a,b}_c=1")}},
		}, LangBash|LangMirBSDKorn|LangZsh),
		langFile(litStmt("local", "{a,b}_c=1"), LangPOSIX),
	),
	fileTest(
		[]string{"typeset"},
		langFile(&DeclClause{Variant: lit("typeset")}, LangBash|LangMirBSDKorn|LangZsh),
		langFile(litStmt("typeset"), LangPOSIX),
	),
	fileTest(
		[]string{"export bar"},
		langFile(&DeclClause{
			Variant: lit("export"),
			Args:    litAssigns("bar"),
		}, LangBash|LangMirBSDKorn|LangZsh),
		langFile(litStmt("export", "bar"), LangPOSIX),
	),
	fileTest(
		[]string{"readonly -n"},
		langFile(&DeclClause{
			Variant: lit("readonly"),
			Args:    []*Assign{{Naked: true, Value: litWord("-n")}},
		}, LangBash|LangMirBSDKorn|LangZsh),
		langFile(litStmt("readonly", "-n"), LangPOSIX),
	),
	fileTest(
		[]string{"nameref bar="},
		langFile(&DeclClause{
			Variant: lit("nameref"),
			Args: []*Assign{{
				Name: lit("bar"),
			}},
		}, LangBash|LangMirBSDKorn|LangZsh),
		langFile(litStmt("nameref", "bar="), LangPOSIX),
	),
	fileTest(
		[]string{"declare -a +n -b$o foo=bar"},
		langFile(&DeclClause{
			Variant: lit("declare"),
			Args: []*Assign{
				{Naked: true, Value: litWord("-a")},
				{Naked: true, Value: litWord("+n")},
				{Naked: true, Value: word(lit("-b"), litParamExp("o"))},
				{Name: lit("foo"), Value: litWord("bar")},
			},
		}, LangBash),
	),
	fileTest(
		[]string{
			"declare -a foo=(b1 $(b2))",
			"declare -a foo=(b1 `b2`)",
		},
		langFile(&DeclClause{
			Variant: lit("declare"),
			Args: []*Assign{
				{Naked: true, Value: litWord("-a")},
				{
					Name: lit("foo"),
					Array: arrValues(
						litWord("b1"),
						word(cmdSubst(litStmt("b2"))),
					),
				},
			},
		}, LangBash),
		langErr2("1:16: the `declare` builtin is a bash feature; tried parsing as LANG", LangPOSIX),
	),
	fileTest(
		[]string{"local -a foo=(b1)"},
		langFile(&DeclClause{
			Variant: lit("local"),
			Args: []*Assign{
				{Naked: true, Value: litWord("-a")},
				{
					Name:  lit("foo"),
					Array: arrValues(litWord("b1")),
				},
			},
		}, LangBash),
	),
	fileTest(
		[]string{"declare -A foo=([a]=b)"},
		langFile(&DeclClause{
			Variant: lit("declare"),
			Args: []*Assign{
				{Naked: true, Value: litWord("-A")},
				{
					Name: lit("foo"),
					Array: &ArrayExpr{Elems: []*ArrayElem{{
						Index: litWord("a"),
						Value: litWord("b"),
					}}},
				},
			},
		}, LangBash),
	),
	fileTest(
		[]string{"declare foo[a]="},
		langFile(&DeclClause{
			Variant: lit("declare"),
			Args: []*Assign{{
				Name:  lit("foo"),
				Index: litWord("a"),
			}},
		}, LangBash),
	),
	fileTest(
		[]string{"declare foo[*]"},
		langFile(&DeclClause{
			Variant: lit("declare"),
			Args: []*Assign{{
				Name:  lit("foo"),
				Index: litWord("*"),
				Naked: true,
			}},
		}, LangBash),
	),
	fileTest(
		[]string{`declare foo["x y"]`},
		langFile(&DeclClause{
			Variant: lit("declare"),
			Args: []*Assign{{
				Name:  lit("foo"),
				Index: word(dblQuoted(lit("x y"))),
				Naked: true,
			}},
		}, LangBash),
	),
	fileTest(
		[]string{`declare foo['x y']`},
		langFile(&DeclClause{
			Variant: lit("declare"),
			Args: []*Assign{{
				Name:  lit("foo"),
				Index: word(sglQuoted("x y")),
				Naked: true,
			}},
		}, LangBash),
	),
	fileTest(
		[]string{"foo=([)"},
		langFile(&CallExpr{Assigns: []*Assign{{
			Name:  lit("foo"),
			Array: arrValues(litWord("[")),
		}}}, LangMirBSDKorn),
	),
	fileTest(
		[]string{
			"a && b=(c)\nd",
			"a && b=(c); d",
		},
		langFile(stmts(
			&BinaryCmd{
				Op: AndStmt,
				X:  litStmt("a"),
				Y: stmt(&CallExpr{Assigns: []*Assign{{
					Name:  lit("b"),
					Array: arrValues(litWord("c")),
				}}}),
			},
			litCall("d"),
		), LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"declare -f $func >/dev/null"},
		langFile(&Stmt{
			Cmd: &DeclClause{
				Variant: lit("declare"),
				Args: []*Assign{
					{Naked: true, Value: litWord("-f")},
					{
						Naked: true,
						Value: word(litParamExp("func")),
					},
				},
			},
			Redirs: []*Redirect{
				{Op: RdrOut, Word: litWord("/dev/null")},
			},
		}, LangBash|LangZsh),
	),
	fileTest(
		[]string{"declare a\n{ x; }"},
		langFile(stmts(
			&DeclClause{
				Variant: lit("declare"),
				Args:    litAssigns("a"),
			},
			block(litStmt("x")),
		), LangBash),
	),
	fileTest(
		[]string{"eval a=b foo"},
		langFile(litStmt("eval", "a=b", "foo")),
	),
	fileTest(
		[]string{"time", "time\n"},
		langFile(litStmt("time"), LangPOSIX),
		langFile(&TimeClause{}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"time -p"},
		langFile(litStmt("time", "-p"), LangPOSIX),
		langFile(&TimeClause{PosixFormat: true}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"time -a"},
		langFile(litStmt("time", "-a"), LangPOSIX),
		langFile(&TimeClause{Stmt: litStmt("-a")}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"time --"},
		langFile(litStmt("time", "--"), LangPOSIX),
		langFile(&TimeClause{Stmt: litStmt("--")}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"time foo"},
		langFile(&TimeClause{Stmt: litStmt("foo")}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"time { foo; }"},
		langFile(&TimeClause{Stmt: stmt(block(litStmt("foo")))}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"time\nfoo"},
		langFile([]*Stmt{
			stmt(&TimeClause{}),
			litStmt("foo"),
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"coproc foo bar"},
		langFile(litStmt("coproc", "foo", "bar")),
		langFile(&CoprocClause{Stmt: litStmt("foo", "bar")}, langBashLike),
	),
	fileTest(
		[]string{"coproc name { foo; }"},
		langFile(&CoprocClause{
			Name: litWord("name"),
			Stmt: stmt(block(litStmt("foo"))),
		}, LangBash),
	),
	fileTest(
		[]string{"coproc $namevar { foo; }"},
		langFile(&CoprocClause{
			Name: word(litParamExp("namevar")),
			Stmt: stmt(block(litStmt("foo"))),
		}, LangBash),
	),
	fileTest(
		[]string{"coproc foo", "coproc foo;"},
		langFile(&CoprocClause{Stmt: litStmt("foo")}, LangBash),
	),
	fileTest(
		[]string{"coproc { foo; }"},
		langFile(&CoprocClause{
			Stmt: stmt(block(litStmt("foo"))),
		}, LangBash),
	),
	fileTest(
		[]string{"coproc (foo)"},
		langFile(&CoprocClause{
			Stmt: stmt(subshell(litStmt("foo"))),
		}, LangBash),
	),
	fileTest(
		[]string{"coproc name foo | bar"},
		langFile(&CoprocClause{
			Name: litWord("name"),
			Stmt: stmt(&BinaryCmd{
				Op: Pipe,
				X:  litStmt("foo"),
				Y:  litStmt("bar"),
			}),
		}, LangBash),
	),
	fileTest(
		[]string{"coproc $()", "coproc ``"},
		langFile(&CoprocClause{Stmt: stmt(call(
			word(cmdSubst()),
		))}, LangBash),
	),
	fileTest(
		[]string{`let i++`},
		langFile(letClause(
			&UnaryArithm{Op: Inc, Post: true, X: litWord("i")},
		), LangBash|LangMirBSDKorn|LangZsh),
		langFile(litStmt("let", "i++"), LangPOSIX),
	),
	fileTest(
		[]string{`let a++ b++ c +d`},
		langFile(letClause(
			&UnaryArithm{Op: Inc, Post: true, X: litWord("a")},
			&UnaryArithm{Op: Inc, Post: true, X: litWord("b")},
			litWord("c"),
			&UnaryArithm{Op: Plus, X: litWord("d")},
		), LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`let ++i >/dev/null`},
		langFile(&Stmt{
			Cmd:    letClause(&UnaryArithm{Op: Inc, X: litWord("i")}),
			Redirs: []*Redirect{{Op: RdrOut, Word: litWord("/dev/null")}},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{
			`let a=(1 + 2) b=3+4`,
			`let a=(1+2) b=3+4`,
		},
		langFile(letClause(
			&BinaryArithm{
				Op: Assgn,
				X:  litWord("a"),
				Y: parenArit(&BinaryArithm{
					Op: Add,
					X:  litWord("1"),
					Y:  litWord("2"),
				}),
			},
			&BinaryArithm{
				Op: Assgn,
				X:  litWord("b"),
				Y: &BinaryArithm{
					Op: Add,
					X:  litWord("3"),
					Y:  litWord("4"),
				},
			},
		), LangBash),
		langErr2("1:7: the `let` builtin is a bash feature; tried parsing as LANG", LangPOSIX),
	),
	fileTest(
		[]string{
			`let a=$(echo 3)`,
			"let a=`echo 3`",
		},
		langFile(letClause(
			&BinaryArithm{
				Op: Assgn,
				X:  litWord("a"),
				Y:  word(cmdSubst(litStmt("echo", "3"))),
			},
		), LangBash),
	),
	fileTest(
		[]string{"(foo-bar)"},
		langFile(subshell(litStmt("foo-bar"))),
	),
	fileTest(
		[]string{
			"let i++\nbar",
			"let i++ \nbar",
			"let i++; bar",
		},
		langFile(stmts(
			letClause(&UnaryArithm{
				Op:   Inc,
				Post: true,
				X:    litWord("i"),
			}),
			litCall("bar"),
		), LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{
			"let i++\nfoo=(bar)",
			"let i++; foo=(bar)",
			"let i++; foo=(bar)\n",
		},
		langFile(stmts(
			letClause(&UnaryArithm{
				Op:   Inc,
				Post: true,
				X:    litWord("i"),
			}),
			&CallExpr{Assigns: []*Assign{{
				Name:  lit("foo"),
				Array: arrValues(litWord("bar")),
			}}},
		), LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{
			"case a in b) let i++ ;; esac",
			"case a in b) let i++;; esac",
		},
		langFile(&CaseClause{
			Word: word(lit("a")),
			Items: []*CaseItem{{
				Op:       Break,
				Patterns: litWords("b"),
				Stmts: stmts(letClause(&UnaryArithm{
					Op:   Inc,
					Post: true,
					X:    litWord("i"),
				})),
			}},
		}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"a+=1"},
		langFile(&CallExpr{
			Assigns: []*Assign{{
				Append: true,
				Name:   lit("a"),
				Value:  litWord("1"),
			}},
		}, LangBash|LangMirBSDKorn|LangZsh),
		langFile(litStmt("a+=1"), LangPOSIX),
	),
	fileTest(
		[]string{"b+=(2 3)"},
		langFile(&CallExpr{Assigns: []*Assign{{
			Append: true,
			Name:   lit("b"),
			Array:  arrValues(litWords("2", "3")...),
		}}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"a[1]=(b c)"},
		langFile(&CallExpr{Assigns: []*Assign{{
			Name:  lit("a"),
			Index: litWord("1"),
			Array: arrValues(litWords("b", "c")...),
		}}}, LangZsh),
	),
	fileTest(
		[]string{"a[2]=b c[-3]= d[x]+=e"},
		langFile(litStmt("a[2]=b", "c[-3]=", "d[x]+=e"), LangPOSIX),
		langFile(&CallExpr{Assigns: []*Assign{
			{
				Name:  lit("a"),
				Index: litWord("2"),
				Value: litWord("b"),
			},
			{
				Name: lit("c"),
				Index: &UnaryArithm{
					Op: Minus,
					X:  litWord("3"),
				},
			},
			{
				Name:   lit("d"),
				Index:  litWord("x"),
				Append: true,
				Value:  litWord("e"),
			},
		}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"*[i]=x"},
		langFile(word(lit("*"), lit("[i]=x")), LangBash|LangMirBSDKorn|LangZsh),
		langFile(lit("*[i]=x"), LangPOSIX),
	),
	fileTest(
		[]string{"arr[0,1]=x"},
		langFile(&CallExpr{Assigns: []*Assign{{
			Name: lit("arr"),
			Index: &BinaryArithm{
				Op: Comma,
				X:  litWord("0"),
				Y:  litWord("1"),
			},
			Value: litWord("x"),
		}}}, LangZsh),
		langFile(lit("arr[0,1]=x"), LangPOSIX),
	),
	fileTest(
		[]string{
			"b[i]+=2",
			"b[ i ]+=2",
		},
		langFile(&CallExpr{Assigns: []*Assign{{
			Append: true,
			Name:   lit("b"),
			Index:  litWord("i"),
			Value:  litWord("2"),
		}}}, LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`$((a + "b + $c"))`},
		langFile(arithmExp(&BinaryArithm{
			Op: Add,
			X:  litWord("a"),
			Y: word(dblQuoted(
				lit("b + "),
				litParamExp("c"),
			)),
		})),
	),
	fileTest(
		[]string{`let 'i++'`},
		langFile(letClause(word(sglQuoted("i++"))), LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{`echo ${a["x y"]}`},
		langFile(call(litWord("echo"), word(&ParamExp{
			Param: lit("a"),
			Index: word(dblQuoted(lit("x y"))),
		})), LangBash),
	),
	fileTest(
		[]string{
			`a[$"x y"]=b`,
			`a[ $"x y" ]=b`,
		},
		langFile(&CallExpr{Assigns: []*Assign{{
			Name: lit("a"),
			Index: word(&DblQuoted{Dollar: true, Parts: []WordPart{
				lit("x y"),
			}}),
			Value: litWord("b"),
		}}}, LangBash),
	),
	fileTest(
		[]string{`((a["x y"] = b))`, `((a["x y"]=b))`},
		langFile(arithmCmd(&BinaryArithm{
			Op: Assgn,
			X: word(&ParamExp{
				Short: true,
				Param: lit("a"),
				Index: word(dblQuoted(lit("x y"))),
			}),
			Y: litWord("b"),
		}), LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{
			`a=(["x y"]=b)`,
			`a=( [ "x y" ]=b)`,
		},
		langFile(&CallExpr{Assigns: []*Assign{{
			Name: lit("a"),
			Array: &ArrayExpr{Elems: []*ArrayElem{{
				Index: word(dblQuoted(lit("x y"))),
				Value: litWord("b"),
			}}},
		}}}, LangBash),
	),
	fileTest(
		[]string{
			"a=([x]= [y]=)",
			"a=(\n[x]=\n[y]=\n)",
		},
		langFile(&CallExpr{Assigns: []*Assign{{
			Name: lit("a"),
			Array: &ArrayExpr{Elems: []*ArrayElem{
				{Index: litWord("x")},
				{Index: litWord("y")},
			}},
		}}}, LangBash),
	),
	fileTest(
		[]string{"a]b"},
		langFile(litStmt("a]b")),
	),
	fileTest(
		[]string{"echo a[b c[de]f"},
		langFile(litStmt("echo", "a[b", "c[de]f"), LangPOSIX),
		langFile(call(litWord("echo"),
			word(lit("a"), lit("[b")),
			word(lit("c"), lit("[de]f")),
		), LangBash|LangMirBSDKorn|LangZsh),
	),
	fileTest(
		[]string{"<<EOF | b\nfoo\nEOF"},
		langFile(&BinaryCmd{
			Op: Pipe,
			X: &Stmt{Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("foo\n"),
			}}},
			Y: litStmt("b"),
		}),
	),
	fileTest(
		[]string{"<<EOF1 <<EOF2 | c && d\nEOF1\nEOF2"},
		langFile(&BinaryCmd{
			Op: AndStmt,
			X: stmt(&BinaryCmd{
				Op: Pipe,
				X: &Stmt{Redirs: []*Redirect{
					{Op: Hdoc, Word: litWord("EOF1")},
					{Op: Hdoc, Word: litWord("EOF2")},
				}},
				Y: litStmt("c"),
			}),
			Y: litStmt("d"),
		}),
	),
	fileTest(
		[]string{
			"<<EOF && { bar; }\nhdoc\nEOF",
			"<<EOF &&\nhdoc\nEOF\n{ bar; }",
		},
		langFile(&BinaryCmd{
			Op: AndStmt,
			X: &Stmt{Redirs: []*Redirect{{
				Op:   Hdoc,
				Word: litWord("EOF"),
				Hdoc: litWord("hdoc\n"),
			}}},
			Y: stmt(block(litStmt("bar"))),
		}),
	),
	fileTest(
		[]string{"foo() {\n\t<<EOF && { bar; }\nhdoc\nEOF\n}"},
		langFile(&FuncDecl{
			Parens: true,
			Name:   lit("foo"),
			Body: stmt(block(stmt(&BinaryCmd{
				Op: AndStmt,
				X: &Stmt{Redirs: []*Redirect{{
					Op:   Hdoc,
					Word: litWord("EOF"),
					Hdoc: litWord("hdoc\n"),
				}}},
				Y: stmt(block(litStmt("bar"))),
			}))),
		}),
	),
	fileTest(
		[]string{`"a$("")"`, "\"a`\"\"`\""},
		langFile(dblQuoted(
			lit("a"),
			cmdSubst(stmt(call(
				word(dblQuoted()),
			))),
		)),
	),
	fileTest(
		[]string{"echo ?(b)*(c)+(d)@(e)!(f)"},
		langFile(call(litWord("echo"), word(
			&ExtGlob{Op: GlobZeroOrOne, Pattern: lit("b")},
			&ExtGlob{Op: GlobZeroOrMore, Pattern: lit("c")},
			&ExtGlob{Op: GlobOneOrMore, Pattern: lit("d")},
			&ExtGlob{Op: GlobOne, Pattern: lit("e")},
			&ExtGlob{Op: GlobExcept, Pattern: lit("f")},
		)), LangBash|LangMirBSDKorn),
		langFile(call(litWord("echo"), word(
			lit("?"), lit("(b)"),
			lit("*"), lit("(c)"),
			lit("+"), lit("(d)"),
			lit("@"), lit("(e)"),
			lit("!"), lit("(f)"),
		)), LangZsh),
	),
	fileTest(
		[]string{"echo foo@(b*(c|d))bar"},
		langFile(call(litWord("echo"), word(
			lit("foo"),
			&ExtGlob{Op: GlobOne, Pattern: lit("b*(c|d)")},
			lit("bar"),
		)), LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{"echo $a@(b)$c?(d)$e*(f)$g+(h)$i!(j)$k"},
		langFile(call(litWord("echo"), word(
			litParamExp("a"),
			&ExtGlob{Op: GlobOne, Pattern: lit("b")},
			litParamExp("c"),
			&ExtGlob{Op: GlobZeroOrOne, Pattern: lit("d")},
			litParamExp("e"),
			&ExtGlob{Op: GlobZeroOrMore, Pattern: lit("f")},
			litParamExp("g"),
			&ExtGlob{Op: GlobOneOrMore, Pattern: lit("h")},
			litParamExp("i"),
			&ExtGlob{Op: GlobExcept, Pattern: lit("j")},
			litParamExp("k"),
		)), LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{`"^$1[[:space:]]"`},
		langFile(dblQuoted(
			lit("^"),
			litParamExp("1"),
			lit("[[:space:]]"),
		)),
	),
	// Zsh glob qualifiers are parsed as part of the word.
	fileTest(
		[]string{"echo *(.)"},
		langFile(call(litWord("echo"), word(lit("*"), lit("(.)"))), LangZsh),
	),
	fileTest(
		[]string{"echo **(/)"},
		langFile(call(litWord("echo"), word(lit("**"), lit("(/)"))), LangZsh),
	),
	fileTest(
		[]string{"echo *.txt(@)"},
		langFile(call(litWord("echo"), word(lit("*.txt"), lit("(@)"))), LangZsh),
	),
	fileTest(
		[]string{"echo *(om[1,5])"},
		langFile(call(litWord("echo"), word(lit("*"), lit("(om[1,5])"))), LangZsh),
	),
	fileTest(
		[]string{"echo /bin/sh(:t)"},
		langFile(call(litWord("echo"), word(lit("/bin/sh"), lit("(:t)"))), LangZsh),
	),
	// Zsh glob qualifiers on words without glob metacharacters or slashes.
	fileTest(
		[]string{"echo .(:a)"},
		langFile(call(litWord("echo"), word(lit("."), lit("(:a)"))), LangZsh),
	),
	fileTest(
		[]string{"echo ~(:t)"},
		langFile(call(litWord("echo"), word(lit("~"), lit("(:t)"))), LangZsh),
	),
	fileTest(
		[]string{`echo {go.mod,shfmt}(N*)`},
		langFile(call(litWord("echo"), word(lit("{go.mod,shfmt}"), lit("(N*)"))), LangZsh),
	),
	fileTest(
		[]string{"echo $var(Nms-3)"},
		langFile(call(litWord("echo"), word(litParamExp("var"), lit("(Nms-3)"))), LangZsh),
	),
	fileTest(
		[]string{"echo a(A) b(B)"},
		langFile(call(litWord("echo"), word(lit("a"), lit("(A)")), word(lit("b"), lit("(B)"))), LangZsh),
	),
	// Zsh numeric range globs.
	fileTest(
		[]string{"echo <->"},
		langFile(litCall("echo", "<->"), LangZsh),
	),
	fileTest(
		[]string{"echo <5-10>"},
		langFile(litCall("echo", "<5-10>"), LangZsh),
	),
	fileTest(
		[]string{"echo foo<->.txt"},
		langFile(litCall("echo", "foo<->.txt"), LangZsh),
	),
	fileTest(
		[]string{"echo <5->.*"},
		langFile(litCall("echo", "<5->.*"), LangZsh),
	),
	fileTest(
		// <2-3 is a redirect, not a numeric range glob (no closing >).
		[]string{"echo <2-3"},
		langFile(&Stmt{
			Cmd:    litCall("echo"),
			Redirs: []*Redirect{{Op: RdrIn, Word: litWord("2-3")}},
		}, LangZsh),
	),
	fileTest(
		[]string{"@test \"desc\" { body; }"},
		langFile(&TestDecl{
			Description: word(dblQuoted(lit("desc"))),
			Body:        stmt(block(litStmt("body"))),
		}, LangBats),
	),
	fileTest(
		[]string{"@test 'desc' {\n\tmultiple\n\tstatements\n}"},
		langFile(&TestDecl{
			Description: word(sglQuoted("desc")),
			Body:        stmt(block(litStmts("multiple", "statements")...)),
		}, LangBats),
	),
	fileTest(
		[]string{"${+foo}"},
		langFile(&ParamExp{
			IsSet: true,
			Param: lit("foo"),
		}, LangZsh),
	),
	fileTest(
		[]string{"$+foo $#bar"},
		langFile(call(
			word(&ParamExp{
				Short: true,
				IsSet: true,
				Param: lit("foo"),
			}),
			word(&ParamExp{
				Short:  true,
				Length: true,
				Param:  lit("bar"),
			}),
		), LangZsh),
	),
	fileTest(
		[]string{"${${foo#head}%tail}"},
		langFile(&ParamExp{
			NestedParam: &ParamExp{
				Param: lit("foo"),
				Exp: &Expansion{
					Op:   RemSmallPrefix,
					Word: litWord("head"),
				},
			},
			Exp: &Expansion{
				Op:   RemSmallSuffix,
				Word: litWord("tail"),
			},
		}, LangZsh),
	),
	fileTest(
		[]string{`${#"${foo}"}`},
		langFile(&ParamExp{
			Length:      true,
			NestedParam: dblQuoted(&ParamExp{Param: lit("foo")}),
		}, LangZsh),
		flipConfirm2(LangZsh), // TODO: why is this a bad substitution in zsh?
	),
	fileTest(
		[]string{"${$(echo footail)%tail}"},
		langFile(&ParamExp{
			NestedParam: cmdSubst(litStmt("echo", "footail")),
			Exp: &Expansion{
				Op:   RemSmallSuffix,
				Word: litWord("tail"),
			},
		}, LangZsh),
	),
	fileTest(
		[]string{"${foo:u}"},
		langFile(&ParamExp{
			Param:     lit("foo"),
			Modifiers: lits("u"),
		}, LangZsh),
		langFile(&ParamExp{
			Param: lit("foo"),
			Slice: &Slice{Offset: litWord("u")},
		}, LangBash|LangMirBSDKorn),
	),
	fileTest(
		[]string{"${foo:t5:h2:l}"},
		langFile(&ParamExp{
			Param:     lit("foo"),
			Modifiers: lits("t5", "h2", "l"),
		}, LangZsh),
	),
	fileTest(
		[]string{"$${foo}"},
		langFile(word(
			litParamExp("$"),
			lit("{foo}"),
		)),
	),
	fileTest(
		[]string{"${(aO)foo} ${(s/x/)foo}"},
		langFile(call(
			word(&ParamExp{
				Flags: lit("aO"),
				Param: lit("foo"),
			}),
			word(&ParamExp{
				Flags: lit("s/x/"),
				Param: lit("foo"),
			}),
		), LangZsh),
	),
	fileTest(
		[]string{`${:-word}`},
		langErr2("1:3: invalid parameter name"),
		langFile(&ParamExp{
			Exp: &Expansion{
				Op:   DefaultUnsetOrNull,
				Word: litWord("word"),
			},
		}, LangZsh),
	),
	fileTest(
		[]string{`${:+word}`},
		langErr2("1:3: invalid parameter name"),
		langFile(&ParamExp{
			Exp: &Expansion{
				Op:   AlternateUnsetOrNull,
				Word: litWord("word"),
			},
		}, LangZsh),
	),
	fileTest(
		[]string{`${(%):-%N}`},
		langFile(&ParamExp{
			Flags: lit("%"),
			Exp: &Expansion{
				Op:   DefaultUnsetOrNull,
				Word: word(lit("%N")),
			},
		}, LangZsh),
	),
}

// these don't have a canonical format with the same syntax tree
var fileTestsNoPrint = []fileTestCase{
	fileTest(
		[]string{`$[foo]`},
		langFile(word(lit("$"), lit("[foo]")), LangPOSIX),
	),
	fileTest(
		[]string{`"$[foo]"`},
		langFile(dblQuoted(lit("$"), lit("[foo]")), LangPOSIX),
	),
	fileTest(
		[]string{`"$[1 + 3]"`},
		langFile(dblQuoted(arithmExpBr(&BinaryArithm{
			Op: Add,
			X:  litWord("1"),
			Y:  litWord("3"),
		})), LangBash),
	),
}

// these parse with comments
var fileTestsKeepComments = []fileTestCase{
	fileTest(
		[]string{"# foo\ncmd\n# bar"},
		langFile(&File{
			Stmts: []*Stmt{{
				Comments: []Comment{{Text: " foo"}},
				Cmd:      litCall("cmd"),
			}},
			Last: []Comment{{Text: " bar"}},
		}),
	),
	fileTest(
		[]string{"foo # bar # baz"},
		langFile(&File{
			Stmts: []*Stmt{{
				Comments: []Comment{{Text: " bar # baz"}},
				Cmd:      litCall("foo"),
			}},
		}),
	),
	fileTest(
		[]string{
			"$(\n\t# foo\n)",
			"`\n\t# foo\n`",
			"`# foo\n`",
		},
		langFile(&CmdSubst{
			Last: []Comment{{Text: " foo"}},
		}),
	),
	fileTest(
		[]string{
			"`# foo`",
			"` # foo`",
		},
		langFile(&CmdSubst{
			Last: []Comment{{Text: " foo"}},
		}),
	),
}

type sanityChecker struct {
	tb   testing.TB
	src  string
	file *File // nil if not checking a whole file
}

func (c sanityChecker) checkPos(node Node, pos Pos, strs ...string) {
	if !pos.IsValid() {
		c.tb.Fatalf("invalid Pos in %T", node)
	}
	offs := pos.Offset()
	if offs > uint(len(c.src)) {
		c.tb.Errorf("Pos offset %d in %T is out of bounds in %q",
			offs, node, c.src)
		return
	}
	if len(strs) == 0 {
		return
	}
	if strings.Contains(c.src, "<<-") {
		// since the tab indentation in <<- heredoc bodies
		// aren't part of the final literals
		return
	}
	var gotErr string
	for i, want := range strs {
		got := c.src[offs:]
		if i == 0 {
			gotErr = got
		}
		got = strings.ReplaceAll(got, "\x00", "")
		got = strings.ReplaceAll(got, "\r\n", "\n")
		if !strings.Contains(want, "\\\n") {
			// Hack to let "foobar" match the input "foo\\\nbar".
			got = strings.ReplaceAll(got, "\\\n", "")
		}
		if strings.HasPrefix(got, want) {
			return
		}
	}
	c.tb.Errorf("Expected one of %q at %s in %q, found %q",
		strs, pos, c.src, gotErr)
}

func (c sanityChecker) visit(node Node) bool {
	if node == nil {
		return true
	}
	if f := c.file; f != nil {
		if !node.Pos().IsValid() && len(f.Stmts) > 0 {
			c.tb.Fatalf("Invalid Pos")
		}
	}
	if node.Pos().After(node.End()) {
		c.tb.Errorf("Found End() before Pos() in %T", node)
	}
	switch node := node.(type) {
	case *Comment:
		if f := c.file; f != nil {
			if f.Pos().After(node.Pos()) {
				c.tb.Fatalf("A Comment is before its File")
			}
			if node.End().After(f.End()) {
				c.tb.Fatalf("A Comment is after its File")
			}
		}
		c.checkPos(node, node.Hash, "#"+node.Text)
	case *Stmt:
		endOff := int(node.End().Offset())
		if endOff < len(c.src) {
			end := c.src[endOff]
			switch {
			case end == ' ', end == '\n', end == '\t', end == '\r':
				// ended by whitespace
			case regOps(rune(end)):
				// ended by end character
			case endOff > 0 && c.src[endOff-1] == ';':
				// ended by semicolon
			case endOff > 0 && c.src[endOff-1] == '&':
				// ended by & or |&
			case end == '\\' && c.src[endOff+1] == '`':
				// ended by an escaped backquote
			default:
				c.tb.Errorf("Unexpected Stmt.End() %d %q in %q",
					endOff, end, c.src)
			}
		}
		if c.src[node.Position.Offset()] == '#' {
			c.tb.Errorf("Stmt.Pos() should not be a comment")
		}
		c.checkPos(node, node.Position)
		if node.Semicolon.IsValid() {
			c.checkPos(node, node.Semicolon, ";", "&", "|&")
		}
		for _, r := range node.Redirs {
			strs := []string{r.Op.String()}
			// Zsh supports alternate forms of redirect operators:
			// ! instead of |, and >& or >>& prefixes instead of &> and &>>.
			switch r.Op {
			case RdrClob:
				strs = append(strs, ">!")
			case AppClob:
				strs = append(strs, ">>!")
			case RdrAllClob:
				strs = append(strs, "&>!", ">&|", ">&!")
			case AppAll:
				strs = append(strs, ">>&")
			case AppAllClob:
				strs = append(strs, "&>>!", ">>&|", ">>&!")
			}
			c.checkPos(node, r.OpPos, strs...)
		}
	case *Lit:
		pos, end := int(node.Pos().Offset()), int(node.End().Offset())
		want := pos + len(node.Value)
		val := node.Value
		posLine := node.Pos().Line()
		endLine := node.End().Line()
		switch {
		case strings.Contains(c.src, "\\\n"), strings.Contains(c.src, "\\\r\n"):
		case !strings.Contains(node.Value, "\n") && posLine != endLine:
			c.tb.Errorf("Lit without newlines has Pos/End lines %d and %d",
				posLine, endLine)
		case strings.Contains(c.src, "`") && strings.Contains(c.src, "\\"):
			// removed backslashes inside backquote cmd substs
			val = ""
		case end < len(c.src) && (c.src[end] == '\n' || c.src[end] == '`'):
			// heredoc literals that end with the
			// stop word and a newline or closing backquote
		case end == len(c.src):
			// same as above, but with word and EOF
		case end != want:
			c.tb.Errorf("Unexpected Lit %q End() %d (wanted %d for pos %d) in %q",
				val, end, want, pos, c.src)
		}
		c.checkPos(node, node.ValuePos, val)
		c.checkPos(node, node.ValueEnd)
	case *Subshell:
		c.checkPos(node, node.Lparen, "(")
		c.checkPos(node, node.Rparen, ")")
	case *Block:
		c.checkPos(node, node.Lbrace, "{")
		c.checkPos(node, node.Rbrace, "}")
	case *IfClause:
		if node.ThenPos.IsValid() {
			c.checkPos(node, node.Position, "if", "elif")
			c.checkPos(node, node.ThenPos, "then")
		} else {
			c.checkPos(node, node.Position, "else")
		}
		c.checkPos(node, node.FiPos, "fi")
	case *WhileClause:
		rsrv := "while"
		if node.Until {
			rsrv = "until"
		}
		c.checkPos(node, node.WhilePos, rsrv)
		c.checkPos(node, node.DoPos, "do")
		c.checkPos(node, node.DonePos, "done")
	case *ForClause:
		if node.Select {
			c.checkPos(node, node.ForPos, "select")
		} else {
			c.checkPos(node, node.ForPos, "for")
		}
		if node.Braces {
			c.checkPos(node, node.DoPos, "{")
			c.checkPos(node, node.DonePos, "}")
			// Zero out Braces, to not duplicate all the test cases.
			// The printer ignores the field anyway.
			node.Braces = false
		} else {
			c.checkPos(node, node.DoPos, "do")
			c.checkPos(node, node.DonePos, "done")
		}
	case *WordIter:
		if node.InPos.IsValid() {
			c.checkPos(node, node.InPos, "in")
		}
	case *CStyleLoop:
		c.checkPos(node, node.Lparen, "((")
		c.checkPos(node, node.Rparen, "))")
	case *SglQuoted:
		c.checkPos(node, posAddCol(node.End(), -1), "'")
		valuePos := posAddCol(node.Left, 1)
		if node.Dollar {
			valuePos = posAddCol(valuePos, 1)
		}
		val := node.Value
		if strings.Contains(c.src, "`") && strings.Contains(c.src, "\\") {
			// removed backslashes inside backquote cmd substs
			val = ""
		}
		c.checkPos(node, valuePos, val)
		if node.Dollar {
			c.checkPos(node, node.Left, "$'")
		} else {
			c.checkPos(node, node.Left, "'")
		}
		c.checkPos(node, node.Right, "'")
	case *DblQuoted:
		c.checkPos(node, posAddCol(node.End(), -1), `"`)
		if node.Dollar {
			c.checkPos(node, node.Left, `$"`)
		} else {
			c.checkPos(node, node.Left, `"`)
		}
		c.checkPos(node, node.Right, `"`)
	case *UnaryArithm:
		c.checkPos(node, node.OpPos, node.Op.String())
	case *UnaryTest:
		strs := []string{node.Op.String()}
		switch node.Op {
		case TsExists:
			strs = append(strs, "-a")
		case TsSmbLink:
			strs = append(strs, "-h")
		}
		c.checkPos(node, node.OpPos, strs...)
	case *BinaryCmd:
		c.checkPos(node, node.OpPos, node.Op.String())
	case *BinaryArithm:
		c.checkPos(node, node.OpPos, node.Op.String())
	case *BinaryTest:
		strs := []string{node.Op.String()}
		switch node.Op {
		case TsMatch:
			strs = append(strs, "=")
		}
		c.checkPos(node, node.OpPos, strs...)
	case *ParenArithm:
		c.checkPos(node, node.Lparen, "(")
		c.checkPos(node, node.Rparen, ")")
	case *FlagsArithm:
	case *ParenTest:
		c.checkPos(node, node.Lparen, "(")
		c.checkPos(node, node.Rparen, ")")
	case *FuncDecl:
		if node.RsrvWord {
			c.checkPos(node, node.Position, "function")
		} else {
			c.checkPos(node, node.Position)
		}
	case *ParamExp:
		if node.nakedIndex() {
			// Dollar is unset; Pos falls back to Param.
		} else {
			c.checkPos(node, node.Dollar, "$")
		}
		if !node.Short {
			c.checkPos(node, node.Rbrace, "}")
		} else if node.nakedIndex() {
			c.checkPos(node, posAddCol(node.End(), -1), "]")
		}
	case *ArithmExp:
		if node.Bracket {
			// deprecated $(( form
			c.checkPos(node, node.Left, "$[")
			c.checkPos(node, node.Right, "]")
		} else {
			c.checkPos(node, node.Left, "$((")
			c.checkPos(node, node.Right, "))")
		}
	case *ArithmCmd:
		c.checkPos(node, node.Left, "((")
		c.checkPos(node, node.Right, "))")
	case *CmdSubst:
		switch {
		case node.TempFile:
			c.checkPos(node, node.Left, "${ ", "${\t", "${\n")
			c.checkPos(node, node.Right, "}")
		case node.ReplyVar:
			c.checkPos(node, node.Left, "${|")
			c.checkPos(node, node.Right, "}")
		case node.Backquotes:
			c.checkPos(node, node.Left, "`", "\\`")
			c.checkPos(node, node.Right, "`", "\\`")
			// Zero out Backquotes, to not duplicate all the test
			// cases. The printer ignores the field anyway.
			node.Backquotes = false
		default:
			c.checkPos(node, node.Left, "$(")
			c.checkPos(node, node.Right, ")")
		}
	case *CaseClause:
		c.checkPos(node, node.Case, "case")
		if node.Braces {
			c.checkPos(node, node.In, "{")
			c.checkPos(node, node.Esac, "}")
			// Zero out Braces, to not duplicate all the test cases.
			// The printer ignores the field anyway.
			node.Braces = false
		} else {
			c.checkPos(node, node.In, "in")
			c.checkPos(node, node.Esac, "esac")
		}
	case *CaseItem:
		if node.OpPos.IsValid() {
			c.checkPos(node, node.OpPos, node.Op.String(), "esac")
		}
	case *TestClause:
		c.checkPos(node, node.Left, "[[")
		c.checkPos(node, node.Right, "]]")
	case *TimeClause:
		c.checkPos(node, node.Time, "time")
	case *CoprocClause:
		c.checkPos(node, node.Coproc, "coproc")
	case *LetClause:
		c.checkPos(node, node.Let, "let")
	case *TestDecl:
		c.checkPos(node, node.Position, "@test")
	case *ArrayExpr:
		c.checkPos(node, node.Lparen, "(")
		c.checkPos(node, node.Rparen, ")")
	case *ExtGlob:
		c.checkPos(node, node.OpPos, node.Op.String())
		c.checkPos(node, posAddCol(node.End(), -1), ")")
	case *ProcSubst:
		c.checkPos(node, node.OpPos, node.Op.String())
		c.checkPos(node, node.Rparen, ")")
	}
	return true
}
