package sidecar

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestARuntimeThatIsNotAProgramSaysSo(t *testing.T) {
	root := testenv.TempDir(t)
	script := filepath.Join(root, "launch.sh")
	if err := os.WriteFile(script, []byte("plain text, not a program\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pkg := pluginpkg.Package{Root: root, Manifest: pluginpkg.Manifest{Runtime: &pluginpkg.RuntimeSpec{Command: "${REASONIX_PLUGIN_ROOT}/launch.sh"}}}
	_, err := startProcess(pkg, pluginpkg.InstalledPlugin{Name: "demo"})
	if !errors.Is(err, ErrRuntimeNotExecutable) {
		t.Fatalf("startProcess error = %v, want ErrRuntimeNotExecutable", err)
	}
	if !strings.Contains(err.Error(), "native executable") {
		t.Fatalf("error %q does not tell the author what to ship", err)
	}
}
