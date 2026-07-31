// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"bytes"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

func lit(s string) *syntax.Lit                { return &syntax.Lit{Value: s} }
func word(ps ...syntax.WordPart) *syntax.Word { return &syntax.Word{Parts: ps} }
func litWord(s string) *syntax.Word           { return word(lit(s)) }
func litWords(strs ...string) []*syntax.Word {
	l := make([]*syntax.Word, 0, len(strs))
	for _, s := range strs {
		l = append(l, litWord(s))
	}
	return l
}

var braceTests = []struct {
	in   *syntax.Word
	want []*syntax.Word
}{
	{
		litWord("a{b"),
		litWords("a{b"),
	},
	{
		litWord("a}b"),
		litWords("a}b"),
	},
	{
		litWord("{a,b{c,d}"),
		litWords("{a,bc", "{a,bd"),
	},
	{
		litWord("{a{b"),
		litWords("{a{b"),
	},
	{
		litWord("a{}"),
		litWords("a{}"),
	},
	{
		litWord("a{b}"),
		litWords("a{b}"),
	},
	{
		litWord("a{b,c}"),
		litWords("ab", "ac"),
	},
	{
		litWord("a{à,世界}"),
		litWords("aà", "a世界"),
	},
	{
		litWord("a{b,c}d{e,f}g"),
		litWords("abdeg", "abdfg", "acdeg", "acdfg"),
	},
	{
		litWord("a{b{x,y},c}d"),
		litWords("abxd", "abyd", "acd"),
	},
	{
		litWord("a{1,2,3,4,5}"),
		litWords("a1", "a2", "a3", "a4", "a5"),
	},
	{
		litWord("a{1.."),
		litWords("a{1.."),
	},
	{
		litWord("a{1..4"),
		litWords("a{1..4"),
	},
	{
		litWord("a{1.4}"),
		litWords("a{1.4}"),
	},
	{
		litWord("{a,b}{1..4"),
		litWords("a{1..4", "b{1..4"),
	},
	{
		litWord("a{1..4}"),
		litWords("a1", "a2", "a3", "a4"),
	},
	{
		litWord("a{1..2}b{4..5}c"),
		litWords("a1b4c", "a1b5c", "a2b4c", "a2b5c"),
	},
	{
		litWord("a{1..f}"),
		litWords("a{1..f}"),
	},
	{
		litWord("a{c..f}"),
		litWords("ac", "ad", "ae", "af"),
	},
	{
		litWord("a{H..K}"),
		litWords("aH", "aI", "aJ", "aK"),
	},
	{
		litWord("a{-..f}"),
		litWords("a{-..f}"),
	},
	{
		litWord("a{3..-}"),
		litWords("a{3..-}"),
	},
	{
		litWord("a{1..10..3}"),
		litWords("a1", "a4", "a7", "a10"),
	},
	{
		litWord("a{1..4..0}"),
		litWords("a1", "a2", "a3", "a4"),
	},
	{
		litWord("a{4..1}"),
		litWords("a4", "a3", "a2", "a1"),
	},
	{
		litWord("a{4..1..-2}"),
		litWords("a4", "a2"),
	},
	{
		litWord("a{4..1..1}"),
		litWords("a4", "a3", "a2", "a1"),
	},
	{
		litWord("{1..005}"),
		litWords("001", "002", "003", "004", "005"),
	},
	{
		litWord("{0001..05..2}"),
		litWords("0001", "0003", "0005"),
	},
	{
		litWord("{0..1}"),
		litWords("0", "1"),
	},
	{
		litWord("a{d..k..3}"),
		litWords("ad", "ag", "aj"),
	},
	{
		litWord("a{d..k..n}"),
		litWords("a{d..k..n}"),
	},
	{
		litWord("a{k..d..-2}"),
		litWords("ak", "ai", "ag", "ae"),
	},
	{
		litWord("{1..1}"),
		litWords("1"),
	},
}

func TestBraces(t *testing.T) {
	t.Parallel()
	for _, tc := range braceTests {
		t.Run("", func(t *testing.T) {
			inStr := printWords(tc.in)
			wantStr := printWords(tc.want...)
			wantBraceExpParts(t, tc.in, false)

			inBraces := *tc.in
			syntax.SplitBraces(&inBraces)
			wantBraceExpParts(t, &inBraces, inStr != wantStr)

			got := Braces(&inBraces)
			gotStr := printWords(got...)
			if gotStr != wantStr {
				t.Fatalf("mismatch in %q\nwant:\n%s\ngot: %s",
					inStr, wantStr, gotStr)
			}
		})
	}
}

func wantBraceExpParts(t *testing.T, word *syntax.Word, want bool) {
	t.Helper()
	anyBrace := false
	for _, part := range word.Parts {
		if _, anyBrace = part.(*syntax.BraceExp); anyBrace {
			break
		}
	}
	if anyBrace && !want {
		t.Fatalf("didn't want any BraceExp node, but found one")
	} else if !anyBrace && want {
		t.Fatalf("wanted a BraceExp node, but found none")
	}
}

func printWords(words ...*syntax.Word) string {
	p := syntax.NewPrinter()
	var buf bytes.Buffer
	call := &syntax.CallExpr{Args: words}
	p.Print(&buf, call)
	return buf.String()
}
