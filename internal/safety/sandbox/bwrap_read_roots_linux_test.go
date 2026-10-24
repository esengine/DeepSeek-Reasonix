//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestBwrapExplicitReadRootsPreservePrivateTemporaryBoundary(t *testing.T) {
	requireConfinedGit(t)
	base := realTempDir(t)
	workspace, metadata, secret := filepath.Join(base, "workspace"), filepath.Join(base, "metadata"), filepath.Join(base, "unrelated")
	for _, dir := range []string{workspace, metadata, secret} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(metadata, "marker")
	if err := os.WriteFile(marker, []byte("metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	spec := Spec{Mode: "enforce", RequiredIntegrity: CompleteFileIntegrity, MinimalWrites: true, WriteRoots: []string{workspace}, ReadRoots: []string{metadata}}
	command := fmt.Sprintf("cat '%s'; test ! -e '%s' && ! (printf overwritten > '%s')", marker, secret, marker)
	if out, err := runConfined(t, spec, workspace, command); err != nil {
		t.Fatalf("declared metadata unavailable or writable, or unrelated temp exposed: %s %v", out, err)
	}
	if raw, err := os.ReadFile(marker); err != nil || string(raw) != "metadata" {
		t.Fatalf("read grant changed metadata: %q %v", raw, err)
	}
	spec.ForbidReadRoots = []string{metadata}
	if out, err := runConfined(t, spec, workspace, fmt.Sprintf("cat '%s'", marker)); err == nil {
		t.Fatalf("read grant overrode an explicit denial: %s", out)
	}
}
