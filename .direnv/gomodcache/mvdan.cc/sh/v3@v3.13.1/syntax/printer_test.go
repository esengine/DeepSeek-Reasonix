// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestPrintFiles(t *testing.T) {
	t.Parallel()
	for lang := range langResolvedVariants.bits() {
		t.Run(lang.String(), func(t *testing.T) {
			parser := NewParser(Variant(lang), KeepComments(true))
			printer := NewPrinter()
			for _, c := range append(fileTests, fileTestsKeepComments...) {
				want, _ := c.byLangIndex[lang.index()].(*File)
				if want == nil {
					continue
				}
				if wantBash, _ := c.byLangIndex[LangBash.index()].(*File); wantBash != nil &&
					lang != LangBash && want != wantBash {
					// Skip cases where a non-Bash language parses differently than Bash.
					// For example, `((foo))` prints as `( (foo))` only when in POSIX.
					continue
				}
				t.Run("", func(t *testing.T) {
					in := c.inputs[0]
					printTest(t, parser, printer, in, in)
				})
			}
		})
	}
}

func strPrint(p *Printer, node Node) (string, error) {
	var buf bytes.Buffer
	err := p.Print(&buf, node)
	return buf.String(), err
}

type printCase struct {
	in, want string
}

func samePrint(s string) printCase { return printCase{in: s, want: s} }

var printTests = []printCase{
	samePrint(`fo○ b\år`),
	samePrint(`"fo○ b\år"`),
	samePrint(`'fo○ b\år'`),
	samePrint(`${a#fo○ b\år}`),
	samePrint(`#fo○ b\år`),
	samePrint("<<EOF\nfo○ b\\år\nEOF"),
	samePrint(`$'○ b\år'`),
	samePrint("${a/b//○}"),
	{strings.Repeat(" ", bufSize-3) + "○", "○"}, // at the end of a chunk
	{strings.Repeat(" ", bufSize-0) + "○", "○"}, // at the start of a chunk
	{strings.Repeat(" ", bufSize-2) + "○", "○"}, // split after 1st byte
	{strings.Repeat(" ", bufSize-1) + "○", "○"}, // split after 2nd byte
	// peekByte that would (but cannot) go to the next chunk
	{strings.Repeat(" ", bufSize-2) + ">(a)", ">(a)"},
	// escaped newline at end of chunk
	{"a" + strings.Repeat(" ", bufSize-2) + "\\\nb", "a \\\n\tb"},
	// panics if padding is only 4 (utf8.UTFMax)
	{strings.Repeat(" ", bufSize-10) + "${a/b//○}", "${a/b//○}"},
	// multiple p.fill calls
	{"a" + strings.Repeat(" ", bufSize*4) + "b", "a b"},
	// newline at the beginning of second chunk
	{"a" + strings.Repeat(" ", bufSize-2) + "\nb", "a\nb"},
	{"foo; bar", "foo\nbar"},
	{"foo\n\n\nbar", "foo\n\nbar"},
	{"foo\n\n", "foo"},
	{"\n\nfoo", "foo"},
	{"# foo \n # bar\t", "# foo\n# bar"},
	samePrint("#"),
	samePrint("#c1\\\n#c2"),
	samePrint("#\\\n#"),
	{"#\\\r\n#", "#\\\n#"},
	samePrint("{\n\tfoo\n\t# bar \\\n}"),
	samePrint("foo\\\\\nbar"),
	samePrint("a=b # inline\nbar"),
	samePrint("a=$(b) # inline"),
	samePrint("foo # inline\n# after"),
	samePrint("$(a) $(b)"),
	{"if a\nthen\n\tb\nfi", "if a; then\n\tb\nfi"},
	samePrint("if a; then\n\tb\nelse\n\tc\nfi"),
	{"if a; then b\nelse c\nfi", "if a; then\n\tb\nelse\n\tc\nfi"},
	samePrint("foo >&2 <f bar"),
	samePrint("foo >&2 bar <f"),
	samePrint(">&2 foo bar <f"),
	samePrint(">&2 foo"),
	samePrint(">&2 foo 2>&1 bar <f"),
	{"foo >&2>/dev/null", "foo >&2 >/dev/null"},
	{"foo <<EOF bar\nl1\nEOF", "foo bar <<EOF\nl1\nEOF"},
	samePrint("foo <<\\\\\\\\EOF\nbar\n\\\\EOF"),
	samePrint("foo <<\"\\EOF\"\nbar\n\\EOF"),
	samePrint("foo <<\"EOF\"\nbar\nEOF\nbar"),
	samePrint("foo <<EOF && bar\nl1\nEOF"),
	samePrint("foo <<EOF &&\nl1\nEOF\n\tbar"),
	samePrint("foo <<EOF\nl1\nEOF\n\nfoo2"),
	samePrint("<<EOF\nfoo\\\nbar\nEOF"),
	samePrint("<<'EOF'\nfoo\\\nbar\nEOF"),
	samePrint("<<EOF\n\\\n$foo\nEOF"),
	samePrint("<<'EOF'\n\\\nEOF"),
	samePrint("{\n\t<<EOF\nfoo\\\nbar\nEOF\n}"),
	samePrint("{\n\t<<'EOF'\nfoo\\\nbar\nEOF\n}"),
	samePrint("<<EOF\nEOF"),
	samePrint("foo <<EOF\nEOF\n\nbar"),
	samePrint("foo <<'EOF'\nEOF\n\nbar"),
	{
		"{ foo; bar; }",
		"{\n\tfoo\n\tbar\n}",
	},
	{
		"{ foo; bar; }\n#etc",
		"{\n\tfoo\n\tbar\n}\n#etc",
	},
	{
		"{\n\tfoo; }",
		"{\n\tfoo\n}",
	},
	{
		"{ foo\n}",
		"{\n\tfoo\n}",
	},
	{
		"(foo\n)",
		"(\n\tfoo\n)",
	},
	{
		"$(foo\n)",
		"$(\n\tfoo\n)",
	},
	{
		"a\n\n\n# etc\nb",
		"a\n\n# etc\nb",
	},
	{
		"a b\\\nc d",
		"a bc d",
	},
	{
		"a bb\\\ncc d",
		"a bbcc d",
	},
	samePrint("a \\\n\tb \\\n\t\"c\" \\\n\t;"),
	samePrint("a=1 \\\n\tb=2 \\\n\tc=\"3\" \\\n\t;"),
	{
		"a=\\\nfoo\nb=\\\n\"bar\"\nc=\\\n'baz'",
		"a=foo\nb=\"bar\"\nc='baz'",
	},

	samePrint("if a \\\n\t; then b; fi"),
	samePrint("a > \\\n\tfoo"),
	samePrint("a <<< \\\n\t\"foo\""),
	samePrint("a 'b\nb' c"),
	samePrint("a $'b\nb' c"),
	{
		"(foo; bar)",
		"(\n\tfoo\n\tbar\n)",
	},
	{
		"{\nfoo\nbar; }",
		"{\n\tfoo\n\tbar\n}",
	},
	samePrint("\"$foo\"\n{\n\tbar\n}"),
	{
		"{\nbar\n# extra\n}",
		"{\n\tbar\n\t# extra\n}",
	},
	{
		"foo\nbar  # extra",
		"foo\nbar # extra",
	},
	{
		"foo # 1\nfooo # 2\nfo # 3",
		"foo  # 1\nfooo # 2\nfo   # 3",
	},
	{
		" foo # 1\n fooo # 2\n fo # 3",
		"foo  # 1\nfooo # 2\nfo   # 3",
	},
	{
		"foo   # 1\nfooo  # 2\nfo    # 3",
		"foo  # 1\nfooo # 2\nfo   # 3",
	},
	{
		"foooooa\nfoo # 1\nfooo # 2\nfo # 3\nfooooo",
		"foooooa\nfoo  # 1\nfooo # 2\nfo   # 3\nfooooo",
	},
	{
		"foo\nbar\nfoo # 1\nfooo # 2",
		"foo\nbar\nfoo  # 1\nfooo # 2",
	},
	samePrint("foobar # 1\nfoo\nfoo # 2"),
	samePrint("foobar # 1\n#foo\nfoo # 2"),
	samePrint("foobar # 1\n\nfoo # 2"),
	{
		"foo # 2\nfoo2 bar # 1",
		"foo      # 2\nfoo2 bar # 1",
	},
	{
		"foo bar # 1\n! foo # 2",
		"foo bar # 1\n! foo   # 2",
	},
	{
		"aa #b\nc  #d\ne\nf #g",
		"aa #b\nc  #d\ne\nf #g",
	},
	{
		"{ a; } #x\nbbb #y\n{ #z\nc\n}",
		"{ a; } #x\nbbb    #y\n{      #z\n\tc\n}",
	},
	{
		"foo; foooo # 1",
		"foo\nfoooo # 1",
	},
	{
		"aaa; b #1\nc #2",
		"aaa\nb #1\nc #2",
	},
	{
		"a #1\nbbb; c #2\nd #3",
		"a #1\nbbb\nc #2\nd #3",
	},
	samePrint("aa #c1\n{  #c2\n\tb\n}"),
	{
		"aa #c1\n{ b; c; } #c2",
		"aa #c1\n{\n\tb\n\tc\n} #c2",
	},
	samePrint("a #c1\n'b\ncc' #c2"),
	{
		"(\nbar\n# extra\n)",
		"(\n\tbar\n\t# extra\n)",
	},
	{
		"for a in 1 2\ndo\n\t# bar\n\tx\ndone",
		"for a in 1 2; do\n\t# bar\n\tx\ndone",
	},
	samePrint("#before\nfoo | bar"),
	samePrint("#before\nfoo && bar"),
	samePrint("foo | bar # inline"),
	samePrint("foo && bar # inline"),
	samePrint("foo `# inline` \\\n\tbar"),
	samePrint("for a in 1 2; do\n\n\tbar\ndone"),
	{
		"a \\\n\t&& b",
		"a &&\n\tb",
	},
	{
		"a \\\n\t&& b\nc",
		"a &&\n\tb\nc",
	},
	{
		"{\n(a \\\n&& b)\nc\n}",
		"{\n\t(a &&\n\t\tb)\n\tc\n}",
	},
	{
		"a && b \\\n&& c",
		"a && b &&\n\tc",
	},
	{
		"a \\\n&& $(b) && c \\\n&& d",
		"a &&\n\t$(b) && c &&\n\td",
	},
	{
		"a \\\n&& b\nc \\\n&& d",
		"a &&\n\tb\nc &&\n\td",
	},
	{
		"a \\\n&&\n#c\nb",
		"a &&\n\t#c\n\tb",
	},
	{
		"a | {\nb \\\n| c\n}",
		"a | {\n\tb |\n\t\tc\n}",
	},
	{
		"a \\\n\t&& if foo; then\nbar\nfi",
		"a &&\n\tif foo; then\n\t\tbar\n\tfi",
	},
	{
		"if\nfoo\nthen\nbar\nfi",
		"if\n\tfoo\nthen\n\tbar\nfi",
	},
	{
		"if foo \\\nbar\nthen\nbar\nfi",
		"if foo \\\n\tbar; then\n\tbar\nfi",
	},
	{
		"if foo \\\n&& bar\nthen\nbar\nfi",
		"if foo &&\n\tbar; then\n\tbar\nfi",
	},
	{
		"a |\nb |\nc",
		"a |\n\tb |\n\tc",
	},
	samePrint("a |\n\tb | c |\n\td"),
	samePrint("a | b |\n\tc |\n\td"),
	{
		"foo |\n# misplaced\nbar",
		"foo |\n\t# misplaced\n\tbar",
	},
	samePrint("{\n\tfoo\n\t#a\n\tbar\n} | etc"),
	{
		"foo &&\n#a1\n#a2\n$(bar)",
		"foo &&\n\t#a1\n\t#a2\n\t$(bar)",
	},
	{
		"{\n\tfoo\n\t#a\n} |\n# misplaced\nbar",
		"{\n\tfoo\n\t#a\n} |\n\t# misplaced\n\tbar",
	},
	samePrint("foo | bar\n#after"),
	{
		"a |\nb | #c2\nc",
		"a |\n\tb | #c2\n\tc",
	},
	{
		"{\nfoo &&\n#a1\n#a2\n$(bar)\n}",
		"{\n\tfoo &&\n\t\t#a1\n\t\t#a2\n\t\t$(bar)\n}",
	},
	{
		"foo | while read l; do\nbar\ndone",
		"foo | while read l; do\n\tbar\ndone",
	},
	samePrint("while x; do\n\t#comment\n\ty\ndone"),
	{
		"while x\ndo\n\ty\ndone",
		"while x; do\n\ty\ndone",
	},
	samePrint("\"\\\nfoo\""),
	samePrint("'\\\nfoo'"),
	samePrint("\"foo\\\n  bar\""),
	samePrint("'foo\\\n  bar'"),
	samePrint("v=\"\\\nfoo\""),
	{
		"v=foo\\\nbar",
		"v=foobar",
	},
	{
		"v='foo'\\\n'bar'",
		"v='foo''bar'",
	},
	{
		"v=\\\n\"foo\"",
		"v=\"foo\"",
	},
	{
		"v=\\\nfoo\\\n$bar",
		"v=foo$bar",
	},
	samePrint("\"\\\n\\\nfoo\\\n\\\n\""),
	samePrint("'\\\n\\\nfoo\\\n\\\n'"),
	{
		"foo \\\n>bar\netc",
		"foo \\\n\t>bar\netc",
	},
	{
		"foo \\\nfoo2 \\\n>bar",
		"foo \\\n\tfoo2 \\\n\t>bar",
	},
	samePrint("> >(foo)"),
	samePrint("x > >(foo) y"),
	samePrint("a | (x) |\n\tb"),
	samePrint("a | (\n\tx\n\ty\n) |\n\tb"),
	samePrint("a |\n\tif foo; then\n\t\tbar\n\tfi |\n\tb"),
	samePrint("a | if foo; then\n\tbar\nfi"),
	samePrint("a | b | if foo; then\n\tbar\nfi"),
	{
		"case $i in\n1)\nfoo\n;;\nesac",
		"case $i in\n1)\n\tfoo\n\t;;\nesac",
	},
	{
		"case $i in\n1)\nfoo\nesac",
		"case $i in\n1)\n\tfoo\n\t;;\nesac",
	},
	{
		"case $i in\n1) foo\nesac",
		"case $i in\n1) foo ;;\nesac",
	},
	{
		"case $i in\n1) foo; bar\nesac",
		"case $i in\n1)\n\tfoo\n\tbar\n\t;;\nesac",
	},
	{
		"case $i in\n1) foo; bar;;\nesac",
		"case $i in\n1)\n\tfoo\n\tbar\n\t;;\nesac",
	},
	{
		"case $i in\n1)\n#foo \t\n;;\nesac",
		"case $i in\n1)\n\t#foo\n\t;;\nesac",
	},
	samePrint("case $i in\n1)\n\t;;\n\n2)\n\t;;\nesac"),
	{
		"case $i\nin\n1)\n\t;;\nesac",
		"case $i in\n1)\n\t;;\nesac",
	},
	samePrint("case $i in\n1)\n\ta\n\t#b\n\t;;\nesac"),
	samePrint("case $i in\n1) foo() { bar; } ;;\nesac"),
	samePrint("case $i in\n1) ;; #foo\nesac"),
	samePrint("case $i in\n#foo\nesac"),
	samePrint("case $i in\n#before\n1) ;;\nesac"),
	samePrint("case $i in\n#bef\n1) ;; #inl\nesac"),
	samePrint("case $i in\n#before1\n'1') ;;\n#before2\n'2') ;;\nesac"),
	samePrint("case $i in\n1) ;; #inl1\n2) ;; #inl2\nesac"),
	samePrint("case $i in\n#bef\n1) #inl\n\tfoo\n\t;;\nesac"),
	samePrint("case $i in\n1) #inl\n\t;;\nesac"),
	samePrint("case $i in\n1) a \\\n\tb ;;\nesac"),
	samePrint("case $i in\n1 | 2 | \\\n\t3 | 4) a b ;;\nesac"),
	samePrint("case $i in\n1 | 2 | \\\n\t3 | 4)\n\ta b\n\t;;\nesac"),
	samePrint("case $i in\nx) ;;\ny) for n in 1; do echo $n; done ;;\nesac"),
	samePrint("case a in b) [[ x =~ y ]] ;; esac"),
	samePrint("case a in b) [[ a =~ b$ || c =~ d$ ]] ;; esac"),
	samePrint("case a in b) [[ a =~ (b) ]] ;; esac"),
	samePrint("[[ (a =~ b$) ]]"),
	samePrint("[[ a && ((b || c) && d) ]]"),
	samePrint("[[ a &&\n\tb ]]"),
	samePrint("[[ a ||\n\tb ]]"),
	{
		"[[ -f \\\n\tfoo ]]",
		"[[ -f foo ]]",
	},
	{
		"[[ foo \\\n\t-ef \\\n\tbar ]]",
		"[[ foo -ef bar ]]",
	},
	{
		"[[ a && \\\nb \\\n && c ]]",
		"[[ a &&\n\tb &&\n\tc ]]",
	},
	samePrint("{\n\t[[ a || b ]]\n}"),
	{
		"a=(\nb\nc\n) b=c",
		"a=(\n\tb\n\tc\n) b=c",
	},
	samePrint("a=(\n\t#before\n\tb #inline\n)"),
	samePrint("a=(\n\tb #foo\n\tc #bar\n)"),
	samePrint("a=(\n\tb\n\n\t#foo\n\t#bar\n\tc\n)"),
	samePrint("a=(\n\t#foo\n\t#bar\n\tc\n)"),
	samePrint("a=(\n\t#lone\n)"),
	samePrint("a=(\n\n)"),
	samePrint("a=(\n\tx\n\n\ty\n)"),
	samePrint("foo <<EOF | $(bar)\n3\nEOF"),
	{
		"a <<EOF\n$(\n\tb\n\tc)\nEOF",
		"a <<EOF\n$(\n\tb\n\tc\n)\nEOF",
	},
	samePrint("<<EOF1\n$(\n\t<<EOF2\ninner\nEOF2\n)\nEOF1"),
	{
		"<(<<EOF\nbody\nEOF\n)",
		"<(\n\t<<EOF\nbody\nEOF\n)",
	},
	{
		"( (foo) )\n$( (foo) )\n<( (foo) )",
		"( (foo))\n$( (foo))\n<((foo))",
	},
	{
		"if ( ((foo)) || bar ); then baz; fi",
		"if ( ((foo)) || bar); then baz; fi",
	},
	samePrint("if x; then (\n\ty\n) & fi"),
	samePrint("\"foo\n$(bar)\""),
	samePrint("\"foo\\\n$(bar)\""),
	samePrint("\"foo\\\nbar\""),
	samePrint("((foo++)) || bar"),
	{
		"a=b \\\nc=d \\\nfoo",
		"a=b \\\n\tc=d \\\n\tfoo",
	},
	{
		"a=b \\\nc=d \\\nfoo \\\nbar",
		"a=b \\\n\tc=d \\\n\tfoo \\\n\tbar",
	},
	samePrint("a $(x) \\\n\tb"),
	samePrint("\"foo\nbar\"\netc"),
	samePrint("\"foo\nbar\nbar2\"\netc"),
	samePrint("a=\"$b\n\"\nd=e"),
	samePrint("\"\n\"\n\nfoo"),
	samePrint("$\"\n\"\n\nfoo"),
	samePrint("'\n'\n\nfoo"),
	samePrint("$'\n'\n\nfoo"),
	samePrint("foo <<EOF\na\nb\nc\nd\nEOF\n{\n\tbar\n}"),
	samePrint("foo bar # one\nif a; then\n\tb\nfi # two"),
	{
		"# foo\n\n\nbar",
		"# foo\n\nbar",
	},
	{
		"# foo\n\n\nbar\nbaz",
		"# foo\n\nbar\nbaz",
	},
	samePrint("#foo\n#\n#bar"),
	{
		"(0 #\n0)#\n0",
		"(\n\t0 #\n\t0\n) #\n0",
	},
	samePrint("a | #c1\n\t(\n\t\tb\n\t)"),
	samePrint("a | #c1\n\t{\n\t\tb\n\t}"),
	samePrint("a | #c1\n\tif b; then\n\t\tc\n\tfi"),
	samePrint("a | #c1\n\t#c2\n\t#c3\n\tb"),
	samePrint("a && #c1\n\t(\n\t\tb\n\t)"),
	samePrint("f() body # comment"),
	samePrint("f <<EOF\nbody\nEOF"),
	samePrint("f <<EOF\nEOF"),
	samePrint("f <<-EOF\n\tbody\nEOF"),
	{
		"f <<-EOF\nbody\nEOF",
		"f <<-EOF\n\tbody\nEOF",
	},
	samePrint("f <<-EOF\nEOF"),
	samePrint("f <<-EOF\n\n\nEOF"),
	samePrint("f <<-EOF\n\n\tindented\n\nEOF"),
	samePrint("{\n\tf <<EOF\nEOF\n}"),
	samePrint("{\n\tf <<-EOF\n\t\tbody\n\tEOF\n}"),
	samePrint("{\n\tf <<-EOF\n\t\tbody\n\tEOF\n\tf2\n}"),
	samePrint("f <<-EOF\n\t{\n\t\tnicely indented\n\t}\nEOF"),
	samePrint("f <<-EOF\n\t{\n\t\tnicely indented\n\t}\nEOF"),
	{
		"f <<-EOF\n\t{\nbadly indented\n\t}\nEOF",
		"f <<-EOF\n\t{\n\tbadly indented\n\t}\nEOF",
	},
	{
		"f <<-EOF\n\t\t{\n\t\t\ttoo indented\n\t\t}\nEOF",
		"f <<-EOF\n\t{\n\t\ttoo indented\n\t}\nEOF",
	},
	{
		"f <<-EOF\n{\n\ttoo little indented\n}\nEOF",
		"f <<-EOF\n\t{\n\t\ttoo little indented\n\t}\nEOF",
	},
	samePrint("<<-EOF\n\t$foo\nEOF\n\n{\n\tbar\n}"),
	samePrint("f <<EOF\nEOF\n# comment"),
	samePrint("f <<EOF\nEOF\n# comment\nbar"),
	samePrint("f <<EOF # inline\n$(\n\t# inside\n)\nEOF\n# outside\nbar"),
	samePrint("while foo; do\n\tbar\ndone <<-EOF # inline\n\tbaz\nEOF"),
	samePrint("{\n\tcat <<EOF\nEOF\n\t# comment\n}"),
	{
		"if foo # inline\nthen\n\tbar\nfi",
		"if foo; then # inline\n\tbar\nfi",
	},
	samePrint("for i; do echo $i; done"),
	samePrint("for i in; do echo $i; done"),
	{
		"for foo in a b # inline\ndo\n\tbar\ndone",
		"for foo in a b; do # inline\n\tbar\ndone",
	},
	{
		"if x # inline\nthen bar; fi",
		"if x; then # inline\n\tbar\nfi",
	},
	{
		"for i in a b # inline\ndo bar; done",
		"for i in a b; do # inline\n\tbar\ndone",
	},
	{
		"for i #a\n\tin 1; do #b\nx\ndone",
		"for i in \\\n\t1; do #a\n\t#b\n\tx\ndone",
	},
	{
		"foo() # inline\n{\n\tbar\n}",
		"foo() { # inline\n\tbar\n}",
	},
	{
		"foo() #before\n(\n\tbar #inline\n)",
		"foo() ( #before\n\tbar #inline\n)",
	},
	{
		"foo() (#before\n\tbar #inline\n)",
		"foo() ( #before\n\tbar #inline\n)",
	},
	{
		"foo()\n#before-1\n(#before-2\n\tbar #inline\n)",
		"foo() ( #before-1\n\t#before-2\n\tbar #inline\n)",
	},
	{
		"(#before\n\tbar #inline\n)",
		"( #before\n\tbar #inline\n)",
	},
	{
		"(\n#before\n\tbar #inline\n)",
		"(\n\t#before\n\tbar #inline\n)",
	},
	{
		"foo=$(#before\n\tbar #inline\n)",
		"foo=$( #before\n\tbar #inline\n)",
	},
	{
		"foo=`#before\nbar`",
		"foo=$( #before\n\tbar\n)",
	},
	samePrint("if foo; then\n\tbar\n\t# comment\nfi"),
	samePrint("if foo; then\n\tbar\n# else commented out\nfi"),
	samePrint("if foo; then\n\tx\nelse\n\tbar\n\t# comment\nfi"),
	samePrint("if foo; then\n\tx\n#comment\nelse\n\ty\nfi"),
	samePrint("if foo; then\n\tx\n\t#comment\nelse\n\ty\nfi"),
	{
		"if foo; then\n\tx\n#a\n\t#b\n\t#c\nelse\n\ty\nfi",
		"if foo; then\n\tx\n\t#a\n\t#b\n\t#c\nelse\n\ty\nfi",
	},
	samePrint("if foo; then\n\tx\nelse #comment\n\ty\nfi"),
	samePrint("if foo; then\n\tx\n#comment\nelif bar; then\n\ty\nfi"),
	samePrint("if foo; then\n\tx\n\t#comment\nelif bar; then\n\ty\nfi"),
	samePrint("case i in\nx)\n\ta\n\t;;\n#comment\ny) ;;\nesac"),
	samePrint("case i in\nx)\n\ta\n\t;;\n\t#comment\ny) ;;\nesac"),
	{
		"case i in\nx)\n\ta\n\t;;\n\t#a\n#b\n\t#c\ny) ;;\nesac",
		"case i in\nx)\n\ta\n\t;;\n\t#a\n\t#b\n\t#c\ny) ;;\nesac",
	},
	samePrint("'foo\tbar'\n'foooo\tbar'"),
	samePrint("\"foo\tbar\"\n\"foooo\tbar\""),
	samePrint("foo\\\tbar\nfoooo\\\tbar"),
	samePrint("#foo\tbar\n#foooo\tbar"),
	{
		"array=('one'\n\t\t# 'two'\n\t\t'three')",
		"array=('one'\n\t# 'two'\n\t'three')",
	},
	samePrint("#comment\n>redir"),
	{
		">redir \\\n\tfoo",
		">redir foo",
	},
	samePrint("$(declare)"),
	{
		"`declare`",
		"$(declare)",
	},
	{
		"(\n(foo >redir))",
		"(\n\t(foo >redir)\n)",
	},
	{
		"( (foo) )",
		"( (foo))",
	},
	{
		"( (foo); bar )",
		"(\n\t(foo)\n\tbar\n)",
	},
	{
		"( ((foo++)) )",
		"( ((foo++)))",
	},
	{
		"( ((foo++)); bar )",
		"(\n\t((foo++))\n\tbar\n)",
	},
	samePrint("(\n\t((foo++))\n)"),
	samePrint("(foo && bar)"),
	samePrint(`$foo#bar ${foo}#bar 'foo'#bar "foo"#bar`),
	samePrint("case $i in\n1)\n\ta\n\t;;\n(esac)\n\tb\n\t;;\nesac"),
	samePrint("case $i in\n1)\n\ta\n\t;;\n(esac | 2)\n\tb\n\t;;\nesac"),
	samePrint("case $i in\n1)\n\ta\n\t;;\n2 | esac)\n\tb\n\t;;\nesac"),
	// TODO: support cases with command substitutions as well
	// {
	// 	"`foo`#bar",
	// 	"$(foo)#bar",
	// },
	// samePrint(`$("foo"#bar)#bar`),
	samePrint("${array[dash - string]}"),
	samePrint("array[dash - string]=123"),
	samePrint("${arr[0,1]}"),
	samePrint("arr[0,1]=x"),
}

func TestPrintTable(t *testing.T) {
	t.Parallel()
	parser := NewParser(KeepComments(true))
	printer := NewPrinter()
	for i, tc := range printTests {
		t.Run(fmt.Sprintf("#%03d", i), func(t *testing.T) {
			printTest(t, parser, printer, tc.in, tc.want)
		})
		t.Run(fmt.Sprintf("#%03d-nl", i), func(t *testing.T) {
			printTest(t, parser, printer, "\n"+tc.in+"\n", tc.want)
		})
		t.Run(fmt.Sprintf("#%03d-redo", i), func(t *testing.T) {
			printTest(t, parser, printer, tc.want, tc.want)
		})
	}
}

func parsePath(tb testing.TB, path string) *File {
	f, err := os.Open(path)
	if err != nil {
		tb.Fatal(err)
	}
	defer f.Close()
	prog, err := NewParser(KeepComments(true)).Parse(f, "")
	if err != nil {
		tb.Fatal(err)
	}
	return prog
}

const canonicalPath = "canonical.sh"

func TestPrintMultiline(t *testing.T) {
	t.Parallel()
	prog := parsePath(t, canonicalPath)
	got, err := strPrint(NewPrinter(), prog)
	if err != nil {
		t.Fatal(err)
	}

	wantBs, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatal(err)
	}

	// If we're on Windows and it was set up to automatically replace LF
	// with CRLF, that might make this test fail. Just ignore \r characters.
	want := strings.ReplaceAll(string(wantBs), "\r", "")
	got = strings.ReplaceAll(got, "\r", "")
	if got != want {
		t.Fatalf("Print mismatch in canonical.sh")
	}
}

func TestPrintSpaces(t *testing.T) {
	t.Parallel()
	spaceFormats := [...]struct {
		spaces   uint
		in, want string
	}{
		{
			0,
			"{\nfoo \\\nbar\n}",
			"{\n\tfoo \\\n\t\tbar\n}",
		},
		{
			2,
			"{\nfoo \\\nbar\n}",
			"{\n  foo \\\n    bar\n}",
		},
		{
			4,
			"{\nfoo \\\nbar\n}",
			"{\n    foo \\\n        bar\n}",
		},
		{
			2,
			"if foo; then # inline1\nbar # inline2\n# withfi\nfi",
			"if foo; then # inline1\n  bar        # inline2\n# withfi\nfi",
		},
		{
			2,
			"array=('one'\n    # 'two'\n    'three')",
			"array=('one'\n  # 'two'\n  'three')",
		},
	}

	parser := NewParser(KeepComments(true))
	for _, tc := range spaceFormats {
		t.Run("", func(t *testing.T) {
			printer := NewPrinter(Indent(tc.spaces))
			printTest(t, parser, printer, tc.in, tc.want)
		})
	}
}

var errBadWriter = fmt.Errorf("write: expected error")

type badWriter struct{}

func (b badWriter) Write(p []byte) (int, error) { return 0, errBadWriter }

func TestWriteErr(t *testing.T) {
	t.Parallel()
	f := &File{Stmts: []*Stmt{
		{
			Redirs: []*Redirect{{
				Op:   RdrOut,
				Word: litWord("foo"),
			}},
			Cmd: &Subshell{},
		},
	}}
	err := NewPrinter().Print(badWriter{}, f)
	if err == nil {
		t.Fatalf("Expected error with bad writer")
	}
	if err != errBadWriter {
		t.Fatalf("Error mismatch with bad writer:\nwant: %v\ngot:  %v",
			errBadWriter, err)
	}
}

func TestPrintBinaryNextLine(t *testing.T) {
	t.Parallel()
	tests := [...]printCase{
		{
			"foo <<EOF &&\nl1\nEOF\nbar",
			"foo <<EOF && bar\nl1\nEOF",
		},
		samePrint("a \\\n\t&& b"),
		samePrint("a \\\n\t&& b\nc"),
		{
			"{\n(a \\\n&& b)\nc\n}",
			"{\n\t(a \\\n\t\t&& b)\n\tc\n}",
		},
		{
			"a && b \\\n&& c",
			"a && b \\\n\t&& c",
		},
		{
			"a \\\n&& $(b) && c \\\n&& d",
			"a \\\n\t&& $(b) && c \\\n\t&& d",
		},
		{
			"a \\\n&& b\nc \\\n&& d",
			"a \\\n\t&& b\nc \\\n\t&& d",
		},
		{
			"a | {\nb \\\n| c\n}",
			"a | {\n\tb \\\n\t\t| c\n}",
		},
		{
			"a \\\n\t&& if foo; then\nbar\nfi",
			"a \\\n\t&& if foo; then\n\t\tbar\n\tfi",
		},
		{
			"if foo \\\n&& bar\nthen\nbar\nfi",
			"if foo \\\n\t&& bar; then\n\tbar\nfi",
		},
		{
			"a |\nb |\nc",
			"a \\\n\t| b \\\n\t| c",
		},
		{
			"foo |\n# misplaced\nbar",
			"foo \\\n\t|\n\t# misplaced\n\tbar",
		},
		samePrint("{\n\tfoo\n\t#a\n\tbar\n} | etc"),
		{
			"foo &&\n#a1\n#a2\n$(bar)",
			"foo \\\n\t&&\n\t#a1\n\t#a2\n\t$(bar)",
		},
		{
			"{\n\tfoo\n\t#a\n} |\n# misplaced\nbar",
			"{\n\tfoo\n\t#a\n} \\\n\t|\n\t# misplaced\n\tbar",
		},
		samePrint("foo | bar\n#after"),
		{
			"a |\nb | #c2\nc",
			"a \\\n\t| b \\\n\t|\n\t#c2\n\tc",
		},
		samePrint("a \\\n\t&"),
		{
			"a \\\n\tb \\\n\tc \\\n\t# EOC",
			"a \\\n\tb \\\n\tc\n# EOC",
		},
		{
			"a \\\n\t| b \\\n\t| c \\\n\t# EOC",
			"a \\\n\t| b \\\n\t| c\n# EOC",
		},
	}
	parser := NewParser(KeepComments(true))
	printer := NewPrinter(BinaryNextLine(true))
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			printTest(t, parser, printer, tc.in, tc.want)
		})
	}
}

func TestPrintSwitchCaseIndent(t *testing.T) {
	t.Parallel()
	tests := [...]printCase{
		{
			"case $i in\n1)\nfoo\n;;\nesac",
			"case $i in\n\t1)\n\t\tfoo\n\t\t;;\nesac",
		},
		{
			"case $i in\n1)\na\n;;\n2)\nb\n;;\nesac",
			"case $i in\n\t1)\n\t\ta\n\t\t;;\n\t2)\n\t\tb\n\t\t;;\nesac",
		},
		samePrint("case $i in\n\t#foo\nesac"),
	}
	parser := NewParser(KeepComments(true))
	printer := NewPrinter(SwitchCaseIndent(true))
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			printTest(t, parser, printer, tc.in, tc.want)
		})
	}
}

func TestPrintFunctionNextLine(t *testing.T) {
	t.Parallel()
	tests := [...]printCase{
		{
			"foo() { bar; }",
			"foo()\n{\n\tbar\n}",
		},
		{
			"foo()\n{ bar; }",
			"foo()\n{\n\tbar\n}",
		},
		{
			"foo()\n\n{\n\n\tbar\n}",
			"foo()\n{\n\n\tbar\n}",
		},
		{
			"function foo {\n\tbar\n}",
			"function foo\n{\n\tbar\n}",
		},
		{
			"function foo() {\n\tbar\n}",
			"function foo()\n{\n\tbar\n}",
		},
		{
			"{ foo() { bar; }; }",
			"{\n\tfoo()\n\t{\n\t\tbar\n\t}\n}",
		},
	}
	parser := NewParser(KeepComments(true))
	printer := NewPrinter(FunctionNextLine(true))
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			printTest(t, parser, printer, tc.in, tc.want)
		})
	}
}

func TestPrintSpaceRedirects(t *testing.T) {
	t.Parallel()
	tests := [...]printCase{
		samePrint("echo foo bar > f"),
		samePrint("echo > f foo bar"),
		samePrint("echo >(cmd)"),
		samePrint("echo > >(cmd)"),
		samePrint("<< EOF\nfoo\nEOF"),
		samePrint("<<- EOF\n\t$(< foo)\nEOF"),
		samePrint("echo 2> f"),
		samePrint("echo foo bar >&1"),
		samePrint("echo 2<&1 foo bar"),
	}
	parser := NewParser(KeepComments(true))
	printer := NewPrinter(SpaceRedirects(true))
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			printTest(t, parser, printer, tc.in, tc.want)
		})
	}
}

func TestPrintKeepPadding(t *testing.T) {
	t.Parallel()
	tests := [...]printCase{
		samePrint("echo foo bar"),
		samePrint("echo  foo   bar"),
		samePrint("a=b  c=d   bar"),
		samePrint("echo foo    >bar"),
		samePrint("echo foo    2>bar"),
		samePrint("{ foo;  }"),
		samePrint("a()   { foo; }"),
		samePrint("a   && b"),
		samePrint("a   | b"),
		samePrint("a |  b"),
		samePrint("{  a b c; }"),
		samePrint("foo    # x\nbaaar  # y"),
		samePrint("{ { a; }; }"),
		samePrint("{  a;  }"),
		samePrint("(  a   )"),
		samePrint("'foo\nbar'   # x"),
		{"\tfoo", "foo"},
		{"  if foo; then bar; fi", "if   foo; then bar; fi"},
		samePrint("echo '★'  || true"),
		{
			"1234 || { x; y; }",
			"1234 || {\n\tx\n\ty\n}",
		},
		{
			"array=('one'\n\t\t# 'two'\n\t\t'three')",
			"array=('one'\n\t# 'two'\n\t'three')",
		},
	}
	parser := NewParser(KeepComments(true))
	printer := NewPrinter(KeepPadding(true))
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			// ensure that Reset does properly reset colCounter
			printer.w.WriteByte('x')
			printer.w.Reset(nil)
			printTest(t, parser, printer, tc.in, tc.want)
		})
	}
}

func TestPrintKeepPaddingSpaces(t *testing.T) {
	t.Parallel()
	tests := [...]printCase{
		samePrint("array=('one'\n        # 'two'\n        'three')"),
		samePrint("    abc=123"),
		samePrint("foo \\\n  bar \\\n    baz"),
		samePrint("{\n  foo\n    bar\n}"),
		samePrint("# foo\n  # bar"),
	}
	parser := NewParser(KeepComments(true))
	printer := NewPrinter(KeepPadding(true), Indent(2))
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			printTest(t, parser, printer, tc.in, tc.want)
		})
	}
}

func TestPrintMinify(t *testing.T) {
	t.Parallel()
	tests := [...]printCase{
		samePrint("echo foo bar $a $(b)"),
		{
			"#comment",
			"",
		},
		{
			"foo #comment",
			"foo",
		},
		{
			"foo\n\nbar",
			"foo\nbar",
		},
		{
			"foo &",
			"foo&",
		},
		samePrint("foo >bar 2>baz <etc"),
		{
			"{\n\tfoo\n}",
			"{\nfoo\n}",
		},
		{
			"(\n\ta\n)\n(\n\tb\n\tc\n)",
			"(a)\n(b\nc)",
		},
		{
			"$(\n\ta\n)\n$(\n\tb\n\tc\n)",
			"$(a)\n$(b\nc)",
		},
		{
			"f() { x; }",
			"f(){ x;}",
		},
		{
			"((1 + 2))",
			"((1+2))",
		},
		// TODO: see https://github.com/mvdan/sh/issues/1169
		{
			"((\nfoo\n))",
			"((\\\nfoo))",
		},
		{
			"((\na > 0\n&&\na < 10\n))",
			"((\\\na>0&& \\\na<10))",
		},
		{
			"((a > 0 &&\na < 10))",
			"((a>0&& \\\na<10))",
		},
		{
			"echo $a ${b} ${c}-d ${e}f ${g}_h",
			"echo $a $b $c-d ${e}f ${g}_h",
		},
		{
			"echo ${0} ${3} ${10} ${22}",
			"echo $0 $3 ${10} ${22}",
		},
		{
			"case $a in\nx) c ;;\ny | z)\n\td\n\t;;\nesac",
			"case $a in\nx)c;;\ny|z)d\nesac",
		},
		{
			"a && b | c",
			"a&&b|c",
		},
		{
			"a &&\n\tb |\n\tc",
			"a&&b|c",
		},
		{
			"${0/${a}\\\n}",
			"${0/$a/}",
		},
		{
			"#!/bin/sh\necho 1\n#!/bin/sh\necho 2",
			"#!/bin/sh\necho 1\necho 2",
		},
		samePrint("foo >bar 2>baz <etc"),
		samePrint("<<-EOF\n$(a|b)\nEOF"),
		{
			"a=$(\n\tcat <<'EOF'\n  hello\nEOF\n)",
			"a=$(cat <<'EOF'\n  hello\nEOF\n)",
		},
		{
			"(\n\tcat <<EOF\n hello\nEOF\n)",
			"(cat <<EOF\n hello\nEOF\n)",
		},
		samePrint("diff -y <(cat <<EOF\n1\n2\n3\nEOF\n) <(cat <<EOF\n1\n4\n3\nEOF\n)"),
	}
	parser := NewParser(KeepComments(true))
	printer := NewPrinter(Minify(true))
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			printTest(t, parser, printer, tc.in, tc.want)
		})
	}
}

func TestPrintSingleLine(t *testing.T) {
	t.Parallel()
	tests := [...]printCase{
		samePrint("echo foo bar $a $(b)"),
		samePrint("foo #comment"),
		{
			"foo\n\nbar",
			"foo; bar",
		},
		samePrint("foo &"),
		samePrint("foo >bar 2>baz <etc"),
		{
			"{\n\tfoo\n}",
			"{ foo; }",
		},
		{
			"(\n\ta\n)\n(\n\tb\n\tc\n)",
			"(a); (b; c)",
		},
		{
			"$(\n\ta\n)\n$(\n\tb\n\tc\n)",
			"$(a); $(b; c)",
		},
		samePrint("f() { x; }"),
		samePrint("((1 + 2))"),
		samePrint("echo $a ${b} ${c}-d ${e}f ${g}_h"),
		samePrint("echo ${0} ${3} ${10} ${22}"),
		{
			"case $a in\nx)c;;\ny|z)d\nesac",
			"case $a in x) c ;; y | z) d ;; esac",
		},
		samePrint("a && b | c"),
		{
			"a &&\n\tb |\n\tc",
			"a && b | c",
		},
		{
			"if\nfoo\nthen\nbar\nfi",
			"if foo; then bar; fi",
		},
		{
			"a \\\n >b",
			"a >b",
		},
		samePrint("foo >bar 2>baz <etc"),
		samePrint("<<-EOF\n\t$(a | b)\nEOF"),
	}
	parser := NewParser(KeepComments(true))
	printer := NewPrinter(SingleLine(true))
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			printTest(t, parser, printer, tc.in, tc.want)
		})
	}
}

func TestPrintOptionsNotBroken(t *testing.T) {
	t.Parallel()
	singleLineException := regexp.MustCompile(`#|<<|'|"`)
	checkSingleLine := func(t *testing.T, got string) {
		if singleLineException.MatchString(got) {
			return
		}
		got = strings.TrimSuffix(got, "\n") // trailing newline is expected
		if strings.Contains(got, "\n") {
			t.Fatalf("unexpected newline with SingleLine: %q", got)
		}
	}

	for _, opts := range []struct {
		name string
		list []PrinterOption
	}{
		{"Minify", []PrinterOption{Minify(true)}},
		{"SingleLine", []PrinterOption{SingleLine(true)}},
	} {
		printer := NewPrinter(opts.list...)
		for lang := range langResolvedVariants.bits() {
			parser := NewParser(Variant(lang), KeepComments(true))
			for _, tc := range append(fileTests, fileTestsNoPrint...) {
				want, _ := tc.byLangIndex[lang.index()].(*File)
				if want == nil {
					continue
				}
				t.Run(fmt.Sprintf("File%s/%s", opts.name, lang), func(t *testing.T) {
					in := tc.inputs[0]
					t.Logf("input: %s", in)
					prog, err := parser.Parse(strings.NewReader(in), "")
					if err != nil {
						t.Skipf("skipping due to error; TestParsePass should catch this")
					}
					got, err := strPrint(printer, prog)
					if err != nil {
						t.Fatal(err)
					}
					if opts.name == "SingleLine" {
						checkSingleLine(t, got)
					}
					_, err = parser.Parse(strings.NewReader(got), "")
					if err != nil {
						t.Fatalf("program was broken: %v\noriginal:\n%s\nfinal:\n%s", err, in, got)
					}
				})
			}
		}
		parser := NewParser(KeepComments(true))
		for _, tc := range printTests {
			t.Run(fmt.Sprintf("Print%s", opts.name), func(t *testing.T) {
				prog, err := parser.Parse(strings.NewReader(tc.in), "")
				if err != nil {
					t.Skipf("skipping due to error; TestPrintTable should catch this")
				}
				got, err := strPrint(printer, prog)
				if err != nil {
					t.Fatal(err)
				}
				if opts.name == "SingleLine" {
					checkSingleLine(t, got)
				}
				_, err = parser.Parse(strings.NewReader(got), "")
				if err != nil {
					t.Fatalf("program was broken: %v\noriginal:\n%s\nfinal:\n%s", err, tc.in, got)
				}
			})
		}
	}
}

func printTest(t *testing.T, parser *Parser, printer *Printer, in, want string) {
	t.Helper()
	t.Logf("input: %s", in)
	prog, err := parser.Parse(strings.NewReader(in), "")
	if err != nil {
		t.Fatalf("parsing got an error: %s:\n%s", err, in)
	}
	origWant := want
	want += "\n"
	got, err := strPrint(printer, prog)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Print mismatch:\nwant:\n%q\ngot:\n%q", want, got)
	}

	// With the original "want" output string,
	// make sure that it's idempotent when formatted again.
	// Note that we don't want the added newline,
	// as that can change the meaning of trailing backslashes.
	progAgain, err := parser.Parse(strings.NewReader(origWant), "")
	if err != nil {
		t.Fatalf("Result is not valid shell:\n%s", want)
	}
	gotAgain, err := strPrint(printer, progAgain)
	if err != nil {
		t.Fatal(err)
	}
	if gotAgain != want {
		t.Fatalf("Re-print mismatch:\nwant:\n%q\ngot:\n%q", want, gotAgain)
	}
}

func TestPrintNodeTypes(t *testing.T) {
	t.Parallel()

	multiline, err := NewParser().Parse(strings.NewReader(`
		echo foo
	`), "")
	if err != nil {
		t.Fatal(err)
	}

	tests := [...]struct {
		in      Node
		want    string
		wantErr bool
	}{
		{
			in:   &File{Stmts: litStmts("foo")},
			want: "foo\n",
		},
		{
			in:   &File{Stmts: litStmts("foo", "bar")},
			want: "foo\nbar\n",
		},
		{
			in:   litStmt("foo", "bar"),
			want: "foo bar",
		},
		{
			in:   litCall("foo", "bar"),
			want: "foo bar",
		},
		{
			in:   litWord("foo"),
			want: "foo",
		},
		{
			in:   lit("foo"),
			want: "foo",
		},
		{
			in:   sglQuoted("foo"),
			want: "'foo'",
		},
		{
			in:      &Comment{},
			wantErr: true,
		},
		{
			in:   multiline.Stmts[0],
			want: "echo foo",
		},
		{
			in:   multiline.Stmts[0].Cmd,
			want: "echo foo",
		},
		{
			in:   multiline.Stmts[0].Cmd.(*CallExpr).Args[0],
			want: "echo",
		},
		{
			in:   multiline.Stmts[0].Cmd.(*CallExpr).Args[0].Parts[0],
			want: "echo",
		},
	}
	printer := NewPrinter()
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			got, err := strPrint(printer, tc.in)
			if err == nil && tc.wantErr {
				t.Fatalf("wanted an error but found none")
			} else if err != nil && !tc.wantErr {
				t.Fatalf("didn't want an error but got %v", err)
			}
			if got != tc.want {
				t.Fatalf("Print mismatch:\nwant:\n%s\ngot:\n%s",
					tc.want, got)
			}
		})
	}
}

func TestPrintManyStmts(t *testing.T) {
	t.Parallel()
	tests := [...]struct {
		in, want string
	}{
		{"foo; bar", "foo\nbar\n"},
		{"foo\nbar", "foo\nbar\n"},
		{"\n\nfoo\nbar\n\n", "foo\nbar\n"},
		{"foo\nbar <<EOF\nbody\nEOF\n", "foo\nbar <<EOF\nbody\nEOF\n"},
		{"foo\nbar # inline", "foo\nbar # inline\n"},
		{"# comment before\nfoo bar", "# comment before\nfoo bar\n"},
	}
	parser := NewParser(KeepComments(true))
	printer := NewPrinter()
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			f, err := parser.Parse(strings.NewReader(tc.in), "")
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			for _, stmt := range f.Stmts {
				printer.Print(&buf, stmt)
				buf.WriteByte('\n')
			}
			got := buf.String()
			if got != tc.want {
				t.Fatalf("Print mismatch:\nwant:\n%s\ngot:\n%s",
					tc.want, got)
			}
		})
	}
}

func TestKeepPaddingRepeated(t *testing.T) {
	t.Parallel()
	parser := NewParser()
	printer := NewPrinter()

	// Enable the KeepPadding option twice. This used to crash, since the
	// option made an invalid type assertion the second time.
	KeepPadding(true)(printer)
	KeepPadding(true)(printer)

	// Ensure the option is enabled.
	printTest(t, parser, printer, "foo  bar", "foo  bar")

	// Disable the option, and ensure it's disabled.
	KeepPadding(false)(printer)
	printTest(t, parser, printer, "foo  bar", "foo bar")
}
