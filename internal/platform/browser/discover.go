package browser

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
)

// Discover finds a Chromium-family browser already installed on this machine:
// Chrome, then Edge, then Chromium. It never downloads one. configured, when
// set, is used as given and is the only candidate.
func Discover(configured string) (string, error) {
	if configured != "" {
		if isExecutableFile(configured) {
			return configured, nil
		}
		return "", fail(CodeEngineMissing, "the configured browser %q is not an executable file", configured)
	}
	for _, candidate := range installedCandidates(runtime.GOOS, os.Getenv) {
		if filepath.IsAbs(candidate) {
			if isExecutableFile(candidate) {
				return candidate, nil
			}
			continue
		}
		if found, err := exec.LookPath(candidate); err == nil {
			return found, nil
		}
	}
	return "", fail(CodeEngineMissing, "no Chrome, Edge or Chromium is installed; install one, or set [browser] executable to its path — which is read when a session starts, so it takes a new one")
}

func installedCandidates(goos string, getenv func(string) string) []string {
	switch goos {
	case "darwin":
		home := getenv("HOME")
		var out []string
		for _, app := range []string{
			"Google Chrome.app/Contents/MacOS/Google Chrome",
			"Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"Chromium.app/Contents/MacOS/Chromium",
		} {
			out = append(out, path.Join("/Applications", app))
			if home != "" {
				out = append(out, path.Join(home, "Applications", app))
			}
		}
		return out
	case "windows":
		var out []string
		for _, base := range []string{getenv("ProgramFiles"), getenv("ProgramFiles(x86)"), getenv("LocalAppData")} {
			if base == "" {
				continue
			}
			out = append(out,
				filepath.Join(base, `Google\Chrome\Application\chrome.exe`),
				filepath.Join(base, `Microsoft\Edge\Application\msedge.exe`),
			)
		}
		return out
	default:
		return []string{
			"google-chrome", "google-chrome-stable",
			"microsoft-edge", "microsoft-edge-stable",
			"chromium", "chromium-browser",
		}
	}
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0
}
