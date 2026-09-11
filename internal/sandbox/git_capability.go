package sandbox

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/proc"
	"reasonix/internal/secrets"
)

// discoverGitCapability shares the shell inventory snapshot but remains a
// separate capability: Git availability neither implies Bash on Unix nor
// participates in ResolveShell's interpreter decision.
func discoverGitCapability(snap *shellSnapshot) ExecutableCapability {
	capability := ExecutableCapability{ID: HostCapabilityGit}
	found := false
	probes := map[string]bool{}
	usable := func(path string) bool {
		found = true
		key := strings.ToLower(filepath.Clean(path))
		if result, ok := probes[key]; ok {
			return result
		}
		if snap.gitPreflight != nil && !snap.gitPreflight(path) {
			probes[key] = false
			return false
		}
		result := snap.gitProbe == nil || snap.gitProbe(path)
		probes[key] = result
		return result
	}
	for _, name := range []string{"git", "git.exe"} {
		if path, err := snap.lookPath(name); err == nil && usable(path) {
			capability.Available = true
			capability.Path = path
			capability.Source = ShellSourcePath
			return capability
		}
	}

	if snap.goos == "windows" {
		var bashPaths []string
		if path, err := snap.lookPath("bash"); err == nil {
			bashPaths = append(bashPaths, path)
		}
		bashPaths = append(bashPaths, snap.bashCands...)
		for _, bashPath := range bashPaths {
			for _, path := range gitCandidatesFromWindowsBash(bashPath) {
				if !snap.exists(path) || !usable(path) {
					continue
				}
				capability.Available = true
				capability.Path = path
				capability.Source = snap.sources[strings.ToLower(bashPath)]
				if capability.Source == "" {
					capability.Source = ShellSourceGitDerived
				}
				return capability
			}
		}
	} else {
		for _, path := range []string{"/opt/homebrew/bin/git", "/usr/local/bin/git", "/usr/bin/git", "/bin/git"} {
			if snap.exists(path) && usable(path) {
				capability.Available = true
				capability.Path = path
				capability.Source = ShellSourceStandard
				return capability
			}
		}
	}
	capability.Reason = "not-found"
	if found {
		capability.Reason = "not-usable"
	}
	return capability
}

func probeGit(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := proc.CommandContext(ctx, path, "--version")
	cmd.Env = secrets.ProcessEnv()
	proc.HideWindow(cmd)
	return cmd.Run() == nil
}

func gitCandidatesFromWindowsBash(bashPath string) []string {
	var out []string
	dir := pathDir(bashPath)
	for range 3 {
		if dir == "" || dir == "." {
			break
		}
		out = append(out,
			filepath.Join(dir, "cmd", "git.exe"),
			filepath.Join(dir, "bin", "git.exe"),
			filepath.Join(dir, "git.exe"),
		)
		next := pathDir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	return out
}
