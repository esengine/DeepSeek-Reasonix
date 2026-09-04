package update

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// hostMains are the mains that assemble a Studio. Declared rather than
// discovered: nothing about a main's shape says it is a host's, and the one
// this list exists for was missed by a reader who had the comment in front of
// them. A new shell adds its entry here, which is the point.
var hostMains = []string{
	"cmd/reasonix-studio-host/main.go",
	"desktop/next/main.go",
}

// A contract kept by a sentence until it was not: the Electron host shipped
// without the call, its macOS update child exited on a flag that host never
// defined, and the parent read EOF off a readiness pipe. Unfixable by updating
// — the process that starts the helper is the one already installed.
func TestEveryHostMainAnswersTheMacHandoff(t *testing.T) {
	for _, rel := range hostMains {
		body, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		if !bytes.Contains(body, []byte("MaybeRunMacHandoff")) {
			t.Errorf("%s never calls update.MaybeRunMacHandoff: its macOS update "+
				"child would reach this host's own flag parsing and exit there", rel)
		}
	}
}
