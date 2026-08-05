//go:build darwin

package repair

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlatformRepairPathCaseInsensitiveMatchesDirectoryLookup(t *testing.T) {
	root := t.TempDir()
	exact := filepath.Join(root, "CaseProbe")
	if err := os.WriteFile(exact, []byte("probe"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, lookupErr := os.Stat(filepath.Join(root, "caseprobe"))
	want := lookupErr == nil
	if got := platformRepairPathCaseInsensitive(exact); got != want {
		t.Fatalf("case-insensitive detection = %v, directory lookup = %v", got, want)
	}
}

func TestCanonicalRepairPathConvergesUnicodeAliases(t *testing.T) {
	root := t.TempDir()
	nfcDir := filepath.Join(root, "Caf\u00e9")
	nfdDir := filepath.Join(root, "Cafe\u0301")
	if err := os.Mkdir(nfcDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(nfdDir); err != nil {
		t.Skipf("filesystem keeps NFC and NFD names distinct: %v", err)
	}
	nfcTarget := filepath.Join(nfcDir, "state.json")
	nfdTarget := filepath.Join(nfdDir, "state.json")
	if got, want := canonicalRepairPath(nfdTarget), canonicalRepairPath(nfcTarget); got != want {
		t.Fatalf("Unicode aliases use different repair keys:\nNFD: %q\nNFC: %q", got, want)
	}
}
