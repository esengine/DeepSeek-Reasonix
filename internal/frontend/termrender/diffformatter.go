// This file implements the optional external diff formatter: a user-global
// command (e.g. delta) that formats a diff before the CLI emits it — a fenced
// ```diff / ```patch block in the conversation answer stream, a writer tool's
// diff card in the transcript, and a shell result whose whole output is a diff
// (DiffText). Configured via [cli].diff_formatter as an argv line and exec'd
// directly — no shell.
package termrender

import (
	"bytes"
	"context"
	"hash/maphash"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/contract/config"
)

const (
	// diffFormatTimeout bounds the external formatter so a hung command cannot
	// stall a render (inline) or hold a background run open; on timeout the
	// built-in path is used.
	diffFormatTimeout = 3 * time.Second
	// diffFormatWaitDelay bounds the wait for a formatter's pipes to close after
	// the process exits or the timeout fires. Without it a formatter that leaves
	// a child holding stdout (sh -c 'sleep 12 & cat') blocks Wait for the
	// child's lifetime, so the timeout does not actually bound the run.
	diffFormatWaitDelay = 500 * time.Millisecond
	// diffFormatMaxBytes skips the external formatter for very large blocks,
	// where the subprocess round-trip costs more than it saves and the built-in
	// renderer (which folds) is the better default.
	diffFormatMaxBytes = 1 << 20 // 1 MiB
)

// activeDiffFormatter is the argv of the configured [cli].diff_formatter; empty
// when unset. Resolved once at CLI startup, like activeTheme.
var activeDiffFormatter []string

// configureDiffFormatter resolves [cli].diff_formatter into activeDiffFormatter.
// It is user-global only: a repo-local reasonix.toml cannot run a command on the
// user's machine.
func configureDiffFormatter(cfg *config.Config) {
	var argv []string
	if cfg != nil {
		argv = splitDiffFormatter(cfg.CLI.DiffFormatter)
	}
	diffFormatMu.Lock()
	activeDiffFormatter = argv
	diffFormatCache, diffFormatOrder = map[DiffKey]diffFormatEntry{}, nil
	diffFormatInflight = map[DiffKey]bool{}
	diffFormatDisabled = false
	diffFormatMu.Unlock()
}

// SetDiffFormatterForTest installs the [cli].diff_formatter argv for a test and
// clears the memo so a case never reads another's result. Restore with the
// returned function; production configures it from the user's config.
func SetDiffFormatterForTest(argv []string) (restore func()) {
	diffFormatMu.Lock()
	prev := activeDiffFormatter
	activeDiffFormatter = argv
	diffFormatCache, diffFormatOrder = map[DiffKey]diffFormatEntry{}, nil
	diffFormatInflight = map[DiffKey]bool{}
	diffFormatMu.Unlock()
	return func() { SetDiffFormatterForTest(prev) }
}

// SetDiffFormatNotify registers fn as the re-render hook: the formatter calls it
// from its goroutine with the key that landed, so the frontend can repaint only
// the rows that asked for it. Pass nil to run the formatter inline instead — a
// renderer that cannot re-render then still shows the output on the frame it
// draws.
func SetDiffFormatNotify(fn func(DiffKey)) {
	diffFormatMu.Lock()
	diffFormatNotify = fn
	diffFormatMu.Unlock()
}

// splitDiffFormatter splits a configured command line into argv, honouring single
// and double quotes so a path with spaces survives. It performs no shell
// expansion — the argv is exec'd directly.
func splitDiffFormatter(s string) []string {
	var argv []string
	var cur strings.Builder
	haveToken := false
	var quote byte // 0 = none, '\'' or '"'
	for i := range len(s) {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote = c
			haveToken = true
		case c == ' ' || c == '\t':
			if haveToken {
				argv = append(argv, cur.String())
				cur.Reset()
				haveToken = false
			}
		default:
			cur.WriteByte(c)
			haveToken = true
		}
	}
	if haveToken {
		argv = append(argv, cur.String())
	}
	return argv
}

// renderDiffExternal pipes the unified diff through argv (exec, no shell) and
// returns its stdout clamped to width. The formatter is told the terminal width
// through COLUMNS so a width-aware one (e.g. delta) can wrap; clamping is the
// backstop for one that ignores it. ok is false when the command is empty, the
// diff is too large, or the run fails or times out — callers keep the built-in.
func renderDiffExternal(argv []string, diff string, width int) (string, bool) {
	if len(argv) == 0 || diff == "" || len(diff) > diffFormatMaxBytes {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), diffFormatTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.WaitDelay = diffFormatWaitDelay
	cmd.Stdin = strings.NewReader(diff)
	if width > 0 {
		cmd.Env = append(os.Environ(), "COLUMNS="+strconv.Itoa(width))
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		diffFormatMu.Lock()
		diffFormatDisabled = true
		diffFormatMu.Unlock()
	}
	if err != nil || out.Len() == 0 {
		return "", false
	}
	return clampLines(out.String(), width), true
}

// clampLines truncates each line to width columns (ANSI-aware, tabs expanded),
// so a formatter's over-wide output cannot overflow the viewport.
func clampLines(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = clampPlain(ln, width)
	}
	return strings.Join(lines, "\n")
}

// formatToolDiff pipes a writer call's unified diff through the configured
// [cli].diff_formatter and returns its stdout as transcript rows, indented to
// the card's body column. ok is false when no formatter is configured, a
// background run is still in flight, or it fails, times out, or overshoots the
// size cap — the caller then keeps the built-in renderer.
func formatToolDiff(diff string, width int) ([]string, bool) {
	// The card indents the body by two columns, so the formatter must target
	// width-2: clamping to the full width and then indenting pushes an over-wide
	// line past the viewport, where it wraps back to column 0.
	body := width
	if width > 0 {
		body = max(width-2, 1)
	}
	out, ok := formatDiffCached(diff, body)
	if !ok {
		return nil, false
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	rows := make([]string, 0, len(lines))
	for _, ln := range lines {
		rows = append(rows, "  "+ln)
	}
	return rows, true
}

const diffFormatCacheMax = 128

// DiffKey identifies a memoized [cli].diff_formatter result: a 128-bit hash of
// the diff text and the width it was laid out to. Hashing rather than holding
// the text keeps the memo from keeping a second copy of a diff a block already
// carries, so a landed background run can still re-render only the rows that
// asked for its key without the memo paying for the input again.
type DiffKey struct {
	Hash  [2]uint64
	Width int
}

// diffKeySeeds give the two hash halves; two seeds make a 128-bit key, so two
// distinct diffs collide with probability below 2^-115 at the memo's size.
var diffKeySeeds = [2]maphash.Seed{maphash.MakeSeed(), maphash.MakeSeed()}

func diffKeyOf(content string, width int) DiffKey {
	return DiffKey{
		Hash:  [2]uint64{maphash.String(diffKeySeeds[0], content), maphash.String(diffKeySeeds[1], content)},
		Width: width,
	}
}

type diffFormatEntry struct {
	text string
	ok   bool
}

var (
	diffFormatMu       sync.Mutex
	diffFormatCache    = map[DiffKey]diffFormatEntry{}
	diffFormatOrder    []DiffKey
	diffFormatInflight = map[DiffKey]bool{}
	// diffFormatDisabled latches once a run hits diffFormatTimeout. A formatter
	// that hangs once hangs again, and each retry costs a full timeout — worst on
	// a resize, which changes the width key and so misses the per-(content,width)
	// cache. Once latched the built-in renderer is used.
	diffFormatDisabled bool
	// diffFormatNotify, when set, makes the run asynchronous and is called from
	// the formatter goroutine with the landed key once a result arrives, so the
	// frontend repaints and picks it up. Nil runs the formatter inline.
	diffFormatNotify func(DiffKey)
	// renderKeySink, when non-nil, receives every key a render consults. Set only
	// for the duration of a RenderKeys call, on the one goroutine that renders.
	renderKeySink func(DiffKey)
)

// RenderKeys runs render and returns its output with every formatter key it
// consulted, so the caller can remember which keys its rows depend on. The keys
// come back whether the memo served them or a background run was started.
func RenderKeys(render func() string) (string, []DiffKey) {
	var keys []DiffKey
	prev := renderKeySink
	renderKeySink = func(k DiffKey) { keys = append(keys, k) }
	out := render()
	renderKeySink = prev
	return out, keys
}

// HasDiffFormat reports whether the memo still holds a result for key. A
// frontend that scopes its repaint by key checks this before dropping a block's
// rows: re-rendering a block whose key the memo has since evicted would re-run
// the formatter and evict another live key, a cascade that never settles, so the
// evicted block keeps the rows it already shows.
func HasDiffFormat(key DiffKey) bool {
	diffFormatMu.Lock()
	defer diffFormatMu.Unlock()
	_, ok := diffFormatCache[key]
	return ok
}

// diffFormatLanded reports whether a formatter's successful result for
// (diff, width) is already in the memo, without starting a run. A caller uses
// it to draw a prefix whose output has landed while a longer body's run is
// still pending.
func diffFormatLanded(diff string, width int) bool {
	diffFormatMu.Lock()
	defer diffFormatMu.Unlock()
	got, hit := diffFormatCache[diffKeyOf(diff, width)]
	return hit && got.ok
}

// diffFormatPending reports whether a configured formatter's result for
// (diff, width) is still pending: the run is in flight. A caller draws a cheap
// plain diff while it is, since the colourised built-in rows would only be
// thrown away. False once the run has landed, when no formatter is set, or when
// it was disabled by a timeout.
func diffFormatPending(diff string, width int) bool {
	diffFormatMu.Lock()
	defer diffFormatMu.Unlock()
	if len(activeDiffFormatter) == 0 || diffFormatDisabled {
		return false
	}
	_, landed := diffFormatCache[diffKeyOf(diff, width)]
	return !landed
}

// formatDiffCached returns the formatter's output for (diff, width), memoized
// per key. With a re-render hook set (SetDiffFormatNotify) the run goes to a
// background goroutine and this returns ok=false at once, so the caller draws
// its placeholder rows and the frontend repaints when the result lands; without
// a hook the run is inline, so a renderer that cannot re-render still shows it.
func formatDiffCached(diff string, width int) (string, bool) {
	if len(activeDiffFormatter) == 0 {
		return "", false
	}
	key := diffKeyOf(diff, width)
	if renderKeySink != nil {
		renderKeySink(key)
	}
	diffFormatMu.Lock()
	if diffFormatDisabled {
		diffFormatMu.Unlock()
		return "", false
	}
	if got, hit := diffFormatCache[key]; hit {
		diffFormatMu.Unlock()
		return got.text, got.ok
	}
	async := diffFormatNotify != nil
	if async {
		if diffFormatInflight[key] {
			diffFormatMu.Unlock()
			return "", false
		}
		diffFormatInflight[key] = true
	}
	argv := activeDiffFormatter
	diffFormatMu.Unlock()

	if async {
		go func() {
			raw, ok := renderDiffExternal(argv, diff, width)
			if ok {
				raw = sgrOnly(raw)
			}
			diffFormatMu.Lock()
			delete(diffFormatInflight, key)
			storeDiffFormat(key, raw, ok)
			notify := diffFormatNotify
			diffFormatMu.Unlock()
			if ok && notify != nil {
				notify(key)
			}
		}()
		return "", false
	}

	raw, ok := renderDiffExternal(argv, diff, width)
	if ok {
		raw = sgrOnly(raw)
	}
	diffFormatMu.Lock()
	storeDiffFormat(key, raw, ok)
	diffFormatMu.Unlock()
	return raw, ok
}

// storeDiffFormat records a result under key, evicting the oldest entry when the
// memo is full. Callers hold diffFormatMu.
func storeDiffFormat(key DiffKey, text string, ok bool) {
	if _, seen := diffFormatCache[key]; seen {
		return
	}
	if len(diffFormatOrder) >= diffFormatCacheMax {
		delete(diffFormatCache, diffFormatOrder[0])
		diffFormatOrder = diffFormatOrder[1:]
	}
	diffFormatCache[key] = diffFormatEntry{text: text, ok: ok}
	diffFormatOrder = append(diffFormatOrder, key)
}
