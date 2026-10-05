package cli

import (
	"fmt"
	"io"

	"strings"
)

// setupArgsVerdict answers -h/--help and rejects any other dashed argument
// before resolveSetupTargets, which takes every unrecognized argument as the
// config path. ok is false when the returned code is final.
func setupArgsVerdict(args []string, stdout, stderr io.Writer) (code int, ok bool) {
	if commandHelpRequested(args, len(args)) {
		setupUsage(stdout)
		return 0, false
	}
	for _, a := range args {
		if a == "--local" || a == "-l" {
			continue
		}
		if strings.HasPrefix(a, "-") {
			fmt.Fprintf(stderr, "unknown setup flag %q\n\n", a)
			setupUsage(stderr)
			return 2, false
		}
	}
	return 0, true
}

func setupUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: reasonix setup [--local|-l] [path]")
	fmt.Fprintln(w, "Interactive configuration wizard. Writes a reasonix config and stores the")
	fmt.Fprintln(w, "provider API key in Reasonix's credential file, never in the config itself.")
	fmt.Fprintln(w, "  --local, -l   write ./reasonix.toml instead of the user-global config")
	fmt.Fprintln(w, "  path          write the config to this path instead of the default")
}
