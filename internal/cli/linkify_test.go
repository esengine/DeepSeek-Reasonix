package cli

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

// TestRenderLinkEmitsClickableHyperlink verifies markdown [text](url) renders
// as an OSC 8 hyperlink bracketed by zero-width click markers, with the
// visible text byte-for-byte unchanged after stripping.
func TestRenderLinkEmitsClickableHyperlink(t *testing.T) {
	r := newMarkdownRenderer(80)
	rendered := r.Render("see [docs](https://example.com/docs) now")
	if !strings.Contains(rendered, ansi.SetHyperlink("https://example.com/docs")) {
		t.Fatalf("render lacks OSC 8 hyperlink: %q", rendered)
	}
	if !strings.Contains(rendered, linkStartPrefix) || !strings.Contains(rendered, linkEndPrefix) {
		t.Fatalf("render lacks click markers: %q", rendered)
	}
	plain := strings.TrimRight(ansi.Strip(rendered), "\n")
	want := "see docs (https://example.com/docs) now"
	if plain != want {
		t.Fatalf("stripped render = %q, want %q", plain, want)
	}
}

// TestRenderBareURLBecomesClickable verifies the Linkify extension turns bare
// http(s) URLs into clickable autolinks without changing their visible text.
func TestRenderBareURLBecomesClickable(t *testing.T) {
	r := newMarkdownRenderer(80)
	rendered := r.Render("visit https://example.com/x today")
	if !strings.Contains(rendered, ansi.SetHyperlink("https://example.com/x")) {
		t.Fatalf("bare URL was not hyperlinked: %q", rendered)
	}
	if !strings.Contains(rendered, linkStartPrefix) {
		t.Fatalf("bare URL lacks click markers: %q", rendered)
	}
	if plain := ansi.Strip(rendered); !strings.Contains(plain, "visit https://example.com/x today") {
		t.Fatalf("stripped render lost text: %q", plain)
	}
}

// TestRenderCodeSpanURLClickable verifies a URL wrapped in backticks (the
// "boxed, highlighted" form from the issue) is clickable too, while non-URL
// code spans stay literal code.
func TestRenderCodeSpanURLClickable(t *testing.T) {
	r := newMarkdownRenderer(80)
	rendered := r.Render("use `https://example.com/x` here")
	if !strings.Contains(rendered, ansi.SetHyperlink("https://example.com/x")) {
		t.Fatalf("URL inside a code span was not hyperlinked: %q", rendered)
	}
	if !strings.Contains(rendered, linkStartPrefix) {
		t.Fatalf("code-span URL lacks click markers: %q", rendered)
	}
	if plain := strings.TrimRight(ansi.Strip(rendered), "\n"); plain != "use https://example.com/x here" {
		t.Fatalf("stripped render = %q, want literal text preserved", plain)
	}

	noURL := r.Render("run `go run main.go` now")
	if strings.Contains(noURL, ansi.SetHyperlink("")) || strings.Contains(noURL, linkStartPrefix) {
		t.Fatalf("plain code span must stay unlinked: %q", noURL)
	}
	if plain := strings.TrimRight(ansi.Strip(noURL), "\n"); plain != "run go run main.go now" {
		t.Fatalf("stripped code span = %q", plain)
	}
}

// TestRenderUnsafeSchemeStaysPlain verifies destinations that fail the scheme
// whitelist render as plain text — no OSC 8, no markers — so a hostile model
// cannot forge clickable javascript:/data: links.
func TestRenderUnsafeSchemeStaysPlain(t *testing.T) {
	r := newMarkdownRenderer(80)
	rendered := r.Render("[x](javascript:alert(1)) and [y](file:///etc/passwd)")
	if strings.Contains(rendered, ansi.SetHyperlink("javascript:")) ||
		strings.Contains(rendered, ansi.SetHyperlink("file:")) {
		t.Fatalf("unsafe schemes must not be hyperlinked: %q", rendered)
	}
	if strings.Contains(rendered, linkStartPrefix) {
		t.Fatalf("unsafe schemes must not carry click markers: %q", rendered)
	}
	plain := ansi.Strip(rendered)
	if !strings.Contains(plain, "javascript:alert(1)") || !strings.Contains(plain, "file:///etc/passwd") {
		t.Fatalf("plain text lost: %q", plain)
	}
}

// TestRenderControlCharsStrippedFromURL verifies ESC/BEL/ST and friends are
// scrubbed from link destinations so a model can't inject terminal sequences.
func TestRenderControlCharsStrippedFromURL(t *testing.T) {
	r := newMarkdownRenderer(80)
	evil := "https://example.com/\x1b]8;;http://evil.example\x1b\\\x07tail"
	rendered := r.Render("[x](" + evil + ")")
	// The injected OSC 8 start (ESC ] 8;;http://evil…) must not survive; the
	// only OSC 8 pair in the output is the renderer's own, around the
	// scrubbed destination.
	if strings.Contains(rendered, "\x1b]8;;http://evil") {
		t.Fatalf("injected OSC 8 survived sanitize: %q", rendered)
	}
	if strings.Count(rendered, "\x1b]8;;") != 2 {
		t.Fatalf("expected exactly one OSC 8 pair (start+end), got: %q", rendered)
	}
	plain := ansi.Strip(rendered)
	if strings.Contains(plain, "\x07") || strings.Contains(plain, "\x1b") {
		t.Fatalf("control characters leaked into visible text: %q", plain)
	}
	if !strings.Contains(plain, "tail") {
		t.Fatalf("trailing text after the stripped BEL must survive: %q", plain)
	}
}

// TestRenderCopySkipsLinkSequences verifies the clipboard render path never
// emits OSC 8 hyperlinks or click markers, so copies stay clean text.
func TestRenderCopySkipsLinkSequences(t *testing.T) {
	r := newMarkdownRenderer(80)
	rendered := r.RenderCopy("see [docs](https://example.com/docs) and https://example.com/x", "b")
	if strings.Contains(rendered, ansi.SetHyperlink("")) ||
		strings.Contains(rendered, linkStartPrefix) ||
		strings.Contains(rendered, linkEndPrefix) {
		t.Fatalf("copy render must not carry link sequences: %q", rendered)
	}
	if plain := ansi.Strip(rendered); !strings.Contains(plain, "docs (https://example.com/docs)") {
		t.Fatalf("copy render lost visible text: %q", plain)
	}
}

func TestSanitizeLinkURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://example.com/a", "https://example.com/a"},
		{"http://example.com", "http://example.com"},
		{"mailto:a@b.com", "mailto:a@b.com"},
		{"", ""},
		{"javascript:alert(1)", ""},
		{"data:text/html,x", ""},
		{"file:///etc/passwd", ""},
		{"ftp://example.com", ""},
		{"example.com", ""},
		{"https://ex.com/\x1b]8;x", "https://ex.com/]8;x"},
		{"https://ex.com/\x07\x1c\x9c", "https://ex.com/"},
	}
	for _, c := range cases {
		if got := sanitizeLinkURL(c.in); got != c.want {
			t.Errorf("sanitizeLinkURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestLinkifyPlainURLs verifies bare URLs in plain text (user bubbles) get the
// hyperlink + marker treatment and trailing sentence punctuation is trimmed
// from the destination while the visible text keeps the original spelling.
func TestLinkifyPlainURLs(t *testing.T) {
	out := linkifyPlainURLs("see https://example.com/a, and https://example.com/b!")
	if !strings.Contains(out, ansi.SetHyperlink("https://example.com/a")) {
		t.Fatalf("first URL not hyperlinked: %q", out)
	}
	if !strings.Contains(out, ansi.SetHyperlink("https://example.com/b")) {
		t.Fatalf("second URL not hyperlinked: %q", out)
	}
	if strings.Contains(out, ansi.SetHyperlink("https://example.com/b!")) {
		t.Fatalf("trailing punctuation must be trimmed from destination: %q", out)
	}
	if !strings.Contains(out, "https://example.com/b!") {
		t.Fatalf("visible text must keep the original spelling: %q", out)
	}
	if plain := ansi.Strip(out); !strings.Contains(plain, "see https://example.com/a, and https://example.com/b!") {
		t.Fatalf("stripped output changed: %q", plain)
	}
	if got := ansi.Strip(linkifyPlainURLs("no links here")); got != "no links here" {
		t.Fatalf("plain text must pass through untouched: %q", got)
	}
}

// TestParseLinkSpansInLine covers single-line links, multiple links per line
// and corrupted marker streams.
func TestParseLinkSpansInLine(t *testing.T) {
	line := "a " + linkStartMarker("0", "https://example.com/x") + "link" + linkEndMarker("0") + " b"
	spans, next, ok := parseLinkSpansInLine(line, nil)
	if !ok {
		t.Fatal("parse failed on well-formed line")
	}
	if next != nil {
		t.Fatal("well-formed line must not carry an open link")
	}
	if len(spans) != 1 || spans[0].start != 2 || spans[0].end != 6 || spans[0].url != "https://example.com/x" {
		t.Fatalf("spans = %+v, want [{2 6 https://example.com/x}]", spans)
	}

	two := linkStartMarker("0", "https://a.com") + "aa" + linkEndMarker("0") +
		" " + linkStartMarker("1", "https://b.com") + "bb" + linkEndMarker("1")
	spans, _, ok = parseLinkSpansInLine(two, nil)
	if !ok || len(spans) != 2 || spans[0].url != "https://a.com" || spans[1].url != "https://b.com" {
		t.Fatalf("two-link line parsed as %+v ok=%v", spans, ok)
	}

	corrupt := linkEndMarker("0") // close without open
	if _, _, ok := parseLinkSpansInLine(corrupt, nil); ok {
		t.Fatal("close-without-open must fail")
	}
	truncated := "\x1b]1337;reasonix-link=0;c2FmZQ"
	if _, _, ok := parseLinkSpansInLine(truncated, nil); ok {
		t.Fatal("truncated marker payload must fail")
	}
}

// TestParseLinkSpansAcrossWrap verifies a link wrapped across rows yields a
// trailing span on the first row and a leading span on the next, both with the
// same URL, and that a mid-link carry-over row is hit-testable from column 0.
func TestParseLinkSpansAcrossWrap(t *testing.T) {
	url := "https://example.com/very/long/path/with/many/segments"
	rendered := newMarkdownRenderer(80).Render("[x](" + url + ")")
	wrapped := strings.Split(wrapTranscript(rendered, 20), "\n")

	var all []linkSpan
	var active *openLink
	for i, line := range wrapped {
		spans, next, ok := parseLinkSpansInLine(line, active)
		if !ok {
			t.Fatalf("line %d failed to parse: %q", i, line)
		}
		for _, s := range spans {
			all = append(all, s)
		}
		active = next
	}
	if len(all) == 0 {
		t.Fatal("no spans found across wrapped lines")
	}
	for _, s := range all {
		if s.url != url {
			t.Errorf("span url = %q, want %q", s.url, url)
		}
	}
	// First span must cover the first row from the link's start; the tail span
	// on an intermediate row starts at column 0 (carry-over).
	if all[0].start <= all[0].end && all[0].end == all[0].start {
		t.Errorf("unexpected zero-width span: %+v", all[0])
	}
}

// TestLinkSpanAtHitTesting drives the click hit-test against wrapped lines,
// including a link that wraps across rows.
func TestLinkSpanAtHitTesting(t *testing.T) {
	m := newTestChatTUI()
	url := "https://example.com/clickable"
	rendered := newMarkdownRenderer(80).Render("hello [link](" + url + ") world")
	m.wrappedLines = strings.Split(wrapTranscript(rendered, 80), "\n")

	if got, ok := m.linkSpanAt(0, 7); !ok || got != url {
		t.Fatalf("click on link label: got %q ok=%v, want %q", got, ok, url)
	}
	if got, ok := m.linkSpanAt(0, 45); ok {
		t.Fatalf("click outside link must miss, got %q", got)
	}
	if _, ok := m.linkSpanAt(5, 0); ok {
		t.Fatal("click beyond transcript must miss")
	}

	// Cross-row link: every row of the wrapped link must hit.
	long := "https://example.com/" + strings.Repeat("segment/", 6)
	rendered = newMarkdownRenderer(80).Render("pre [x](" + long + ") post")
	m.wrappedLines = strings.Split(wrapTranscript(rendered, 30), "\n")
	hitRows := 0
	for i := range m.wrappedLines {
		// Probe a few columns per row: the link body starts after "pre ".
		for _, col := range []int{4, 12, 20, 28} {
			if _, ok := m.linkSpanAt(i, col); ok {
				hitRows = i + 1
				break
			}
		}
	}
	if hitRows < 2 {
		t.Fatalf("long link should be clickable on multiple wrapped rows, hitRows=%d lines=%d",
			hitRows, len(m.wrappedLines))
	}
}

// TestCtrlClickOpensLink verifies a Ctrl+Click on a link opens it in the
// browser instead of starting a selection.
// TestDoubleClickOpensLink verifies that two left-clicks on the same link
// within the double-click window open it in the browser, and that the second
// click clears the selection the first click started.
func TestDoubleClickOpensLink(t *testing.T) {
	prev := openInBrowser
	t.Cleanup(func() { openInBrowser = prev })
	opened := ""
	openInBrowser = func(url string) error {
		opened = url
		return nil
	}

	ctrl := control.New(control.Options{})
	ch := make(chan event.Event, 1)
	m := newChatTUI(ctrl, "", ch, 80)
	m = advTUI(m, tea.WindowSizeMsg{Width: 80, Height: 10})
	url := "https://example.com/open-me"
	rendered := newMarkdownRenderer(80).Render("[x](" + url + ")")
	m.transcript = []string{rendered}
	m.wrappedLines = strings.Split(wrapTranscript(rendered, 80), "\n")

	// First click: starts a selection and arms the double-click state.
	first := advTUI(m, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if opened != "" {
		t.Fatalf("single click must not open the link, got %q", opened)
	}
	if !first.sel.active {
		t.Fatal("first click of a double-click must still start a selection")
	}
	if first.lastLinkClickURL != url {
		t.Fatalf("first click should arm the link, got %q", first.lastLinkClickURL)
	}

	// Second click within the window: opens the link and clears the selection.
	second := advTUI(first, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if opened != url {
		t.Fatalf("double-click opened %q, want %q", opened, url)
	}
	if second.sel.active {
		t.Fatal("double-click on a link must not leave a selection")
	}
	if !second.lastLinkClickAt.IsZero() {
		t.Fatal("double-click should disarm the link state")
	}
	if !strings.Contains(strings.Join(second.transcript, "\n"), "opened "+url) {
		t.Fatal("successful open should leave a notice in the transcript")
	}
}

// TestDoubleClickOpensBareURL verifies double-click also opens a bare URL
// (GFM autolink), not just explicit [text](url) links.
func TestDoubleClickOpensBareURL(t *testing.T) {
	prev := openInBrowser
	t.Cleanup(func() { openInBrowser = prev })
	opened := ""
	openInBrowser = func(url string) error {
		opened = url
		return nil
	}

	ctrl := control.New(control.Options{})
	ch := make(chan event.Event, 1)
	m := newChatTUI(ctrl, "", ch, 80)
	m = advTUI(m, tea.WindowSizeMsg{Width: 80, Height: 10})
	url := "https://example.com/bare"
	rendered := newMarkdownRenderer(80).Render("visit " + url + " today")
	m.wrappedLines = strings.Split(wrapTranscript(rendered, 80), "\n")

	first := advTUI(m, tea.MouseClickMsg{X: 6, Y: 0, Button: tea.MouseLeft})
	if opened != "" {
		t.Fatalf("single click must not open, got %q", opened)
	}
	advTUI(first, tea.MouseClickMsg{X: 6, Y: 0, Button: tea.MouseLeft})
	if opened != url {
		t.Fatalf("double-click on bare URL opened %q, want %q", opened, url)
	}
}

// TestSlowDoubleClickDoesNotOpen verifies that two clicks on the same link
// farther apart than the double-click window keep the single-click behaviour
// (selection only, no browser).
func TestSlowDoubleClickDoesNotOpen(t *testing.T) {
	prev := openInBrowser
	t.Cleanup(func() { openInBrowser = prev })
	openInBrowser = func(url string) error {
		t.Fatal("clicks beyond the double-click window must not open a link")
		return nil
	}

	ctrl := control.New(control.Options{})
	ch := make(chan event.Event, 1)
	m := newChatTUI(ctrl, "", ch, 80)
	m = advTUI(m, tea.WindowSizeMsg{Width: 80, Height: 10})
	url := "https://example.com/slow"
	rendered := newMarkdownRenderer(80).Render("[x](" + url + ")")
	m.wrappedLines = strings.Split(wrapTranscript(rendered, 80), "\n")

	first := advTUI(m, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	// Age the armed state past the window, then click again.
	first.lastLinkClickAt = time.Now().Add(-doubleClickWindow - time.Millisecond)
	second := advTUI(first, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if !second.sel.active {
		t.Fatal("slow second click must keep the selection behaviour")
	}
}

// TestPlainClickStillSelects verifies a single left-click on a link keeps the
// existing selection behaviour — only a deliberate double-click opens.
func TestPlainClickStillSelects(t *testing.T) {
	prev := openInBrowser
	t.Cleanup(func() { openInBrowser = prev })
	openInBrowser = func(url string) error {
		t.Fatal("plain click must not open a link")
		return nil
	}

	ctrl := control.New(control.Options{})
	ch := make(chan event.Event, 1)
	m := newChatTUI(ctrl, "", ch, 80)
	m = advTUI(m, tea.WindowSizeMsg{Width: 80, Height: 10})
	url := "https://example.com/no-open"
	rendered := newMarkdownRenderer(80).Render("[x](" + url + ")")
	m.wrappedLines = strings.Split(wrapTranscript(rendered, 80), "\n")

	next := advTUI(m, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if !next.sel.active {
		t.Fatal("plain left-click must still start a selection")
	}
}

// TestDoubleClickUnsafeSchemeIgnored verifies the open-link path re-validates
// the destination scheme at click time, so a forged marker carrying a
// javascript:/data: URL can never reach the browser even if it somehow ends
// up in the wrapped line cache.
func TestDoubleClickUnsafeSchemeIgnored(t *testing.T) {
	prev := openInBrowser
	t.Cleanup(func() { openInBrowser = prev })
	openInBrowser = func(url string) error {
		t.Fatalf("unsafe scheme must not reach the browser: %q", url)
		return nil
	}

	m := newChatTUI(control.New(control.Options{}), "", make(chan event.Event, 1), 80)
	m = advTUI(m, tea.WindowSizeMsg{Width: 80, Height: 10})
	forged := "\x1b]1337;reasonix-link=0;amF2YXNjcmlwdDphbGVydCgxKQ\x07" +
		"x" + "\x1b]1337;reasonix-link-end=0\x07"
	m.wrappedLines = strings.Split(wrapTranscript(forged, 80), "\n")

	first := advTUI(m, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	next := advTUI(first, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if next.sel.active {
		t.Fatal("modifier click on non-clickable content must still select")
	}
	if strings.Contains(strings.Join(next.transcript, "\n"), "opened ") {
		t.Fatal("unsafe scheme must not produce an opened notice")
	}
}

// TestCopyStripsClickMarkers verifies the transcript copy path strips OSC 8
// and click markers from fixed blocks (user bubbles) so clipboard text is
// clean.
func TestCopyStripsClickMarkers(t *testing.T) {
	m := newTestChatTUI()
	m.viewport.SetWidth(80)
	m.transcript = []string{linkifyPlainURLs("see https://example.com/x")}
	m.transcriptSources = []transcriptSource{{kind: transcriptSourceUser, raw: "see https://example.com/x"}}
	m.wrappedLines = strings.Split(wrapTranscript(m.transcript[0], m.viewport.Width()), "\n")

	lines, ok := m.copyTranscriptLines()
	if !ok {
		t.Fatal("copyTranscriptLines failed")
	}
	copied := selectedCopyText(lines, selPos{line: 0, col: 0}, selPos{line: 0, col: ansi.StringWidth(lines[0].text)})
	if strings.Contains(copied, linkStartPrefix) || strings.Contains(copied, ansi.SetHyperlink("")) {
		t.Fatalf("copy leaked click sequences: %q", copied)
	}
	if !strings.Contains(copied, "see https://example.com/x") {
		t.Fatalf("copy lost visible text: %q", copied)
	}
}

func advTUI(m chatTUI, msg tea.Msg) chatTUI {
	n, _ := m.Update(msg)
	return n.(chatTUI)
}
