package cli

import (
	"fmt"
	"os"

	"reasonix/internal/base/i18n"
)

// readsLanguageFromConfig says whether the command may read configuration for
// the UI language before it runs. The scheduled-run child reads no file of the
// current directory at all.
func readsLanguageFromConfig(cmd string, doctorRepair bool) bool {
	return !doctorRepair && cmd != "schedule"
}

// unknownCommand is the fallthrough of the command switch: the internal commands
// that are not listed in usage, then the usage error.
func unknownCommand(cmd string, rest []string) int {
	if cmd == "schedule" {
		return scheduleCommand(rest)
	}
	fmt.Fprintf(os.Stderr, i18n.M.UnknownCommandFmt+"\n\n", cmd)
	usage()
	return 2
}

// scheduleCommand dispatches `reasonix schedule`. Only the supervisor's own
// child entry exists here; it is not a command a person types.
func scheduleCommand(args []string) int {
	if len(args) > 0 && args[0] == "exec" {
		return scheduleExec(args[1:])
	}
	fmt.Fprintln(os.Stderr, "usage: reasonix schedule exec <trigger-id>   (started by the scheduler, not by hand)")
	return 2
}
