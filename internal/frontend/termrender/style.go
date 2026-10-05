package termrender

import (
	"os"

	"github.com/charmbracelet/colorprofile"
)

// activeColorProfile is the single description of what the terminal can render.
// colorprofile owns the NO_COLOR, CLICOLOR_FORCE, TERM=dumb and isatty rules,
// so piped output stays plain. Colour fidelity is derived from it, never probed
// a second time.
var activeColorProfile = detectColorProfile(ANSIConsoleReady())

func detectColorProfile(ansiReady bool) colorprofile.Profile {
	if !ansiReady {
		return colorprofile.ASCII
	}
	return colorprofile.Detect(os.Stdout, os.Environ())
}

// SetColorProfile replaces the terminal's detected rendering profile and
// returns the one it replaced, for a caller that knows the output better.
func SetColorProfile(p colorprofile.Profile) colorprofile.Profile {
	prev := activeColorProfile
	activeColorProfile = p
	return prev
}

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

func Bold(s string) string    { return sgr(ansiBold, s) }
func Dim(s string) string     { return ThemeFg(activeTheme.Faint, s) }
func Green(s string) string   { return ThemeFg(activeTheme.Success, s) }
func Red(s string) string     { return ThemeFg(activeTheme.Err, s) }
func Yellow(s string) string  { return ThemeFg(activeTheme.Warn, s) }
func Accent(s string) string  { return ThemeFg(activeTheme.Accent, s) }
func Reverse(s string) string { return sgr(ansiReverse, s) }
