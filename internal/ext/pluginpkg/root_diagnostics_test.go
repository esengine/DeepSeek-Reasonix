package pluginpkg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestManifestWarnsWhenContributionRootsNameFiles(t *testing.T) {
	for _, kind := range []string{"skills", "agents", "commands", "prompts"} {
		t.Run(kind, func(t *testing.T) {
			root := testenv.TempDir(t)
			rel := kind + "/sample.md"
			writeV2Plugin(t, root, fmt.Sprintf(`{"apiVersion":"reasonix.io/plugin/v2","name":"root-warning-kit","contributes":{%q:[%q]}}`, kind, rel))
			writeTestFile(t, filepath.Join(root, filepath.FromSlash(rel)), "---\nname: sample\ndescription: A sample\n---\nSample body")
			pkg, warnings, err := ParseDir(root)
			if err != nil {
				t.Fatalf("file contribution changed manifest acceptance: %v", err)
			}
			if len(warnings) != 1 || !strings.Contains(warnings[0], kind+" path \""+rel+"\" is not a directory") {
				t.Errorf("warnings=%v, want the named directory diagnostic", warnings)
			}
			if pkg.Manifest.Name != "root-warning-kit" {
				t.Fatal("warning changed the package identity")
			}
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(root, filepath.FromSlash(rel)), 0o755); err != nil {
				t.Fatal(err)
			}
			if _, corrected, err := ParseDir(root); err != nil || len(corrected) != 0 {
				t.Fatalf("actual directory warnings=%v, err=%v", corrected, err)
			}
		})
	}
}

func TestManifestDirectoryRootWarningsFollowInternalAliases(t *testing.T) {
	root := testenv.TempDir(t)
	writeV2Plugin(t, root, `{"apiVersion":"reasonix.io/plugin/v2","name":"root-warning-kit","contributes":{"commands":["file-alias"],"prompts":["dir-alias"]}}`)
	writeTestFile(t, filepath.Join(root, "real-dir", "sample.md"), "Sample body")
	for _, link := range []struct{ target, name string }{
		{filepath.Join(root, "real-dir", "sample.md"), "file-alias"},
		{filepath.Join(root, "real-dir"), "dir-alias"},
	} {
		if err := os.Symlink(link.target, filepath.Join(root, link.name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	pkg, warnings, err := ParseDir(root)
	if err != nil || len(warnings) != 1 || !strings.Contains(warnings[0], `commands path "file-alias" is not a directory`) {
		t.Fatalf("alias warnings=%v, err=%v", warnings, err)
	}
	if pkg.PromptCount() != 1 {
		t.Fatal("the valid directory alias stopped contributing its prompt")
	}
}
