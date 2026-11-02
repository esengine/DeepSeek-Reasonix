package openwith

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	// ErrUnknown is a target id the table does not declare.
	ErrUnknown = errors.New("openwith: unknown target")
	// ErrUnavailable is a declared target none of whose executables is installed.
	ErrUnavailable = errors.New("openwith: that program is not installed")
	// ErrBadFolder is a folder that is not an absolute path to a directory.
	ErrBadFolder = errors.New("openwith: not an absolute folder on this machine")
)

// Status is one declared target and whether this machine has it.
type Status struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
	// Why and Within say what kept a program that is installed from being
	// offered: it sits inside a project folder, which is named in Within.
	Why    string `json:"why,omitempty"`
	Within string `json:"within,omitempty"`
}

// WhyInsideProject marks a program that exists but lies in a project folder, so
// whatever an agent can write there could be started from the list.
const WhyInsideProject = "inside_project"

// machine is what discovery and launching read of the computer.
type machine struct {
	goos   string
	getenv func(string) string
	glob   func(string) ([]string, error)
	isFile func(string) bool
	isDir  func(string) bool
	// eval resolves symlinks; a path it cannot resolve is not launchable.
	eval  func(string) (string, error)
	start func(exe string, args []string) error
	// folders are directories no launched program may come from.
	forbidden []string
}

func system(getenv func(string) string) machine {
	return machine{
		goos: runtime.GOOS, getenv: getenv, glob: filepath.Glob,
		isFile: func(p string) bool { i, err := os.Stat(p); return err == nil && !i.IsDir() },
		isDir:  func(p string) bool { i, err := os.Stat(p); return err == nil && i.IsDir() },
		eval:   filepath.EvalSymlinks,
		start:  startDetached,
	}
}

// List says which declared targets this machine has. getenv is the caller's
// view of the environment, and forbidden names project folders whose contents
// may not be launched.
func List(getenv func(string) string, forbidden []string) []Status {
	m := system(getenv)
	m.forbidden = forbidden
	return m.list()
}

// Open starts target id on dir, which must be an absolute folder. It returns
// the target's name once the process has started and does not wait for it.
func Open(getenv func(string) string, id, dir string, forbidden []string) (string, error) {
	m := system(getenv)
	m.forbidden = forbidden
	return m.open(id, dir)
}

func (m machine) list() []Status {
	out := make([]Status, 0, len(targets))
	for _, t := range targets {
		_, ok := m.resolve(t, "")
		st := Status{ID: t.id, Name: t.name, Available: ok}
		if !ok {
			st.Why, st.Within = m.whyNot(t)
		}
		out = append(out, st)
	}
	return out
}

// whyNot names the project folder holding a program the target would have used
// had it been elsewhere; a program that is simply not installed has no reason.
func (m machine) whyNot(t target) (why, within string) {
	for _, r := range t.by[m.goos] {
		for _, c := range m.candidates(r) {
			if !m.absolute(c.path) || (!c.bundle && !m.isFile(c.path)) {
				continue
			}
			if m.goos == "windows" && !strings.EqualFold(filepath.Ext(c.path), ".exe") {
				continue
			}
			if root := m.containing(c.path); root != "" {
				return WhyInsideProject, root
			}
		}
	}
	return "", ""
}

func (m machine) open(id, dir string) (string, error) {
	var t target
	found := false
	for _, c := range targets {
		if c.id == id {
			t, found = c, true
			break
		}
	}
	if !found {
		return "", ErrUnknown
	}
	if !m.absolute(dir) || !m.isDir(dir) {
		return "", ErrBadFolder
	}
	call, ok := m.resolve(t, dir)
	if !ok {
		return "", ErrUnavailable
	}
	if err := m.start(call.exe, call.args); err != nil {
		return "", fmt.Errorf("openwith: starting %s: %w", t.name, err)
	}
	return t.name, nil
}

// call is a program and its argument list, the folder always one element.
type call struct {
	exe  string
	args []string
}

// absolute accepts the paths this operating system means by absolute. On
// Windows only a drive-letter path passes: a share or a device namespace is
// reached just by being named.
func (m machine) absolute(p string) bool {
	if p == "" || strings.ContainsRune(p, 0) {
		return false
	}
	if m.goos == "windows" {
		return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') &&
			((p[0] >= 'A' && p[0] <= 'Z') || (p[0] >= 'a' && p[0] <= 'z'))
	}
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//")
}

// resolve finds the first declared way of starting t that is installed, and
// the call that starts it on dir.
func (m machine) resolve(t target, dir string) (call, bool) {
	for _, r := range t.by[m.goos] {
		for _, c := range m.candidates(r) {
			if !m.launchable(c) {
				continue
			}
			if c.bundle {
				open := "/usr/bin/open"
				if !m.isFile(open) {
					continue
				}
				return call{exe: open, args: append(append([]string{"-a", c.path}, r.lead...), r.glue+dir)}, true
			}
			return call{exe: c.path, args: append(append([]string{}, r.lead...), r.glue+dir)}, true
		}
	}
	return call{}, false
}

type candidate struct {
	path   string
	bundle bool
}

func (m machine) candidates(r recipe) []candidate {
	var out []candidate
	for _, pattern := range r.exes {
		expanded := m.expand(pattern)
		if expanded == "" {
			continue
		}
		matches, err := m.glob(expanded)
		if err != nil {
			continue
		}
		for _, p := range matches {
			out = append(out, candidate{path: p})
		}
	}
	if m.goos == "darwin" {
		for _, app := range r.apps {
			for _, root := range []string{"/Applications", filepath.Join(m.getenv("HOME"), "Applications")} {
				if bundle := filepath.Join(root, app+".app"); m.isDir(bundle) {
					out = append(out, candidate{path: bundle, bundle: true})
				}
			}
		}
	}
	for _, name := range r.commands {
		out = append(out, m.onPath(name)...)
	}
	return out
}

// onPath is every place on PATH holding name, in PATH order. The whole list,
// not the first hit: a project directory sitting early on PATH must not hide the
// installed program behind it. A relative entry is skipped, so the working
// directory is never searched.
func (m machine) onPath(name string) []candidate {
	var out []candidate
	for _, dir := range filepath.SplitList(m.getenv("PATH")) {
		if !m.absolute(dir) {
			continue
		}
		file := filepath.Join(dir, name)
		if m.goos == "windows" {
			file += ".exe"
		}
		out = append(out, candidate{path: file})
	}
	return out
}

// expand fills %NAME% from the environment; a pattern naming a variable this
// machine has not set is dropped rather than searched from the wrong place.
func (m machine) expand(pattern string) string {
	for {
		i := strings.IndexByte(pattern, '%')
		if i < 0 {
			return pattern
		}
		j := strings.IndexByte(pattern[i+1:], '%')
		if j < 0 {
			return ""
		}
		value := m.getenv(pattern[i+1 : i+1+j])
		if value == "" {
			return ""
		}
		pattern = pattern[:i] + value + pattern[i+j+2:]
	}
}

// launchable is whether c is something this launcher may start: an absolute
// path to a regular file (or, on macOS, an application bundle), outside every
// project folder, and on Windows a real executable rather than a script a
// command interpreter would have to run.
func (m machine) launchable(c candidate) bool {
	if !m.absolute(c.path) {
		return false
	}
	if !c.bundle && !m.isFile(c.path) {
		return false
	}
	if m.goos == "windows" && !strings.EqualFold(filepath.Ext(c.path), ".exe") {
		return false
	}
	if m.inside(c.path) {
		return false
	}
	// Where it really is: a link on PATH that points into a project is that
	// project's program, whatever the link's own name says.
	real, err := m.eval(c.path)
	if err != nil || m.inside(real) {
		return false
	}
	return true
}

// inside is whether c lies within a forbidden folder, compared against each
// folder as named and as it resolves, since either may be reached through a link.
func (m machine) inside(c string) bool { return m.containing(c) != "" }

// containing is the forbidden folder c lies within, as it was named, or "".
func (m machine) containing(c string) string {
	clean := filepath.Clean(c)
	for _, root := range m.forbidden {
		roots := []string{filepath.Clean(root)}
		if real, err := m.eval(root); err == nil {
			roots = append(roots, filepath.Clean(real))
		}
		for _, r := range roots {
			if r == "." || r == "" {
				continue
			}
			rel, err := filepath.Rel(r, clean)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				return root
			}
		}
	}
	return ""
}

// startDetached starts exe and returns without waiting. Not in the project: a
// program that inherits a folder as its working directory keeps a handle on it,
// which stops Windows renaming or deleting it afterwards.
func startDetached(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Dir = os.TempDir()
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
