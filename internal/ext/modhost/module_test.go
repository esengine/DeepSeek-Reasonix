package modhost

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func packageWith(t *testing.T, hooks string, files map[string]string) pluginpkg.InstalledPackage {
	t.Helper()
	root := testenv.TempDir(t)
	if hooks != "" {
		if err := os.MkdirAll(filepath.Join(root, "hooks"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "hooks", "hooks.json"), []byte(hooks), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return pluginpkg.InstalledPackage{Installed: pluginpkg.InstalledPlugin{Name: "demo"}, Package: pluginpkg.Package{Root: root}}
}

func TestAPackageWithAModulesListIsAMod(t *testing.T) {
	pkg := packageWith(t, `{"modules":["./register.js"]}`, map[string]string{"hooks/register.js": "export function register(){}"})
	mods, bad := Discover([]pluginpkg.InstalledPackage{pkg, packageWith(t, `{"hooks":{}}`, nil)})
	if len(bad) != 0 || len(mods) != 1 || mods[0].Plugin != "demo" || mods[0].Rel != "hooks/register.js" {
		t.Fatalf("mods = %+v, bad = %v", mods, bad)
	}
}

func TestAModulePathThatLeavesThePackageOrIsNotAFileIsRefused(t *testing.T) {
	outside := filepath.Join(testenv.TempDir(t), "evil.js")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, hooks := range map[string]string{
		"parent":    `{"modules":["../../../etc/evil.js"]}`,
		"absolute":  `{"modules":["` + filepath.ToSlash(outside) + `"]}`,
		"two paths": `{"modules":["./a.js","./b.js"]}`,
		"empty":     `{"modules":[""]}`,
		"directory": `{"modules":["./dir"]}`,
		"missing":   `{"modules":["./nope.js"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			pkg := packageWith(t, hooks, map[string]string{"hooks/dir/x.js": "x"})
			mods, bad := Discover([]pluginpkg.InstalledPackage{pkg})
			if len(mods) != 0 || !errors.Is(bad["demo"], ErrBadModule) {
				t.Fatalf("mods = %+v, bad = %v; want refused", mods, bad)
			}
		})
	}
	t.Run("symlink out", func(t *testing.T) {
		pkg := packageWith(t, `{"modules":["./link.js"]}`, nil)
		if err := os.Symlink(outside, filepath.Join(pkg.Package.Root, "hooks", "link.js")); err != nil {
			t.Skip("no symlinks here:", err)
		}
		mods, bad := Discover([]pluginpkg.InstalledPackage{pkg})
		if len(mods) != 0 || !errors.Is(bad["demo"], ErrBadModule) {
			t.Fatalf("mods = %+v, bad = %v; a link out of the package must be refused", mods, bad)
		}
	})
}
