package tui

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
	"golang.org/x/text/width"

	"reasonix/internal/frontend/termrender"
)

// glyphFit swaps each rune the console cannot draw at the width the layout
// counted, wider than counted or with no glyph in its font, for a stand-in it
// can, so every row draws whole at the width it was laid out to. The
// console's own measurement and font are the only judges.
type glyphFit struct {
	mu       sync.Mutex      // the inline banner is printed from a command goroutine
	cells    func(rune) int  // columns the console advances for r; 0 when unmeasured
	bestFit  func(rune) rune // single-byte best-fit stand-in for r, 0 when none
	hasGlyph func(rune) bool // whether the console's own font draws r; nil when the terminal picks fonts
	chosen   map[rune]string // per-rune verdict; r maps to itself when it already fits
}

func newGlyphFit(cells func(rune) int, bestFit func(rune) rune, hasGlyph func(rune) bool) *glyphFit {
	return &glyphFit{cells: cells, bestFit: bestFit, hasGlyph: hasGlyph, chosen: map[rune]string{}}
}

// apply rewrites the printable text of a styled frame; escape sequences,
// including OSC payloads such as hyperlink targets, pass through untouched.
// A terminal drawing grapheme clusters picks its own fonts, so only the
// clusters no Windows font draws are replaced there.
func (g *glyphFit) apply(s string) string {
	if g == nil {
		return s
	}
	if termrender.Cells() == ansi.GraphemeWidth {
		return unfontedClusters(s)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.needed(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	var state byte
	for len(s) > 0 {
		seq, _, n, next := ansi.DecodeSequence(s, state, nil)
		state = next
		s = s[n:]
		if isEscape(seq) {
			b.WriteString(seq)
			continue
		}
		for _, r := range seq {
			b.WriteString(g.fit(r))
		}
	}
	return b.String()
}

func (g *glyphFit) needed(s string) bool {
	for _, r := range s {
		if r >= utf8.RuneSelf && g.fit(r) != string(r) {
			return true
		}
	}
	return false
}

func (g *glyphFit) fit(r rune) string {
	if r < utf8.RuneSelf {
		return string(r)
	}
	if c, ok := g.chosen[r]; ok {
		return c
	}
	c := g.choose(r)
	g.chosen[r] = c
	return c
}

// choose keeps r when the console draws it as counted, else takes the first
// stand-in it draws as counted: a drawn symbol of the same meaning, Unicode's
// halfwidth variant, a flag letter, the OS best-fit mapping; blank cells last.
// A stand-in narrower than r is padded, so the per-rune count never moves.
func (g *glyphFit) choose(r rune) string {
	counted := ansi.WcWidth.StringWidth(string(r))
	if g.draws(r) {
		return string(r)
	}
	cands := append([]string(nil), glyphStandIns[r]...)
	if n := width.LookupRune(r).Narrow(); n > 0 {
		cands = append(cands, string(n))
	}
	if r >= regionalFirst && r <= regionalLast {
		cands = append(cands, string('A'+r-regionalFirst))
	}
	if c := g.bestFit(r); c > 0 {
		cands = append(cands, string(c))
	}
	for _, c := range cands {
		w := ansi.WcWidth.StringWidth(c)
		if c != string(r) && w <= counted && g.drawsAll(c) {
			return c + strings.Repeat(" ", counted-w)
		}
	}
	return strings.Repeat(" ", counted)
}

func (g *glyphFit) draws(r rune) bool {
	if r < utf8.RuneSelf {
		return true
	}
	counted := ansi.WcWidth.StringWidth(string(r))
	if c := g.cells(r); c != 0 && c != counted {
		return false
	}
	return g.hasGlyph == nil || g.hasGlyph(r)
}

func (g *glyphFit) drawsAll(s string) bool {
	for _, r := range s {
		if !g.draws(r) {
			return false
		}
	}
	return true
}

func isEscape(seq string) bool {
	r, _ := utf8.DecodeRuneInString(seq)
	return r == ansi.ESC || unicode.IsControl(r)
}

// glyphStandIns are drawn symbols of the same meaning, tried in order, for the
// symbols the interface draws and the emoji answers carry most. Each is tried
// only against the console's own measurement and font.
var glyphStandIns = map[rune][]string{
	'◆': {"*"}, '◇': {"*"}, '●': {"•", "*"}, '○': {"o"}, '•': {"*"},
	'■': {"#"}, '□': {"#"}, '★': {"*"}, '☆': {"*"},
	'✓': {"√", "v"}, '✔': {"√", "v"}, '✅': {"√", "OK"}, '☑': {"x"}, '☐': {"o"},
	'✗': {"×", "x"}, '✘': {"×", "x"}, '❌': {"×", "X"}, '⚠': {"!"},
	'⎿': {"└", "L"}, '↳': {"└", ">"}, '▸': {">"}, '▶': {">"}, '▾': {"v"},
	'❯': {">"}, '›': {">"}, '→': {">"}, '←': {"<"}, '↑': {"^"}, '↓': {"v"}, '⟲': {"<"}, '⧗': {"~"}, '▎': {"│", "|"},
	'│': {"|"}, '─': {"-"}, '█': {"#"}, '╭': {"┌", "+"}, '╮': {"┐", "+"},
	'╰': {"└", "+"}, '╯': {"┘", "+"}, '┼': {"+"}, '❤': {"♥", "*"},
	'×': {"x"}, '÷': {"/"}, '±': {"+"}, '°': {"o"}, '≈': {"~"}, '≤': {"<"}, '≥': {">"},
	'≠': {"!"}, '∞': {"~"}, '√': {"v"}, '※': {"*"}, '…': {"."}, '·': {"."},
	'①': {"1"}, '②': {"2"}, '③': {"3"}, '④': {"4"}, '⑤': {"5"}, '⑥': {"6"}, '⑦': {"7"}, '⑧': {"8"}, '⑨': {"9"},
	'α': {"a"}, 'β': {"b"}, 'γ': {"g"}, 'δ': {"d"}, 'ε': {"e"}, 'θ': {"q"}, 'λ': {"l"}, 'μ': {"u"},
	'π': {"p"}, 'σ': {"s"}, 'τ': {"t"}, 'φ': {"f"}, 'ω': {"w"}, 'Δ': {"D"}, 'Σ': {"S"}, 'Ω': {"W"},
	'⣾': {"|"}, '⣽': {"/"}, '⣻': {"-"}, '⢿': {"\\"}, '⡿': {"|"}, '⣟': {"/"}, '⣯': {"-"}, '⣷': {"\\"},
}

// unfontedClusters replaces, keeping each cluster's grapheme count, the
// clusters Windows ships no glyph for: a flag becomes its letters, a keycap its
// base character, a tag sequence its base flag.
func unfontedClusters(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool {
		return r == keycapMark || r >= regionalFirst && r <= regionalLast || r >= tagFirst && r <= tagLast
	}) {
		return s
	}
	var b, text strings.Builder
	b.Grow(len(s))
	flush := func() {
		g := uniseg.NewGraphemes(text.String())
		for g.Next() {
			b.WriteString(unfontedCluster(g.Str()))
		}
		text.Reset()
	}
	var state byte
	for len(s) > 0 {
		seq, _, n, next := ansi.DecodeSequence(s, state, nil)
		state = next
		s = s[n:]
		if isEscape(seq) {
			flush()
			b.WriteString(seq)
		} else {
			text.WriteString(seq)
		}
	}
	flush()
	return b.String()
}

func unfontedCluster(c string) string {
	var out string
	switch {
	case strings.ContainsFunc(c, func(r rune) bool { return r >= regionalFirst && r <= regionalLast }):
		out = strings.Map(func(r rune) rune {
			if r >= regionalFirst && r <= regionalLast {
				return 'A' + r - regionalFirst
			}
			return -1
		}, c)
	case strings.ContainsFunc(c, func(r rune) bool { return r == keycapMark || r >= tagFirst && r <= tagLast }):
		out = strings.Map(func(r rune) rune {
			if r == keycapMark || r == emojiSelector || r >= tagFirst && r <= tagLast {
				return -1
			}
			return r
		}, c)
	default:
		return c
	}
	return out + strings.Repeat(" ", max(ansi.GraphemeWidth.StringWidth(c)-ansi.GraphemeWidth.StringWidth(out), 0))
}

// splitClusters rewrites the printable text of s for a terminal that counts
// per rune, where a multi-rune emoji draws at whatever width the font folds
// it to and an emoji selector draws a two-column glyph into one. Joiners and
// selectors go, a skin tone becomes the two blank cells it was counted, a
// regional indicator its letter: the per-rune count is unchanged throughout.
func splitClusters(s string) string {
	if !strings.ContainsFunc(s, foldsIntoCluster) && !strings.ContainsFunc(s, narrowPictograph) {
		return s
	}
	var ps []clusterPiece
	var state byte
	for len(s) > 0 {
		seq, _, n, next := ansi.DecodeSequence(s, state, nil)
		state = next
		s = s[n:]
		if isEscape(seq) {
			ps = append(ps, clusterPiece{esc: seq})
			continue
		}
		for _, r := range seq {
			switch {
			case r == zeroWidthJoiner, r == keycapMark, r == emojiSelector, r >= tagFirst && r <= tagLast:
			case r >= skinToneFirst && r <= skinToneLast:
				ps = append(ps, clusterPiece{r: ' '}, clusterPiece{r: ' '})
			case r >= regionalFirst && r <= regionalLast:
				ps = append(ps, clusterPiece{r: 'A' + r - regionalFirst})
			default:
				ps = append(ps, clusterPiece{r: r})
			}
		}
	}
	var b strings.Builder
	for i, p := range ps {
		switch {
		case p.esc != "":
			b.WriteString(p.esc)
		case narrowPictograph(p.r) && !blankAfter(ps[i+1:]):
			b.WriteByte(' ')
		default:
			b.WriteRune(p.r)
		}
	}
	return b.String()
}

// narrowPictograph is an emoji outside the Basic Multilingual Plane counted
// one column: no text font draws it, so its two-column colour glyph covers
// half of whatever is drawn next.
func narrowPictograph(r rune) bool {
	return r >= 0x1f000 && r <= 0x1ffff && ansi.WcWidth.StringWidth(string(r)) == 1
}

func blankAfter(rest []clusterPiece) bool {
	for _, q := range rest {
		if q.esc == "" {
			return q.r == ' '
		}
	}
	return true
}

// clusterPiece is an escape sequence or one printable rune of a frame.
type clusterPiece struct {
	esc string
	r   rune
}

const (
	zeroWidthJoiner = '\u200d'
	keycapMark      = '\u20e3'
	emojiSelector   = '\ufe0f'
	skinToneFirst   = '\U0001f3fb'
	skinToneLast    = '\U0001f3ff'
	regionalFirst   = '\U0001f1e6'
	regionalLast    = '\U0001f1ff'
	tagFirst        = '\U000e0020'
	tagLast         = '\U000e007f'
)

func foldsIntoCluster(r rune) bool {
	return r == zeroWidthJoiner || r == keycapMark || r == emojiSelector ||
		r >= skinToneFirst && r <= skinToneLast ||
		r >= regionalFirst && r <= regionalLast ||
		r >= tagFirst && r <= tagLast
}
