package cli

import (
	"os"

	"golang.org/x/term"
)

// colorEnabled is decided once at startup: only colorize when writing to a real
// terminal and the user hasn't opted out via NO_COLOR (https://no-color.org) or
// a dumb TERM. Piped/redirected output and CI stay plain so scripts aren't broken.
var colorEnabled = detectColor()

func detectColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}

const (
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
	ansiDim     = "\033[2m"
	ansiGreen   = "\033[32m"
	ansiRed     = "\033[31m"
	ansiYellow  = "\033[33m"
	ansiBlue    = "\033[38;5;39m"
	ansiCyan    = "\033[38;5;44m"
	ansiMagenta = "\033[38;5;176m"
	ansiReverse = "\033[7m"
)

// ansiAccent is reasonix's current accent colour. The CLI /theme command updates
// it at runtime so titles, prompts, and box borders switch together.
var ansiAccent = "\033[38;5;173m"

func sgr(code, s string) string {
	if !colorEnabled {
		return s
	}
	return code + s + ansiReset
}

func bold(s string) string    { return sgr(ansiBold, s) }
func dim(s string) string     { return sgr(ansiDim, s) }
func green(s string) string   { return sgr(ansiGreen, s) }
func red(s string) string     { return sgr(ansiRed, s) }
func yellow(s string) string  { return sgr(ansiYellow, s) }
func accent(s string) string  { return sgr(ansiAccent, s) }
func reverse(s string) string { return sgr(ansiReverse, s) }
