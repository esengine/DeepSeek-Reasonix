// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lcs

import (
	"log"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

type Btest struct {
	a, b string
	lcs  []string
}

var Btests = []Btest{
	{"aaabab", "abaab", []string{"abab", "aaab"}},
	{"aabbba", "baaba", []string{"aaba"}},
	{"cabbx", "cbabx", []string{"cabx", "cbbx"}},
	{"c", "cb", []string{"c"}},
	{"aaba", "bbb", []string{"b"}},
	{"bbaabb", "b", []string{"b"}},
	{"baaabb", "bbaba", []string{"bbb", "baa", "bab"}},
	{"baaabb", "abbab", []string{"abb", "bab", "aab"}},
	{"baaba", "aaabba", []string{"aaba"}},
	{"ca", "cba", []string{"ca"}},
	{"ccbcbc", "abba", []string{"bb"}},
	{"ccbcbc", "aabba", []string{"bb"}},
	{"ccb", "cba", []string{"cb"}},
	{"caef", "axe", []string{"ae"}},
	{"bbaabb", "baabb", []string{"baabb"}},
	// Example from Myers:
	{"abcabba", "cbabac", []string{"caba", "baba", "cbba"}},
	{"3456aaa", "aaa", []string{"aaa"}},
	{"aaa", "aaa123", []string{"aaa"}},
	{"aabaa", "aacaa", []string{"aaaa"}},
	{"1a", "a", []string{"a"}},
	{"abab", "bb", []string{"bb"}},
	{"123", "ab", []string{""}},
	{"a", "b", []string{""}},
	{"abc", "123", []string{""}},
	{"aa", "aa", []string{"aa"}},
	{"abcde", "12345", []string{""}},
	{"aaa3456", "aaa", []string{"aaa"}},
	{"abcde", "12345a", []string{"a"}},
	{"ab", "123", []string{""}},
	{"1a2", "a", []string{"a"}},
	// for two-sided
	{"babaab", "cccaba", []string{"aba"}},
	{"aabbab", "cbcabc", []string{"bab"}},
	{"abaabb", "bcacab", []string{"baab"}},
	{"abaabb", "abaaaa", []string{"abaa"}},
	{"bababb", "baaabb", []string{"baabb"}},
	{"abbbaa", "cabacc", []string{"aba"}},
	{"aabbaa", "aacaba", []string{"aaaa", "aaba"}},
}

type stringSeqs struct{ a, b string }

func (s stringSeqs) lengths() (int, int) { return len(s.a), len(s.b) }
func (s stringSeqs) commonPrefixLen(ai, aj, bi, bj int) int {
	return commonPrefixLenString(s.a[ai:aj], s.b[bi:bj])
}
func (s stringSeqs) commonSuffixLen(ai, aj, bi, bj int) int {
	return commonSuffixLenString(s.a[ai:aj], s.b[bi:bj])
}

func commonPrefixLenString(a, b string) int {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}
func commonSuffixLenString(a, b string) int {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[len(a)-1-i] == b[len(b)-1-i] {
		i++
	}
	return i
}

func init() {
	log.SetFlags(log.Lshortfile)
}

func check(t *testing.T, str string, lcs lcs, want []string) {
	t.Helper()
	if !lcs.valid() {
		t.Errorf("bad lcs %v", lcs)
	}
	var got strings.Builder
	for _, dd := range lcs {
		got.WriteString(str[dd.X : dd.X+dd.Len])
	}
	ans := got.String()
	if slices.Contains(want, ans) {
		return
	}
	t.Fatalf("str=%q lcs=%v want=%q got=%q", str, lcs, want, ans)
}

func checkDiffs(t *testing.T, before string, diffs []Diff, after string) {
	t.Helper()
	var ans strings.Builder
	sofar := 0 // index of position in before
	for _, d := range diffs {
		if sofar < d.Start {
			ans.WriteString(before[sofar:d.Start])
		}
		ans.WriteString(after[d.ReplStart:d.ReplEnd])
		sofar = d.End
	}
	ans.WriteString(before[sofar:])
	if ans.String() != after {
		t.Fatalf("diff %v took %q to %q, not to %q", diffs, before, ans.String(), after)
	}
}

func lcslen(l lcs) int {
	ans := 0
	for _, d := range l {
		ans += int(d.Len)
	}
	return ans
}

// return a random string of length n made of characters from s
func randstr(rng *rand.Rand, s string, n int) string {
	src := []rune(s)
	x := make([]rune, n)
	for i := range n {
		x[i] = src[rng.Int64N(int64(len(src)))]
	}
	return string(x)
}

func TestLcsFix(t *testing.T) {
	tests := []struct{ before, after lcs }{
		{lcs{diag{0, 0, 3}, diag{2, 2, 5}, diag{3, 4, 5}, diag{8, 9, 4}}, lcs{diag{0, 0, 2}, diag{2, 2, 1}, diag{3, 4, 5}, diag{8, 9, 4}}},
		{lcs{diag{1, 1, 6}, diag{6, 12, 3}}, lcs{diag{1, 1, 5}, diag{6, 12, 3}}},
		{lcs{diag{0, 0, 4}, diag{3, 5, 4}}, lcs{diag{0, 0, 3}, diag{3, 5, 4}}},
		{lcs{diag{0, 20, 1}, diag{0, 0, 3}, diag{1, 20, 4}}, lcs{diag{0, 0, 3}, diag{3, 22, 2}}},
		{lcs{diag{0, 0, 4}, diag{1, 1, 2}}, lcs{diag{0, 0, 4}}},
		{lcs{diag{0, 0, 4}}, lcs{diag{0, 0, 4}}},
		{lcs{}, lcs{}},
		{lcs{diag{0, 0, 4}, diag{1, 1, 6}, diag{3, 3, 2}}, lcs{diag{0, 0, 1}, diag{1, 1, 6}}},
	}
	for n, x := range tests {
		got := x.before.fix()
		if len(got) != len(x.after) {
			t.Errorf("got %v, expected %v, for %v", got, x.after, x.before)
		}
		olen := lcslen(x.after)
		glen := lcslen(got)
		if olen != glen {
			t.Errorf("%d: lens(%d,%d) differ, %v, %v, %v", n, glen, olen, got, x.after, x.before)
		}
	}
}
