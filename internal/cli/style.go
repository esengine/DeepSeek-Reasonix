package cli

import (
	"os"

	"github.com/charmbracelet/colorprofile"
)

// activeColorProfile is the single description of what the terminal can render.
// colorprofile owns the NO_COLOR, CLICOLOR_FORCE, TERM=dumb and isatty rules,
// so piped output stays plain. Colour fidelity is derived from it, never probed
// a second time.
var activeColorProfile = colorprofile.Detect(os.Stdout, os.Environ())

func colorOn() bool {
	return activeColorProfile > colorprofile.ASCII
}

func trueColorTerminal() bool {
	return activeColorProfile == colorprofile.TrueColor
}

const (
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
	ansiReverse = "\033[7m"
	// ansiAccent is the dark graphite accent as a literal escape, for tests that
	// pin the concrete sequence instead of the active theme.
	ansiAccent = "\033[38;5;173m"
)

func sgr(code, s string) string {
	if !colorOn() {
		return s
	}
	return code + s + ansiReset
}

func bold(s string) string    { return sgr(ansiBold, s) }
func dim(s string) string     { return themeFg(activeCLITheme.faint, s) }
func green(s string) string   { return themeFg(activeCLITheme.success, s) }
func red(s string) string     { return themeFg(activeCLITheme.err, s) }
func yellow(s string) string  { return themeFg(activeCLITheme.warn, s) }
func accent(s string) string  { return themeFg(activeCLITheme.accent, s) }
func reverse(s string) string { return sgr(ansiReverse, s) }
