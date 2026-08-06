package cli

// Equivalence gate for per-block wrapped-line caching.
//
// wrapTranscript(join(blocks)) can be replaced by join(wrapTranscript(b) for
// b in blocks) — the premise of the per-block wrap cache — under two
// conditions:
//
//  1. Every block ends with its SGR state closed (a trailing reset or no
//     escapes at all). If a style leaks across the block boundary, lipgloss's
//     wrap writer reopens it on the next line of the JOINED string but not
//     across separate per-block renders, and the outputs diverge.
//  2. Lines wider than the target width are handled: x/ansi's wrap can leave
//     a word glued into a line wider than the limit (a hyphen filling the
//     line exactly, or a wide rune at width 1), and lipgloss then pads every
//     line of the WHOLE string to that widest line. The cache's join
//     (wrapTranscriptBlocks) therefore re-pads each block's lines to the
//     global widest line, which the naive join in perBlockWrap does not.
//
// TestWrapTranscriptPerBlockEquivalenceBalanced is the hard gate against the
// PRODUCTION path (wrapTranscriptBlocks): it must pass for all-balanced
// blocks at every width, or the cache must not ship. The renderer variant
// feeds the real markdown/user/reasoning/tool-card/connector/banner
// renderers and additionally checks that each block ends reset-closed.
// TestNaivePerBlockWrapDivergesWithoutRepad documents why condition 2 needs
// the re-pad, and the unbalanced probe documents what the gate catches.

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

// sgrOpeners are the escape forms the renderers emit (bold, italic, 256-color,
// true-color, underline). Closing is always "\x1b[0m".
var sgrOpeners = []string{
	"\x1b[1m",
	"\x1b[3m",
	"\x1b[4m",
	"\x1b[38;5;173m",
	"\x1b[38;2;10;150;200m",
}

var plainWords = []string{
	"lorem", "ipsum", "dolor", "sit", "amet", "consectetur", "adipiscing",
	"elit", "sed", "do", "eiusmod", "tempor", "incididunt", "ut", "labore",
	"日本語", "テキスト", "文章", "한국어", "문장", "español", "café", "naïve",
	"🚀", "✨", "λ", "≈", "x²", "a_b", "foo-bar", "word", "with-hyphens",
	"supercalifragilisticexpialidocious",
}

func randText(rng *rand.Rand) string {
	switch rng.Intn(4) {
	case 0:
		// a single long unbreakable run (forces hard breaks)
		n := 1 + rng.Intn(40)
		c := byte('a' + rng.Intn(26))
		return strings.Repeat(string(c), n)
	case 1:
		// digits / punctuation soup
		n := 1 + rng.Intn(12)
		var b strings.Builder
		for i := 0; i < n; i++ {
			b.WriteByte(byte('0' + rng.Intn(10)))
		}
		return b.String()
	default:
		return plainWords[rng.Intn(len(plainWords))]
	}
}

// randomBalancedBlock builds a block whose every SGR sequence is closed by a
// trailing "\x1b[0m", possibly spanning several lines, with leading/trailing
// spaces, wide runes, and occasionally a trailing newline.
func randomBalancedBlock(rng *rand.Rand) string {
	var b strings.Builder
	lines := 1 + rng.Intn(3)
	for l := 0; l < lines; l++ {
		if l > 0 {
			b.WriteByte('\n')
		}
		if rng.Intn(8) == 0 {
			b.WriteString("  ")
		}
		segments := 1 + rng.Intn(4)
		for s := 0; s < segments; s++ {
			switch rng.Intn(4) {
			case 0: // styled span, closed
				open := sgrOpeners[rng.Intn(len(sgrOpeners))]
				b.WriteString(open)
				b.WriteString(randText(rng))
				if rng.Intn(4) == 0 { // nested span
					b.WriteString(sgrOpeners[rng.Intn(len(sgrOpeners))])
					b.WriteString(randText(rng))
					b.WriteString("\x1b[0m")
				}
				b.WriteString("\x1b[0m")
			case 1: // tab
				b.WriteByte('\t')
				b.WriteString(randText(rng))
			default:
				b.WriteString(randText(rng))
			}
			if rng.Intn(3) == 0 {
				b.WriteByte(' ')
			}
		}
		if rng.Intn(6) == 0 {
			b.WriteString("  ")
		}
	}
	if rng.Intn(4) == 0 {
		b.WriteByte('\n') // trailing newline: real renderers trim these, but the gate must hold anyway
	}
	return b.String()
}

// randomUnbalancedBlock is like randomBalancedBlock but occasionally leaves an
// SGR sequence open at the end — the case the gate must reject.
func randomUnbalancedBlock(rng *rand.Rand) string {
	if rng.Intn(3) == 0 {
		return randomBalancedBlock(rng) + sgrOpeners[rng.Intn(len(sgrOpeners))]
	}
	b := randomBalancedBlock(rng)
	// strip the trailing reset of the last styled span, if any
	idx := strings.LastIndex(b, "\x1b[0m")
	if idx >= 0 && idx+len("\x1b[0m") == len(b) {
		return b[:idx]
	}
	return b
}

// perBlockWrap renders each block alone at width and joins with "\n" — the
// NAIVE shape (no widest-line re-pad). It is byte-identical to the
// whole-transcript wrap only when no wrapped line exceeds the width.
func perBlockWrap(blocks []string, width int) string {
	var b strings.Builder
	for i, block := range blocks {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(wrapTranscript(block, width))
	}
	return b.String()
}

// cachedWrap runs the production per-block wrap cache (wrapTranscriptBlocks)
// on a fresh model, so the gate covers the exact code path the TUI renders
// with.
func cachedWrap(blocks []string, width int) string {
	var m chatTUI
	return m.wrapTranscriptBlocks(blocks, width)
}

// lastSGREndsWithReset reports whether the block's final SGR sequence is a
// reset (or the block contains no SGR at all) — no style is left open across
// the block boundary.
func lastSGREndsWithReset(s string) bool {
	seqStart, seqEnd := -1, -1
	for i := 0; i+1 < len(s); {
		if s[i] != 0x1b || s[i+1] != '[' {
			i++
			continue
		}
		start := i
		j := i + 2
		for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
			j++
		}
		if j < len(s) && s[j] == 'm' {
			seqStart, seqEnd = start, j
		}
		i = j + 1
	}
	if seqStart < 0 {
		return true
	}
	seq := s[seqStart : seqEnd+1]
	return seq == "\x1b[0m" || seq == "\x1b[m"
}

// TestWrapTranscriptPerBlockEquivalenceBalanced is the gate: for every
// all-balanced random block set at widths 1..60, the production per-block
// wrap cache must be byte-identical to wrapping the joined transcript. It
// fails on ANY mismatch and shows the offending block. Half the iterations
// also mutate one block in place between cache rounds, exercising the
// stale-entry detection (content verification).
func TestWrapTranscriptPerBlockEquivalenceBalanced(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for iter := 0; iter < 150; iter++ {
		width := 1 + rng.Intn(60)
		n := 1 + rng.Intn(6)
		blocks := make([]string, n)
		for i := range blocks {
			blocks[i] = randomBalancedBlock(rng)
		}
		want := wrapTranscript(strings.Join(blocks, "\n"), width)
		if got := cachedWrap(blocks, width); got != want {
			var b strings.Builder
			for i, block := range blocks {
				fmt.Fprintf(&b, "  [%d] %q\n", i, block)
			}
			t.Fatalf(
				"iter %d width %d: cached per-block wrapping diverged from whole-transcript wrapping\nblocks:\n%swhole    =%q\ncached    =%q",
				iter, width, b.String(), want, got,
			)
		}

		// Round two on the SAME model with one block mutated in place: the
		// cache must notice the stale entry and re-wrap that block.
		if iter%2 == 0 {
			var m chatTUI
			if got := m.wrapTranscriptBlocks(blocks, width); got != want {
				t.Fatalf("iter %d width %d: first cached wrap diverged: %q", iter, width, got)
			}
			idx := rng.Intn(len(blocks))
			blocks[idx] = randomBalancedBlock(rng)
			mutWant := wrapTranscript(strings.Join(blocks, "\n"), width)
			if got := m.wrapTranscriptBlocks(blocks, width); got != mutWant {
				t.Fatalf(
					"iter %d width %d: cached wrap after in-place mutation of block %d diverged (stale entry served?)\nblock   =%q\nwhole   =%q\ncached  =%q",
					iter, width, idx, blocks[idx], mutWant, got,
				)
			}
		}
	}
}

// TestWrapTranscriptPerBlockEquivalenceRealRenderers feeds the actual
// renderers' output through the production cache and asserts the invariant
// the cache relies on: every block ends with its SGR state closed.
func TestWrapTranscriptPerBlockEquivalenceRealRenderers(t *testing.T) {
	prev := activeColorProfile
	activeColorProfile = colorprofile.TrueColor
	defer func() { activeColorProfile = prev }()

	blocks := realRendererBlocks()
	for i, block := range blocks {
		if !lastSGREndsWithReset(block) {
			t.Errorf("renderer block %d does not end with a style reset (invariant violated): %q", i, block)
		}
	}

	for _, width := range []int{1, 2, 8, 24, 40, 60, 100} {
		want := wrapTranscript(strings.Join(blocks, "\n"), width)
		if got := cachedWrap(blocks, width); got != want {
			var b strings.Builder
			for i, block := range blocks {
				fmt.Fprintf(&b, "  [%d] %q\n", i, block)
			}
			t.Fatalf(
				"width %d: real-renderer cached wrapping diverged from whole-transcript wrapping\nblocks:\n%swhole    =%q\ncached    =%q",
				width, b.String(), want, got,
			)
		}
	}
}

// realRendererBlocks builds transcript blocks through the actual renderers
// (markdown, user bubble, reasoning, tool card, connector, banner, receipt,
// sub-agent preview) so the equivalence gate covers the real production shapes
// — indented lines, width-wrapped bodies, styled spans, empty blocks.
func realRendererBlocks() []string {
	return []string{
		renderAssistantMarkdown("# Heading one\n\nSome *italic* and **bold** text with a [link](https://example.com) and `code`.\n\n- item one\n- item two\n\n```go\nfunc main() {}\n```\n\n> quoted line\n\n| a | b |\n|---|---|\n| 1 | 2 |", 40),
		renderAssistantMarkdown("日本語の長いテキストと한국어 and emoji 🚀 plus a verylongwordthatcannotbreakanywhereinthemiddle to force hard wrapping behavior at this width", 40),
		renderAssistantMarkdown("Math spans $x^2 + y^2 = z^2$ inline and displayed $$\\int_0^1 f(x)\\,dx$$ at the end.", 40),
		renderAssistantMarkdown("", 40),
		renderUserBubble("user typed this prompt with some length", 40, false),
		renderUserBubble("[plan] a plan-mode prompt", 40, true),
		reasoningBlock("thinking about the problem\nwith multiple lines here and some longer text that wraps across several lines at this width", 40, 0),
		reasoningBlock("long reasoning text", 40, 2),
		toolCard("bash", `{"command":"ls -la /some/long/path/with/many/parts"}`, 40),
		toolCard("use_capability", `{"action":"call","capability_id":"mcp-server:github","tool":"search_issues"}`, 40),
		connectorBlock([]string{"first line", "second line", "third line"}),
		connectorBlock(nil),
		renderTUIBanner("test label", "", 40),
		renderTUIBanner("test label", "missing API key warning text that might be longer than the width allows", 40),
		renderTurnReceiptBand("receipt band text that may also wrap", 40),
		subagentPreviewBlock("✎", "preview text for a sub-agent that may wrap\nacross multiple lines", 40, 0),
		subagentPreviewBlock("!", "truncated preview notice", 40, 1),
	}
}

// TestNaivePerBlockWrapDivergesWithoutRepad documents the two non-SGR reasons
// a plain join of per-block wraps (perBlockWrap) diverges from the
// whole-transcript wrap, and why wrapTranscriptBlocks re-pads to the global
// widest line: (a) at width 1 x/ansi's wrap glues wide-run content into lines
// wider than the limit; (b) at any width a hyphen falling exactly at the fill
// column leaves a word glued onto the line, making it wider than the limit.
// lipgloss then pads every line of the whole string to that widest line.
func TestNaivePerBlockWrapDivergesWithoutRepad(t *testing.T) {
	cases := []struct {
		name   string
		blocks []string
		width  int
	}{
		{
			name:   "width-1 wide-run glue",
			blocks: []string{"ascii words here", "  文章990401991710\n\t한국어\t日本語 \n\tテキスト \n"},
			width:  1,
		},
		{
			name:   "hyphen fills line exactly",
			blocks: []string{"546496 foo-bar\n", "\t7605796 \t문장 \t177948with-hyphens\nttttttttttttttttttttttttttttttt hhhhhhhhhhhhhhhhhhhhhhhh"},
			width:  5,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			naive := perBlockWrap(tc.blocks, tc.width)
			whole := wrapTranscript(strings.Join(tc.blocks, "\n"), tc.width)
			if naive == whole {
				t.Skip("x/ansi wrap behavior changed: naive per-block now matches whole — the re-pad rationale is gone, revisit")
			}
			if got := cachedWrap(tc.blocks, tc.width); got != whole {
				t.Fatalf("cached wrap must still match whole-transcript wrap (re-pad failed):\nwhole =%q\ncached=%q", whole, got)
			}
			t.Logf("naive per-block diverges (documented); cache re-pad keeps output identical to whole-transcript wrap")
		})
	}
}

// TestWrapTranscriptPerBlockUnbalancedProbe documents what the gate catches:
// a block with an open SGR sequence at its end makes per-block wrapping
// diverge. Evidence-only (the cache is gated on the balanced tests above).
func TestWrapTranscriptPerBlockUnbalancedProbe(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	diverged := 0
	for iter := 0; iter < 60; iter++ {
		width := 2 + rng.Intn(40)
		n := 1 + rng.Intn(4)
		blocks := make([]string, n)
		for i := range blocks {
			blocks[i] = randomUnbalancedBlock(rng)
		}
		wrapped := wrapTranscript(strings.Join(blocks, "\n"), width)
		per := perBlockWrap(blocks, width)
		if wrapped == per {
			continue
		}
		diverged++
		if diverged <= 3 {
			offending := -1
			for i, block := range blocks {
				if !lastSGREndsWithReset(block) {
					offending = i
					break
				}
			}
			t.Logf(
				"iter %d width %d: divergence with unbalanced block [%d] %q\nwhole  =%q\nperBlock=%q",
				iter, width, offending, blocks[max(offending, 0)], wrapped, per,
			)
		}
	}
	t.Logf("unbalanced probe: %d/%d block sets diverged (expected; confirms the gate is meaningful)", diverged, 60)
}
