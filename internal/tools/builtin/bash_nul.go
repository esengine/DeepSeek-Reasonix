// bash_nul.go — the file Git Bash leaves when a Windows habit meets an MSYS path.
package builtin

import (
	"os"
	"strings"

	"reasonix/internal/safety/sandbox"
)

// nulWatch notices a regular file named NUL that a Git Bash command created in
// its working directory. MSYS programs take NUL as an ordinary path, so output
// meant to be discarded lands in a file the host can name the cause of.
type nulWatch struct {
	dir   string
	armed bool
}

// armNULWatch arms only on Windows under bash, and only when no NUL file is
// there already: one the command did not create is not its to report.
func armNULWatch(goos string, sh sandbox.Shell, dir string) nulWatch {
	if goos != "windows" || sh.Kind != sandbox.ShellBash || dir == "" || nulFileIn(dir) {
		return nulWatch{}
	}
	return nulWatch{dir: dir, armed: true}
}

func (w nulWatch) note() string {
	if !w.armed || !nulFileIn(w.dir) {
		return ""
	}
	return "This command created a file named NUL in " + w.dir + ": Git Bash programs take NUL as an ordinary path " +
		"(ssh -o UserKnownHostsFile=NUL, curl -o NUL), so nothing was discarded. Remove it with rm ./NUL and use /dev/null instead."
}

// nulFileIn reads the directory rather than stat-ing the name: every Windows
// path API resolves NUL to the device, so a stat answers for the device.
func nulFileIn(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), "NUL") && e.Type().IsRegular() {
			return true
		}
	}
	return false
}
