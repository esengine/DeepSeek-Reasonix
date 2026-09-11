package sandbox

import (
	"os/exec"
	"reflect"
	"testing"
)

func TestResolveShellRejectsCrossKindConfiguredPaths(t *testing.T) {
	paths := map[string]string{
		"bash":       `C:\found\bash.exe`,
		"powershell": `C:\found\powershell.exe`,
		"pwsh":       `C:\found\pwsh.exe`,
	}
	lookPath := func(name string) (string, error) {
		if path := paths[name]; path != "" {
			return path, nil
		}
		return "", exec.ErrNotFound
	}
	exists := func(string) bool { return true }
	probe := func(string) bool { return true }
	noWSL := func(string) bool { return false }

	tests := []struct {
		name     string
		prefer   string
		path     string
		wantKind ShellKind
		wantPath string
		wantArgv []string
	}{
		{
			name: "bash path is not relabeled as Windows PowerShell", prefer: "powershell", path: `C:\Git\bin\bash.exe`,
			wantKind: ShellPowerShell, wantPath: paths["powershell"],
			wantArgv: []string{paths["powershell"], "-NoProfile", "-NonInteractive", "-Command"},
		},
		{
			name: "sh path is not relabeled as PowerShell 7", prefer: "pwsh", path: `C:\Tools\sh.exe`,
			wantKind: ShellPowerShell, wantPath: paths["pwsh"],
			wantArgv: []string{paths["pwsh"], "-NoProfile", "-NonInteractive", "-Command"},
		},
		{
			name: "PowerShell path is not probed as Bash", prefer: "bash", path: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
			wantKind: ShellBash, wantPath: paths["bash"], wantArgv: []string{paths["bash"], "-c"},
		},
		{
			name: "zsh path is not relabeled as Bash", prefer: "bash", path: `C:\Tools\zsh.exe`,
			wantKind: ShellBash, wantPath: paths["bash"], wantArgv: []string{paths["bash"], "-c"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveShell(tc.prefer, tc.path, nil, "windows", lookPath, exists, nil, nil, probe, noWSL)
			if got.Kind != tc.wantKind || got.Path != tc.wantPath {
				t.Fatalf("resolved shell = {%s %q}, want {%s %q}", got.Kind, got.Path, tc.wantKind, tc.wantPath)
			}
			argv := got.argv("echo ok")
			if len(argv) < len(tc.wantArgv) || !reflect.DeepEqual(argv[:len(tc.wantArgv)], tc.wantArgv) {
				t.Fatalf("argv = %q, want prefix %q", argv, tc.wantArgv)
			}
		})
	}
}

func TestResolveShellRejectsExplicitWSLLauncher(t *testing.T) {
	const (
		wslBash = `C:\Windows\System32\bash.exe`
		gitBash = `C:\Program Files\Git\bin\bash.exe`
	)
	got := resolveShell(
		"bash", wslBash, nil, "windows",
		func(string) (string, error) { return "", exec.ErrNotFound },
		func(string) bool { return true },
		[]string{gitBash}, nil,
		func(string) bool { return true },
		func(path string) bool { return path == wslBash },
	)
	if got.Kind != ShellBash || got.Path != gitBash {
		t.Fatalf("resolved shell = {%s %q}, want native Git Bash %q", got.Kind, got.Path, gitBash)
	}
}

func TestResolveShellAutoPrefersConfiguredWindowsBashOverPath(t *testing.T) {
	const (
		configured = `E:\Portable\Git\bin\bash.exe`
		onPath     = `C:\Program Files\Git\bin\bash.exe`
	)
	got := resolveShell(
		"auto", configured, nil, "windows",
		func(name string) (string, error) {
			if name == "bash" {
				return onPath, nil
			}
			return "", exec.ErrNotFound
		},
		func(path string) bool { return path == configured || path == onPath },
		nil, nil,
		func(string) bool { return true },
		func(string) bool { return false },
	)
	if got.Kind != ShellBash || got.Path != configured {
		t.Fatalf("resolved shell = {%s %q}, want configured Bash %q before PATH", got.Kind, got.Path, configured)
	}
}

func TestConfiguredShellPathKeepsUnknownWrappers(t *testing.T) {
	exists := func(string) bool { return true }
	noWSL := func(string) bool { return false }
	for _, tc := range []struct {
		kind ShellKind
		path string
	}{
		{ShellBash, `C:\Tools\posix-wrapper.exe`},
		{ShellPowerShell, `C:\Tools\ps-wrapper.exe`},
	} {
		if got := configuredShellPath("windows", tc.kind, tc.path, exists, noWSL); got != tc.path {
			t.Errorf("configured path = %q, want wrapper %q", got, tc.path)
		}
	}
}

func TestConfiguredShellPathRejectsUnresolvedGitBashLauncher(t *testing.T) {
	const launcher = `C:\Tools\Git\git-bash.exe`
	got := configuredShellPath(
		"windows", ShellBash, launcher,
		func(string) bool { return false },
		func(string) bool { return false },
	)
	if got != "" {
		t.Fatalf("configured path = %q, want the MinTTY launcher rejected", got)
	}
	for _, prefer := range []string{"auto", "bash"} {
		if got := configuredWindowsBashPath(prefer, launcher, func(string) bool { return false }); got != "" {
			t.Errorf("configuredWindowsBashPath(%q) = %q, want launcher rejected", prefer, got)
		}
	}
}
