//go:build darwin

package update

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Which bundle an update replaces, and whether it is a real one. Separate from
// the handoff because they answer different questions: this file decides what
// is being swapped, apply_darwin.go performs the swap.

// The two ways a stated application can be unusable. They are sentinels because
// they are told apart: "nobody said" is a shell that has not been taught to
// state one, and "that is not an app" is a shell that stated the wrong path.
var (
	ErrApplicationNotStated = errors.New("update: this shell did not state which application is being replaced")
	ErrApplicationInvalid   = errors.New("update: stated app bundle is not one")
)

// resolve names the bundle to swap, refusing rather than falling back to this
// process: a shell that stated no application is one whose process may not be
// it, and swapping whatever directory this binary sits in is how an update
// lands on the wrong thing with every later check still passing.
func (a Application) resolve() (string, error) {
	if a.Bundle == "" || a.PID <= 0 {
		return "", ErrApplicationNotStated
	}
	if _, err := os.Stat(filepath.Join(a.Bundle, "Contents", "Info.plist")); err != nil {
		return "", fmt.Errorf("%w: %w", ErrApplicationInvalid, err)
	}
	return a.Bundle, nil
}

// ApplicationAt states an application neither half of which this process can
// answer for: the executable belongs to the shell around it, and the process
// holding the bundle open is that shell's rather than this one's.
func ApplicationAt(exe string, pid int) (Application, error) {
	bundle, err := MacAppBundle(exe)
	if err != nil {
		return Application{}, err
	}
	return Application{Bundle: bundle, PID: pid}, nil
}

// MacAppBundle resolves the .app an executable runs from. It takes the path
// rather than reading os.Executable() so the answer follows the install a shell
// states; two ways of finding the bundle is two ways of being wrong about which
// one is being replaced.
func MacAppBundle(exe string) (string, error) {
	if strings.TrimSpace(exe) == "" {
		return "", fmt.Errorf("update: no executable to resolve a bundle from")
	}
	exe, _ = filepath.EvalSymlinks(exe)
	const marker = ".app/Contents/MacOS/"
	idx := strings.Index(exe, marker)
	if idx < 0 {
		return "", fmt.Errorf("update: current executable is not inside a macOS .app bundle")
	}
	app := exe[:idx+len(".app")]
	if _, err := os.Stat(filepath.Join(app, "Contents", "Info.plist")); err != nil {
		return "", fmt.Errorf("update: current app bundle is invalid: %w", err)
	}
	return app, nil
}

func findMacApp(root string) (string, error) {
	direct := filepath.Join(root, "Reasonix.app")
	if _, err := os.Stat(filepath.Join(direct, "Contents", "Info.plist")); err == nil {
		return direct, nil
	}
	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return err
		}
		if d.IsDir() && strings.HasSuffix(path, ".app") {
			if _, statErr := os.Stat(filepath.Join(path, "Contents", "Info.plist")); statErr == nil {
				found = path
				return filepath.SkipDir
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("update: no .app bundle found in macOS update archive")
	}
	return found, nil
}

func verifyMacApp(appPath, bundleID string) error {
	info := filepath.Join(appPath, "Contents", "Info.plist")
	out, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Print :CFBundleIdentifier", info).Output()
	if err != nil {
		return fmt.Errorf("read macOS bundle identifier: %w", err)
	}
	if got := strings.TrimSpace(string(out)); got != bundleID {
		return fmt.Errorf("update: bundle identifier %q does not match %q", got, bundleID)
	}
	if err := exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", appPath).Run(); err != nil {
		return fmt.Errorf("verify macOS code signature: %w", err)
	}
	if err := exec.Command("/usr/sbin/spctl", "--assess", "--type", "execute", appPath).Run(); err != nil {
		return fmt.Errorf("assess macOS notarization: %w", err)
	}
	return nil
}
