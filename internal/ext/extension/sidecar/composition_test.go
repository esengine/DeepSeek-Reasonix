package sidecar

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestCompositionExtensionRuntimeCopiesDoNotShareMutableFiles(t *testing.T) {
	source := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(source, "entry"), []byte("original"), 0o700); err != nil {
		t.Fatal(err)
	}
	p := pluginpkg.InstalledPackage{}
	p.Package.Root = source
	first, err := privateCompositionPackages(testenv.TempDir(t), []pluginpkg.InstalledPackage{p})
	if err != nil {
		t.Fatal(err)
	}
	second, err := privateCompositionPackages(testenv.TempDir(t), []pluginpkg.InstalledPackage{p})
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
