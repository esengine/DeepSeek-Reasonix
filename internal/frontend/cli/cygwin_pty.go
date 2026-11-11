package cli

import (
	"os"
	"regexp"

	"reasonix/internal/base/i18n"
)

// A Cygwin or MSYS terminal, Git Bash's mintty among them, hands a Windows
// program named pipes instead of a console, so no console call works on it.
// The runtime names them this way; MSYS2 adds "-nat" to those it opens for
// native programs.
var cygwinPty = regexp.MustCompile(`^\\(?:cygwin|msys)-[0-9a-f]{16}-pty[0-9]+-(?:from|to)-master(?:-nat)?$`)

func cygwinPtyName(name string) bool { return cygwinPty.MatchString(name) }

// minttyHint is what to say instead of the usage text when either end of the
// session is a Cygwin or MSYS terminal, and "" when neither is.
func minttyHint(in, out *os.File, on func(*os.File) bool) string {
	if on(in) || on(out) {
		return i18n.M.MinttyConsoleHint
	}
	return ""
}
