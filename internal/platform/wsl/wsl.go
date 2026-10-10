package wsl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"reasonix/internal/base/proc"
)

var (
	// ErrUnavailable: this machine cannot run WSL, or wsl.exe is not there.
	ErrUnavailable = errors.New("wsl: not available on this machine")
	// ErrNoDistro: the name is not one of the installed distributions.
	ErrNoDistro = errors.New("wsl: no such distribution")
	// ErrUnsupported: the distribution answered, but not as a machine a kernel runs on.
	ErrUnsupported = errors.New("wsl: unsupported distribution")
)

const (
	maxOutput    = 64 << 10
	probeTimeout = 30 * time.Second
)

// Distro is an installed distribution. Version is its WSL generation, 1 or 2.
type Distro struct {
	Name    string
	Default bool
	Version int
}

// Probe is what a distribution answered. Arch is a GOARCH.
type Probe struct {
	Distro Distro
	Arch   string
	User   string
}

// Runner runs wsl.exe with args and returns what it wrote to stdout.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// System runs the wsl.exe in System32, so a PATH entry cannot stand in for it.
func System() Runner {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		root := os.Getenv("SystemRoot")
		if runtime.GOOS != "windows" || root == "" {
			return nil, ErrUnavailable
		}
		exe := filepath.Join(root, "System32", "wsl.exe")
		if _, err := os.Stat(exe); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		cmd := exec.CommandContext(ctx, exe, args...)
		proc.HideWindow(cmd)
		var out bytes.Buffer
		cmd.Stdout = &limited{w: &out, left: maxOutput}
		if err := cmd.Run(); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}
}

type limited struct {
	w    *bytes.Buffer
	left int
}

func (l *limited) Write(p []byte) (int, error) {
	n := len(p)
	if n > l.left {
		p = p[:l.left]
	}
	l.left -= len(p)
	l.w.Write(p)
	return n, nil
}

// Decode turns wsl.exe output into text: it is UTF-16 when it is piped, and
// UTF-8 when the machine was asked for it.
func Decode(b []byte) string {
	if bytes.IndexByte(b, 0) < 0 {
		return strings.TrimPrefix(string(b), string(rune(0xFEFF)))
	}
	units := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		units = append(units, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return strings.TrimPrefix(string(utf16.Decode(units)), string(rune(0xFEFF)))
}

// ParseList reads `wsl -l -v`: after the header line, each row is an optional
// "*", the name, a state word and the version.
func ParseList(out string) ([]Distro, error) {
	lines := strings.Split(strings.ReplaceAll(out, "\r", ""), "\n")
	var list []Distro
	header := true
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if header {
			header = false
			continue
		}
		d := Distro{}
		if fields[0] == "*" {
			d.Default = true
			fields = fields[1:]
		}
		if len(fields) != 3 {
			return nil, fmt.Errorf("%w: unexpected list row %q", ErrUnsupported, strings.TrimSpace(line))
		}
		v, err := strconv.Atoi(fields[2])
		if err != nil || (v != 1 && v != 2) {
			return nil, fmt.Errorf("%w: unexpected list row %q", ErrUnsupported, strings.TrimSpace(line))
		}
		d.Name, d.Version = fields[0], v
		list = append(list, d)
	}
	return list, nil
}

// List returns the installed distributions. A machine with none installed has
// an empty list and no error.
func List(ctx context.Context, run Runner) ([]Distro, error) {
	out, err := run(ctx, "-l", "-v")
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return nil, err
		}
		// With nothing installed wsl.exe exits non-zero and says so in prose.
		return nil, nil
	}
	return ParseList(Decode(out))
}

// ParseArch maps `uname -m` to a GOARCH.
func ParseArch(out string) (string, error) {
	switch strings.TrimSpace(out) {
	case "x86_64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	}
	return "", fmt.Errorf("%w: architecture %q", ErrUnsupported, strings.TrimSpace(out))
}

// Check asks a distribution what it is. name must be one of the listed
// distributions, which keeps anything else out of wsl.exe's argv; the commands
// run are fixed and go through --exec, with no shell in between.
func Check(ctx context.Context, run Runner, name string) (Probe, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	list, err := List(ctx, run)
	if err != nil {
		return Probe{}, err
	}
	var p Probe
	found := false
	for _, d := range list {
		if d.Name == name {
			p.Distro, found = d, true
		}
	}
	if !found {
		return Probe{}, fmt.Errorf("%w: %q", ErrNoDistro, name)
	}
	machine, err := run(ctx, "-d", name, "--exec", "uname", "-m")
	if err != nil {
		return Probe{}, fmt.Errorf("%w: %q did not answer uname: %w", ErrUnsupported, name, err)
	}
	if p.Arch, err = ParseArch(Decode(machine)); err != nil {
		return Probe{}, err
	}
	user, err := run(ctx, "-d", name, "--exec", "id", "-un")
	if err != nil {
		return Probe{}, fmt.Errorf("%w: %q did not answer id: %w", ErrUnsupported, name, err)
	}
	p.User = strings.TrimSpace(Decode(user))
	return p, nil
}
