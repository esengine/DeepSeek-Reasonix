package builtin

import (
	"strings"

	"reasonix/internal/safety/sandbox"
)

var writeRootDropReason = map[string]string{
	sandbox.WriteRootChangedCode:    "no longer is the directory the session started with: it was replaced, re-pointed or removed",
	sandbox.WriteRootRedirectedCode: "resolves through a link inside a writable directory, where a confined command can re-point it",
}

// writeRootNote is the host's account of configured write roots this confined
// command did not get. Without it a refused write reads as a plain
// permission error the model has to invent a cause for.
func (b bash) writeRootNote(wrapped bool) string {
	if !wrapped {
		return ""
	}
	var notes []string
	for _, d := range sandbox.DroppedWriteRoots(b.sb) {
		notes = append(notes, "[host] "+d.Code+": "+d.Root+" is not a write root for this command; it "+writeRootDropReason[d.Code]+
			". Paths under it are writable only where another write root covers them, and writes never follow a link out. The user has to restore it and start a new session to write there.")
	}
	return strings.Join(notes, "\n")
}
