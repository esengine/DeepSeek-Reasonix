package cli

import (
	"bufio"
	"fmt"
	"os"

	"reasonix/internal/base/i18n"
)

// bareUsage answers argv with no terminal to chat in. A bare `reasonix` asked
// for nothing wrong and exits 0, as in 1.x; session options with no session to
// apply them to are a usage error. A console this process owns alone was
// opened by a double-click, so it points at Studio and waits instead of closing.
func bareUsage(bare bool) int {
	configureThemeForTTYOutput()
	usage()
	if ownsConsoleAlone() {
		fmt.Print("\n" + i18n.M.StandaloneConsoleHint)
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	if bare {
		return 0
	}
	return 2
}

// bareOutsideConsole answers a bare invocation that has no console: a Cygwin
// or MSYS terminal is told why the UI cannot run there, anything else gets the
// usage text.
func bareOutsideConsole(bare bool) int {
	if hint := minttyHint(os.Stdin, os.Stdout, onCygwinPty); hint != "" {
		fmt.Fprint(os.Stderr, hint)
		return 2
	}
	return bareUsage(bare)
}
