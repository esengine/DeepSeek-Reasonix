// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"bytes"
	"strings"
	"testing"
)

type simplifyTest struct {
	in, want string
}

func noSimple(in string) simplifyTest {
	return simplifyTest{in: in, want: in}
}

var simplifyTests = [...]simplifyTest{
	// arithmetic exprs
	{"$((a + ((b - c))))", "$((a + (b - c)))"},
	{"$((a + (((b - c)))))", "$((a + (b - c)))"},
	{"$(((b - c)))", "$((b - c))"},
	{"(((b - c)))", "((b - c))"},
	{"${foo[(1)]}", "${foo[1]}"},
	{"${foo:(1):(2)}", "${foo:1:2}"},
	{"a[(1)]=2", "a[1]=2"},
	{"$(($a + ${b}))", "$((a + b))"},
	noSimple("$((${!a} + ${#b}))"),
	noSimple("a[$b]=2"),
	noSimple("${a[$b]}"),
	noSimple("${a[@]}"),
	noSimple("((${a[@]}))"),
	noSimple("((${a[*]}))"),
	noSimple("((${a[0]}))"),
	noSimple("(($3 == $#))"),

	// test exprs
	{`[[ "$foo" == "bar" ]]`, `[[ $foo == "bar" ]]`},
	{`[[ (-z "$foo") ]]`, `[[ -z $foo ]]`},
	{`[[ "a b" > "$c" ]]`, `[[ "a b" > $c ]]`},
	{`[[ ! -n $foo ]]`, `[[ -z $foo ]]`},
	{`[[ ! ! -e a && ! -z $b ]]`, `[[ -e a && -n $b ]]`},
	{`[[ (! a == b) || (! c != d) ]]`, `[[ (a != b) || (c == d) ]]`},
	noSimple(`[[ -n a$b && -n $c ]]`),
	noSimple(`[[ ! -e foo ]]`),
	noSimple(`[[ foo == bar ]]`),
	{`[[ foo = bar ]]`, `[[ foo == bar ]]`},

	// stmts
	{"$( (sts))", "$(sts)"},
	{"( ( (sts)))", "(sts)"},
	noSimple("( (sts) >f)"),
	noSimple("(\n\tx\n\t(sts)\n)"),

	// strings
	noSimple(`"foo"`),
	noSimple(`"foo$bar"`),
	noSimple(`"$bar"`),
	noSimple(`"f'o\\o"`),
	noSimple(`"fo\'o"`),
	noSimple(`"fo\\'o"`),
	noSimple(`"fo\no"`),
	{`"fo\$o"`, `'fo$o'`},
	{`"fo\"o"`, `'fo"o'`},
	{"\"fo\\`o\"", "'fo`o'"},
	noSimple(`fo"o"bar`),
	noSimple(`foo""bar`),
}

func TestSimplify(t *testing.T) {
	t.Parallel()
	parser := NewParser()
	printer := NewPrinter()
	for _, tc := range simplifyTests {
		t.Run("", func(t *testing.T) {
			prog, err := parser.Parse(strings.NewReader(tc.in), "")
			if err != nil {
				t.Fatal(err)
			}
			simplified := Simplify(prog)
			var buf bytes.Buffer
			printer.Print(&buf, prog)
			want := tc.want + "\n"
			if got := buf.String(); got != want {
				t.Fatalf("Simplify mismatch of %q\nwant: %q\ngot:  %q",
					tc.in, want, got)
			}
			if simplified && tc.in == tc.want {
				t.Fatalf("returned true but did not simplify")
			} else if !simplified && tc.in != tc.want {
				t.Fatalf("returned false but did simplify")
			}
		})
	}
}
