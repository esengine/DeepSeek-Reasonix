package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Shell is an interpreter this machine has and a terminal can start.
type Shell struct {
	// Name is what the person sees; Kind is the short tag beside it.
	Name string   `json:"name"`
	Kind string   `json:"kind"`
	Path string   `json:"path"`
	Args []string `json:"args,omitempty"`
}

// Shells lists the interpreters found on this machine, best first. Only what is
// really there is listed: a row for a shell that is missing is a button that
// fails.
func Shells(getenv func(string) string) []Shell {
	return shellsOn(runtime.GOOS, getenv, exec.LookPath, fileExists)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func shellsOn(goos string, getenv func(string) string, look func(string) (string, error), exists func(string) bool) []Shell {
	var out []Shell
	seen := map[string]bool{}
	add := func(name, kind, p string, args ...string) {
		key := strings.ToLower(p)
		if p == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, Shell{Name: name, Kind: kind, Path: p, Args: args})
	}
	if goos == "windows" {
		for _, root := range []string{getenv("ProgramFiles"), getenv("ProgramFiles(x86)"), joinFor(goos, getenv("LOCALAPPDATA"), "Programs")} {
			if root == "" {
				continue
			}
			if p := joinFor(goos, root, "Git", "bin", "bash.exe"); exists(p) {
				add("Git Bash", "bash", p, "--login", "-i")
			}
		}
		if p, err := look("pwsh"); err == nil {
			add("PowerShell", "pwsh", p, "-NoLogo")
		}
		if p, err := look("powershell"); err == nil {
			add("Windows PowerShell", "powershell", p, "-NoLogo")
		}
		if p, err := look("cmd"); err == nil {
			add("命令提示符", "cmd", p)
		}
		if p, err := look("wsl"); err == nil {
			add("WSL", "wsl", p)
		}
		return out
	}
	if p := getenv("SHELL"); p != "" && exists(p) {
		add(filepath.Base(p), filepath.Base(p), p, "-l")
	}
	for _, name := range []string{"bash", "zsh", "fish", "sh"} {
		if p, err := look(name); err == nil {
			add(name, name, p)
		}
	}
	return out
}

// joinFor joins path parts the way the named OS does, so a listing computed for
// another OS than the host's still names paths that OS would accept.
func joinFor(goos string, parts ...string) string {
	if goos != "windows" {
		return filepath.Join(parts...)
	}
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimRight(part, `\/`); part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, `\`)
}
