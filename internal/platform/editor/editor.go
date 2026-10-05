// Package editor opens a workspace in the code editor a machine already has.
// It never installs one, and it never guesses past the families it knows: an
// editor nobody can name is reported as missing rather than launched blind.
package editor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrNoEditor is refused when this machine has none of the editors below and
// nothing was configured. Callers tell it apart from a launch that failed:
// one asks the person to install or configure an editor, the other does not.
var ErrNoEditor = errors.New("no code editor was found on this machine")

// Spec is the editor a workspace will open in. Name is what a surface says so
// the person knows which one it found, because a machine with several has no
// obvious answer and "opened in your editor" is not one.
type Spec struct {
	Name       string
	Executable string
}

// family is one editor this package knows how to find: how it is spelled on
// PATH, and where its installer puts it when PATH has nothing.
type family struct {
	name    string
	command string
	windows []string
	darwin  []string
}

// VS Code first because it is what most of these are forks of, then the forks
// in the order they are likely to be somebody's only editor.
var families = []family{
	{
		name:    "Visual Studio Code",
		command: "code",
		windows: []string{`Programs\Microsoft VS Code\Code.exe`},
		darwin:  []string{"Visual Studio Code.app/Contents/Resources/app/bin/code"},
	},
	{
		name:    "VS Code Insiders",
		command: "code-insiders",
		windows: []string{`Programs\Microsoft VS Code Insiders\Code - Insiders.exe`},
		darwin:  []string{"Visual Studio Code - Insiders.app/Contents/Resources/app/bin/code"},
	},
	{
		name:    "Cursor",
		command: "cursor",
		windows: []string{`Programs\cursor\Cursor.exe`},
		darwin:  []string{"Cursor.app/Contents/Resources/app/bin/cursor"},
	},
	{
		name:    "Windsurf",
		command: "windsurf",
		windows: []string{`Programs\Windsurf\Windsurf.exe`},
		darwin:  []string{"Windsurf.app/Contents/Resources/app/bin/windsurf"},
	},
	{
		name:    "VSCodium",
		command: "codium",
		windows: []string{`Programs\VSCodium\VSCodium.exe`},
		darwin:  []string{"VSCodium.app/Contents/Resources/app/bin/codium"},
	},
}

// Discover answers which editor this machine will use. configured is taken as
// given and is the only candidate, so a person who names one is never
// second-guessed by a search that finds something else first.
func Discover(configured string) (Spec, error) {
	return discover(configured, systemHost())
}

// host is what discovery reads of the machine it runs on.
type host struct {
	goos   string
	getenv func(string) string
	// applications is the system-wide folder macOS installs app bundles into.
	applications string
}

func systemHost() host {
	return host{goos: runtime.GOOS, getenv: os.Getenv, applications: "/Applications"}
}

func discover(configured string, h host) (Spec, error) {
	if c := strings.TrimSpace(configured); c != "" {
		if path, err := resolve(c); err == nil {
			return Spec{Name: filepath.Base(path), Executable: path}, nil
		}
		return Spec{}, fmt.Errorf("%w: the configured editor %q is not an executable on this machine", ErrNoEditor, c)
	}
	for _, f := range families {
		if path, ok := f.find(h); ok {
			return Spec{Name: f.name, Executable: path}, nil
		}
	}
	return Spec{}, ErrNoEditor
}

func (f family) find(h host) (string, bool) {
	for _, candidate := range f.installed(h) {
		if isExecutableFile(candidate) {
			return candidate, true
		}
	}
	// PATH last on Windows: `code` there is a .cmd shim, and launching the
	// executable the installer placed avoids handing a path to a shell.
	if path, err := exec.LookPath(f.command); err == nil {
		return path, true
	}
	return "", false
}

func (f family) installed(h host) []string {
	getenv := h.getenv
	switch h.goos {
	case "windows":
		var out []string
		for _, root := range []string{getenv("LOCALAPPDATA"), getenv("ProgramFiles")} {
			if root == "" {
				continue
			}
			for _, rel := range f.windows {
				out = append(out, filepath.Join(root, rel))
			}
		}
		return out
	case "darwin":
		var out []string
		for _, rel := range f.darwin {
			out = append(out, filepath.Join(h.applications, rel))
			if home := getenv("HOME"); home != "" {
				out = append(out, filepath.Join(home, "Applications", rel))
			}
		}
		return out
	default:
		return nil
	}
}

func resolve(configured string) (string, error) {
	if filepath.IsAbs(configured) {
		if isExecutableFile(configured) {
			return configured, nil
		}
		return "", ErrNoEditor
	}
	return exec.LookPath(configured)
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// Open launches spec on dir and returns once the process has started. It never
// waits: an editor is a window somebody is about to use, not a step in this
// turn, and holding the request until it exits would hold it for the session.
func Open(spec Spec, dir string) error {
	if strings.TrimSpace(spec.Executable) == "" {
		return ErrNoEditor
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("workspace %q is not a directory this machine can open", dir)
	}
	cmd := exec.Command(spec.Executable, dir)
	// Not the workspace: an editor that inherits it holds a handle on the
	// directory, which is what stops Windows renaming or deleting it later.
	cmd.Dir = os.TempDir()
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reaped rather than left as a zombie; the editor outlives this wait.
	go func() { _ = cmd.Wait() }()
	return nil
}
