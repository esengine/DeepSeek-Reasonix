package sidecar

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/ext/pluginpkg"
)

func TestCompositionExtensionRuntimeCopiesDoNotShareMutableFiles(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "entry"), []byte("original"), 0o700); err != nil {
		t.Fatal(err)
	}
	p := pluginpkg.InstalledPackage{}
	p.Package.Root = source
	first, err := privateCompositionPackages(t.TempDir(), []pluginpkg.InstalledPackage{p})
	if err != nil {
		t.Fatal(err)
	}
	second, err := privateCompositionPackages(t.TempDir(), []pluginpkg.InstalledPackage{p})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first[0].Package.Root, "entry"), []byte("session one"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{source, second[0].Package.Root} {
		data, err := os.ReadFile(filepath.Join(root, "entry"))
		if err != nil || string(data) != "original" {
			t.Fatalf("mutable runtime leaked into %s: %v", root, err)
		}
	}
}
