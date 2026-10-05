package termrender

import (
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// perRune is set while the terminal drawn on counts columns per rune rather
// than per grapheme cluster; layout measured the other way comes out a
// different width on screen than it was padded to.
var perRune atomic.Bool

// Cells is how the terminal drawn on counts columns.
func Cells() ansi.Method {
	if perRune.Load() {
		return ansi.WcWidth
	}
	return ansi.GraphemeWidth
}

// SetCells records how the terminal drawn on counts columns.
func SetCells(m ansi.Method) { perRune.Store(m == ansi.WcWidth) }

// Truncate cuts s to w columns as Cells counts them, tail included, keeping
// every escape sequence. The library's cuts split text differently from how
// its StringWidth counts it (per rune it keeps whole ZWJ clusters, per
// grapheme it splits a keycap from its base), which yields rows wider than w.
func Truncate(s string, w int, tail string) string {
	if Cells().StringWidth(s) <= w {
		return s
	}
	return cut(s, 0, max(w-Cells().StringWidth(tail), 0), tail)
}

// Cut is the part of s from column left to column right as Cells counts them.
func Cut(s string, left, right int) string {
	return cut(s, left, right, "")
}

// Hardwrap breaks s into rows of at most w columns as Cells counts them,
// between the units Cells counts; escape sequences stay where they were.
func Hardwrap(s string, w int) string {
	var b strings.Builder
	b.Grow(len(s) + len(s)/max(w, 1))
	col := 0
	units(s, func(u string, esc bool) {
		if esc {
			b.WriteString(u)
			return
		}
		if u == "\n" {
			b.WriteString(u)
			col = 0
			return
		}
		uw := Cells().StringWidth(u)
		if col+uw > w && col > 0 {
			b.WriteByte('\n')
			col = 0
		}
		b.WriteString(u)
		col += uw
	})
	return b.String()
}

func cut(s string, left, right int, tail string) string {
	var b strings.Builder
	b.Grow(len(s))
	col, tailed := 0, false
	units(s, func(u string, esc bool) {
		if esc {
			b.WriteString(u)
			return
		}
		uw := Cells().StringWidth(u)
		if col+uw > right || (uw == 0 && col > right) {
			if !tailed {
				b.WriteString(tail)
				tailed = true
			}
			col = right + 1
			return
		}
		if col >= left {
			b.WriteString(u)
		}
		col += uw
	})
	return b.String()
}

// units calls fn with each escape sequence and each unit Cells counts: a
// grapheme cluster, or per rune a single rune. Text split around escapes is
// joined first, so a grapheme cluster is never split by a style change.
func units(s string, fn func(u string, esc bool)) {
	var text strings.Builder
	flush := func() {
		t := text.String()
		text.Reset()
		if perRune.Load() {
			for _, r := range t {
				fn(string(r), false)
			}
			return
		}
		g := uniseg.NewGraphemes(t)
		for g.Next() {
			fn(g.Str(), false)
		}
	}
	var state byte
	for len(s) > 0 {
		seq, _, n, next := ansi.DecodeSequence(s, state, nil)
		state = next
		s = s[n:]
		if r, _ := utf8.DecodeRuneInString(seq); r != '\n' && (r == ansi.ESC || unicode.IsControl(r)) {
			flush()
			fn(seq, true)
			continue
		}
		text.WriteString(seq)
	}
	flush()
}
