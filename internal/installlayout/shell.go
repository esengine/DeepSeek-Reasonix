package installlayout

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// AppShellDirName is the tree member inside versions/<version>/ that holds the
// Electron shell bundle beside the Go desktop binary.
const AppShellDirName = "app"

// ShellExecutableName is the shell executable base name under app/.
func ShellExecutableName() string { return ShellExecutableNameFor(runtime.GOOS) }

// ShellExecutableNameFor returns the shell executable base name for goos.
func ShellExecutableNameFor(goos string) string {
	switch goos {
	case "windows":
		return "Reasonix.exe"
	case "darwin":
		return "Reasonix"
	default:
		return "Reasonix"
	}
}

// ShellMembers enumerates a complete shell tree without following symlinks.
// Legacy flat releases may omit it; a present tree must contain its entry and
// renderer resources before any version pointer can be committed.
func ShellMembers(root, goos string) ([]Member, error) {
	tree := filepath.Join(root, AppShellDirName)
	if _, err := os.Lstat(tree); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var members []Member
	seen := map[string]bool{}
	err := filepath.WalkDir(tree, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("shell tree contains symlink: %s", p)
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("shell tree member is not regular: %s", p)
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if err := ValidateMemberName(name); err != nil {
			return err
		}
		members = append(members, Member{Name: name, Path: p, Mode: info.Mode().Perm()})
		seen[name] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, name := range ShellRequiredNames(goos) {
		if !seen[name] {
			return nil, fmt.Errorf("shell tree missing %s", name)
		}
	}
	return members, nil
}

func ShellRequiredNames(goos string) []string {
	return []string{"app/" + ShellExecutableNameFor(goos), "app/resources/app.asar", "app/resources/app/index.html", "app/resources/build.json"}
}
