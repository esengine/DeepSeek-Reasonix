// This file renders a fenced ```diff / ```patch block: the colourised diff path
// behind [cli].diff_fences, and the plain-rail fallbacks around it. Split out of
// md.go to keep that file under the 800-line ceiling.
package termrender

import (
	"slices"
	"strings"
	"sync"

	"github.com/yuin/goldmark/ast"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
)

// renderCodeRail draws a plain fenced code block on the code rail — the lossless
// path for a fence the diff renderer does not take up. A colourised row costs far
// more to lay out than a plain one.
func (r *MarkdownRenderer) renderCodeRail(buf *strings.Builder, n ast.Node, src []byte, indent int) {
	prefix := strings.Repeat(" ", indent) + Dim("│ ")
	if r.hideRail {
		prefix = strings.Repeat(" ", indent+VisibleWidth("│ "))
	}
	for i := range n.Lines().Len() {
		l := n.Lines().At(i)
		line := strings.TrimRight(string(l.Value(src)), "\n")
		buf.WriteString(prefix)
		buf.WriteString(Accent(line))
		buf.WriteString("\n")
	}
	buf.WriteString("\n")
}

// codeRailPrefix is the fenced-code gutter: a dim "│ " rail, or spaces when the
// terminal owns the viewport chrome. A diff fence that cannot be parsed as one
// falls back to it so no line is dropped.
func codeRailPrefix(indent int, hideRail bool) string {
	if hideRail {
		return strings.Repeat(" ", indent+VisibleWidth("│ "))
	}
	return strings.Repeat(" ", indent) + Dim("│ ")
}

// activeDiffFences opts a fenced ```diff / ```patch block into the colourised
// diff renderer; false (the default) keeps every fence on the plain code rail,
// the lossless choice for the headerless fences models write. Set once at CLI
// startup from [cli].diff_fences. A streaming fence colours in hunk by hunk,
// leaving only the in-progress hunk on the plain rail.
var activeDiffFences bool

// configureDiffFences resolves [cli].diff_fences into activeDiffFences. It is
// user-global only, mirroring [cli].diff_formatter.
func configureDiffFences(cfg *config.Config) {
	activeDiffFences = cfg != nil && cfg.CLI.DiffFences
}

// isDiffFence reports whether a fence's info string asks for a unified diff.
// Copy mode keeps the plain rail path so the copied text stays byte-identical
// to the fence source (the diff rows carry render-only gutter/line numbers).
func isDiffFence(fc *ast.FencedCodeBlock, src []byte) bool {
	if fc.Info == nil {
		return false
	}
	switch string(fc.Info.Segment.Value(src)) {
	case "diff", "patch":
		return true
	}
	return false
}

func (r *MarkdownRenderer) renderDiffFence(buf *strings.Builder, fc *ast.FencedCodeBlock, src []byte, indent int) {
	width := max(r.width-indent, 8)
	text := diffFenceText(fc, src)
	// Each "@@ " hunk header settles the rows before it, so render that prefix
	// and leave only the in-progress hunk on the plain rail: the diff colours in
	// as it streams, and a closed fence settles the whole body.
	settled, tail := text, ""
	if !fenceClosed(fc, src) {
		settled, tail = splitAtLastHunk(text)
	}
	// The formatter owns the settled body, SGR or not: run it before the verbatim
	// check so a configured formatter still sees a pasted-coloured fence.
	if settled != "" {
		if out, ok := formatDiffCached(settled, width); ok {
			writeFormatted(buf, out)
			r.renderRailText(buf, tail, indent)
			buf.WriteString("\n")
			return
		}
		// The settled body just grew by a hunk, so its run is pending again:
		// keep the rows already formatted and leave only the newly settled
		// remainder (and the in-progress hunk) on the rail until it lands.
		if head, rest, found := landedSettledPrefix(settled, width); found {
			if out, ok := formatDiffCached(head, width); ok {
				writeFormatted(buf, out)
				r.renderRailText(buf, rest, indent)
				r.renderRailText(buf, tail, indent)
				buf.WriteString("\n")
				return
			}
		}
	}
	// A fence that already carries ANSI (e.g. pasted delta output) is shown
	// verbatim — splitting/counting it would mis-read the SGR-prefixed lines.
	if hasSGR(text) {
		prefix := strings.Repeat(" ", indent)
		for _, row := range verbatimDiffBody(text, width, 0) {
			buf.WriteString(prefix)
			buf.WriteString(row)
			buf.WriteString("\n")
		}
		buf.WriteString("\n")
		return
	}
	if settled != "" {
		r.renderSettledRows(buf, settled, width, indent)
	}
	r.renderRailText(buf, tail, indent)
	buf.WriteString("\n")
}

// writeFormatted writes a formatter's output, ensuring it ends in a newline so
// the rows after it start on their own line.
func writeFormatted(buf *strings.Builder, out string) {
	buf.WriteString(out)
	if !strings.HasSuffix(out, "\n") {
		buf.WriteString("\n")
	}
}

// landedSettledPrefix returns the largest prefix of a settled diff body whose
// formatter result has landed, and the remainder still pending. A settled body
// grows one hunk at a time, so its previous value — the body without its last
// hunk — is usually the landed prefix; the walk back covers a burst that
// settled several hunks in one frame. found is false when none has landed.
func landedSettledPrefix(settled string, width int) (prefix, rest string, found bool) {
	for body := settled; ; {
		head, _ := splitAtLastHunk(body)
		if head == "" {
			return "", "", false
		}
		if diffFormatLanded(head, width) {
			return head, settled[len(head)+1:], true
		}
		body = head
	}
}

// renderSettledRows writes a settled diff body the built-in way: the colourised
// rows once a configured formatter is out of the running, the plain rail while
// its result is still pending. The formatter's own output is written by the
// caller, which also still has a tail to draw.
func (r *MarkdownRenderer) renderSettledRows(buf *strings.Builder, body string, width, indent int) {
	if diffFormatPending(body, width) {
		r.renderRailText(buf, body, indent)
		return
	}
	prefix := strings.Repeat(" ", indent)
	for _, row := range diffFenceRows(body, width, r.hideRail) {
		buf.WriteString(prefix)
		buf.WriteString(row)
		buf.WriteString("\n")
	}
}

// renderRailText writes body on the plain code rail, one row per line: the
// in-progress hunk of a streaming fence, the placeholder while a configured
// formatter's output is pending, and the newly settled remainder until it lands.
func (r *MarkdownRenderer) renderRailText(buf *strings.Builder, body string, indent int) {
	if body == "" {
		return
	}
	rail := codeRailPrefix(indent, r.hideRail)
	for line := range strings.SplitSeq(strings.TrimRight(body, "\n"), "\n") {
		buf.WriteString(rail)
		buf.WriteString(Accent(line))
		buf.WriteString("\n")
	}
}

// splitAtLastHunk cuts text at its last "@@ " hunk header once an earlier hunk
// is complete: everything before that header is settled, the rest is the
// in-progress hunk. With fewer than two headers no hunk has finished, so the
// whole body is in progress.
func splitAtLastHunk(text string) (settled, tail string) {
	lines := strings.Split(text, "\n")
	last, prev := -1, -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, "@@ ") {
			prev, last = last, i
		}
	}
	if prev < 0 {
		return "", text
	}
	// The next file's header ("diff --git"/"index" preamble and "--- "/"+++ "
	// pair) streams before its "@@ " hunk, so it can trail the settled part with
	// no hunk after it. Move it into the tail until its "@@ " arrives.
	cut := last
	if start := danglingFileHeader(lines, last); start >= 0 {
		cut = start
	}
	return strings.Join(lines[:cut], "\n"), strings.Join(lines[cut:], "\n")
}

// danglingFileHeader returns the index where a trailing file header begins in
// lines[:cut] — the next file's "--- "/"+++ " pair and the git preamble lines
// above it, sitting at the end of the settled part with no "@@ " after them — or
// -1. Only a complete pair counts: a lone "--- " line is not yet a header, and a
// pair a "@@ " follows is a real section start rather than a dangling one.
func danglingFileHeader(lines []string, cut int) int {
	if cut < 2 || !strings.HasPrefix(lines[cut-2], "--- ") || !strings.HasPrefix(lines[cut-1], "+++ ") {
		return -1
	}
	start := cut - 2
	for start > 0 && isDiffPreambleLine(lines[start-1]) {
		start--
	}
	return start
}

// isDiffPreambleLine reports whether a line belongs to git's per-file preamble
// ("diff --git", "index", mode/rename lines) rather than to a hunk body: every
// hunk line starts with " ", "+", "-", "@" or "\".
func isDiffPreambleLine(line string) bool {
	if line == "" {
		return false
	}
	switch line[0] {
	case ' ', '+', '-', '@', '\\':
		return false
	}
	return true
}

// diffFenceRows renders a diff body as colourised rows — a header per file
// section, then its highlighted hunks — without the fence's own indent. A
// section with no "--- "/"+++ " header falls back to the plain rail.
func diffFenceRows(text string, width int, hideRail bool) []string {
	rail := codeRailPrefix(0, hideRail)
	var rows []string
	for _, sec := range splitDiffSections(text) {
		body := trimDiffPreamble(sec)
		if body == "" {
			// A section with no "--- "/"+++ " header is not one diffBody can lay
			// out: a bare "@@ …" hunk is real content (plain rail), a pure
			// preamble such as a `git show` commit header is dropped.
			if !hasDiffBody(sec) {
				continue
			}
			for line := range strings.SplitSeq(strings.TrimRight(sec, "\n"), "\n") {
				rows = append(rows, rail+Accent(line))
			}
			continue
		}
		path := diffFencePath(body)
		if header := diffFenceHeader(path, countDiff(body)); header != "" {
			rows = append(rows, header)
		}
		for hi, hunk := range splitHunks(body) {
			if hi > 0 {
				rows = append(rows, "  "+Dim("⋮"))
			}
			rows = append(rows, hunkRowsCached(hunk, path, width, hideRail)...)
		}
	}
	return rows
}

// splitHunks cuts a section body into its hunks, each beginning at its "@@ "
// header; text before the first header (the "--- "/"+++ " pair) is dropped.
func splitHunks(body string) []string {
	lines := strings.Split(body, "\n")
	var starts []int
	for i, ln := range lines {
		if strings.HasPrefix(ln, "@@ ") {
			starts = append(starts, i)
		}
	}
	hunks := make([]string, 0, len(starts))
	for i, s := range starts {
		end := len(lines)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		hunks = append(hunks, strings.Join(lines[s:end], "\n"))
	}
	return hunks
}

// builtinDiffCache memoizes one hunk's colourised rows per (hunk, path, width,
// rail, theme). A streaming fence's completed hunks never change, so each is
// highlighted once however many frames redraw it, and the theme is in the key so
// a runtime theme switch cannot serve the old palette's colours. Caching the
// whole body instead would re-highlight every earlier hunk at each "@@ ".
var (
	builtinDiffMu    sync.Mutex
	builtinDiffCache = map[builtinDiffKey][]string{}
	builtinDiffOrder []builtinDiffKey
)

type builtinDiffKey struct {
	hash  DiffKey
	rail  bool
	theme Palette
}

// A fence's hunks are all live while it streams, so the cap must exceed the
// hunk count of any one fence or eviction re-highlights every frame. 1024 hunks
// of rows is O(diff size) memory, not a per-entry multiple of it.
const builtinDiffCacheMax = 1024

// diffHunkMemo is on in production; the streaming benchmark turns it off to
// price the per-frame re-highlight the memo replaced.
var diffHunkMemo = true

func hunkRowsCached(hunk, path string, width int, hideRail bool) []string {
	// A section's trailing newline makes splitHunks return its last hunk with one
	// and earlier hunks without, so keying on the raw text would highlight one
	// hunk twice. Normalize it away before keying.
	hunk = strings.TrimRight(hunk, "\n")
	if !diffHunkMemo {
		return diffBody(event.FileDiff{Diff: hunk}, path, width, 0)
	}
	key := builtinDiffKey{hash: diffKeyOf(hunk+"\x00"+path, width), rail: hideRail, theme: activeTheme}
	builtinDiffMu.Lock()
	if rows, ok := builtinDiffCache[key]; ok {
		builtinDiffMu.Unlock()
		return rows
	}
	builtinDiffMu.Unlock()
	rows := diffBody(event.FileDiff{Diff: hunk}, path, width, 0)
	builtinDiffMu.Lock()
	if _, seen := builtinDiffCache[key]; !seen {
		if len(builtinDiffOrder) >= builtinDiffCacheMax {
			delete(builtinDiffCache, builtinDiffOrder[0])
			builtinDiffOrder = builtinDiffOrder[1:]
		}
		builtinDiffCache[key] = rows
		builtinDiffOrder = append(builtinDiffOrder, key)
	}
	builtinDiffMu.Unlock()
	return rows
}

// hasDiffBody reports whether a section carries diff content (a "@@ …" hunk or
// an added/removed line) even without a "--- "/"+++ " header. A section that
// only holds a `git show` commit header does not.
func hasDiffBody(section string) bool {
	for line := range strings.SplitSeq(section, "\n") {
		if strings.HasPrefix(line, "@@ ") || strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			return true
		}
	}
	return false
}

// fenceClosed reports whether src actually carried the closing fence line for
// fc. goldmark still builds a FencedCodeBlock for a fence left open at EOF, so
// its Lines() run to the end of the source; a closed fence's last body line
// stops before its closing line. A block with no body lines (an empty or
// still-open fence) reads as open, which keeps the external formatter off it.
func fenceClosed(fc *ast.FencedCodeBlock, src []byte) bool {
	lines := fc.Lines()
	if lines.Len() == 0 {
		return false
	}
	return lines.At(lines.Len()-1).Stop < len(src)
}

func diffFenceText(fc *ast.FencedCodeBlock, src []byte) string {
	var b strings.Builder
	for i := range fc.Lines().Len() {
		l := fc.Lines().At(i)
		b.WriteString(strings.TrimRight(string(l.Value(src)), "\n"))
		b.WriteByte('\n')
	}
	return b.String()
}

// diffFenceHeader names the file and its +/- tally: for a fence it stands in for
// the tool card's header line (a prose fence carries no tool name or args), and
// DiffText reuses it for a shell diff's card.
func diffFenceHeader(path string, d event.FileDiff) string {
	var parts []string
	if path != "" {
		parts = append(parts, Bold(Accent(path)))
	}
	if stat := DiffStat(d); stat != "" {
		parts = append(parts, stat)
	}
	return strings.Join(parts, "  ")
}

// splitDiffSections cuts a diff — a fence body or a whole shell diff — into
// per-file sections so each gets its own header. Each section is returned
// untrimmed: the caller drops the preamble (git's "diff --git"/"index" lines)
// with trimDiffPreamble, then a section with no "--- "/"+++ " pair either falls
// back to the plain rail (diffFenceRows) or is dropped (DiffText).
func splitDiffSections(diff string) []string {
	lines := strings.Split(strings.TrimRight(diff, "\n"), "\n")
	gitStyle := slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, "diff --git ") })
	var sections []string
	var cur []string
	for i, ln := range lines {
		if len(cur) > 0 && diffSectionStart(lines, i, gitStyle) {
			sections = append(sections, strings.Join(cur, "\n")+"\n")
			cur = nil
		}
		cur = append(cur, ln)
	}
	if len(cur) > 0 {
		sections = append(sections, strings.Join(cur, "\n")+"\n")
	}
	return sections
}

// diffSectionStart reports whether lines[i] opens a new file section. With
// git-style fences the "diff --git " line is authoritative; otherwise a
// "--- "/"+++ " pair is, but only the pair a "@@ " hunk follows: a removed
// "-- x" line renders "--- x", so a pair without a hunk after it would drop
// both changed comment rows as a file header.
func diffSectionStart(lines []string, i int, gitStyle bool) bool {
	if gitStyle {
		return strings.HasPrefix(lines[i], "diff --git ")
	}
	return isFileHeaderPair(lines, i)
}

// isFileHeaderPair reports whether lines[i] opens a real "--- "/"+++ " file
// header: the pair the unified-diff grammar puts immediately before the first
// "@@ " hunk. Requiring that hunk keeps a removed "-- x" line (rendered
// "--- x") from being mistaken for a header when another changed line follows.
func isFileHeaderPair(lines []string, i int) bool {
	return strings.HasPrefix(lines[i], "--- ") &&
		i+2 < len(lines) &&
		strings.HasPrefix(lines[i+1], "+++ ") &&
		strings.HasPrefix(lines[i+2], "@@ ")
}

// trimDiffPreamble drops a section's preamble (git's "diff --git"/"index" lines,
// or a `git show` commit header) so the returned text starts at the "--- " line
// diffBody drops. A section with no real file header returns empty, so a
// headerless hunk falls back to the plain rail rather than having a removed
// "-- x" line (rendered "--- x") mistaken for the header.
func trimDiffPreamble(section string) string {
	lines := strings.Split(section, "\n")
	for i := range lines {
		if isFileHeaderPair(lines, i) {
			return strings.Join(lines[i:], "\n")
		}
	}
	return ""
}

// countDiff tallies a section's added/removed rows for the header stat, using
// the same positional header-pair drop as diffBody.
func countDiff(section string) event.FileDiff {
	lines := strings.Split(strings.TrimRight(section, "\n"), "\n")
	if len(lines) >= 2 && strings.HasPrefix(lines[0], "--- ") && strings.HasPrefix(lines[1], "+++ ") {
		lines = lines[2:]
	}
	var d event.FileDiff
	for _, ln := range lines {
		switch {
		case strings.HasPrefix(ln, "+"):
			d.Added++
		case strings.HasPrefix(ln, "-"):
			d.Removed++
		}
	}
	return d
}

// diffFencePath reads a section's file from its "+++ b/…" line. Named for the
// fence path that first needed it; DiffText reuses it for a shell diff.
func diffFencePath(diff string) string {
	for line := range strings.SplitSeq(diff, "\n") {
		if rest, ok := strings.CutPrefix(line, "+++ b/"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}
