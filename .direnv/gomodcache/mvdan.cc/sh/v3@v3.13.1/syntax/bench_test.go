// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"io"
	"strings"
	"testing"
)

func BenchmarkParse(b *testing.B) {
	b.ReportAllocs()
	src := "" +
		strings.Repeat("\n\n\t\t        \n", 10) +
		"# " + strings.Repeat("foo bar ", 10) + "\n" +
		strings.Repeat("longlit_", 10) + "\n" +
		"'" + strings.Repeat("foo bar ", 10) + "'\n" +
		`"` + strings.Repeat("foo bar ", 10) + `"` + "\n" +
		strings.Repeat("aa bb cc dd; ", 6) +
		"a() { (b); { c; }; }; $(d; `e`)\n" +
		"foo=bar; a=b; c=d$foo${bar}e $simple ${complex:-default}\n" +
		"if a; then while b; do for c in d e; do f; done; done; fi\n" +
		"a | b && c || d | e && g || f\n" +
		"foo >a <b <<<c 2>&1 <<EOF\n" +
		strings.Repeat("somewhat long heredoc line\n", 10) +
		"EOF" +
		""
	p := NewParser(KeepComments(true))
	in := strings.NewReader(src)
	for b.Loop() {
		if _, err := p.Parse(in, ""); err != nil {
			b.Fatal(err)
		}
		in.Reset(src)
	}
}

func BenchmarkPrint(b *testing.B) {
	b.ReportAllocs()
	prog := parsePath(b, canonicalPath)
	printer := NewPrinter()
	for b.Loop() {
		if err := printer.Print(io.Discard, prog); err != nil {
			b.Fatal(err)
		}
	}
}
