package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const (
	npmPlatformPkgPrefix = "@reasonix/cli-"
	brewCaskName         = "reasonix"
)

var upgradeExecutable = os.Executable

// managedInstallError marks an executable that a package manager owns and keeps
// records for; replacing it behind that manager's back leaves the records
// naming a version that is no longer on disk.
type managedInstallError struct {
	manager string
	command string
}

func (e *managedInstallError) Error() string {
	return "executable is owned by " + e.manager
}

// npmOwnedExecutable reports whether exe is the prebuilt binary of an npm
// package: <pkg>/bin/<exe> with <pkg>/package.json naming one of ours.
func npmOwnedExecutable(exe string) bool {
	binDir := filepath.Dir(exe)
	if filepath.Base(binDir) != "bin" {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(binDir), "package.json"))
	if err != nil {
		return false
	}
	var pkg struct {
		Name string `json:"name"`
	}
	return json.Unmarshal(raw, &pkg) == nil && strings.HasPrefix(pkg.Name, npmPlatformPkgPrefix)
}

// brewOwnedExecutable reports whether exe sits in Homebrew's Caskroom/<cask>/
// tree, where `brew upgrade` replaces the whole version directory.
func brewOwnedExecutable(exe string) bool {
	parts := strings.Split(filepath.ToSlash(exe), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "Caskroom" && parts[i+1] == brewCaskName {
			return true
		}
	}
	return false
}

// checkSelfReplaceable returns a *managedInstallError when the running
// executable belongs to a package manager.
func checkSelfReplaceable() error {
	exe, err := upgradeExecutable()
	if err != nil {
		return nil
	}
	resolved, _ := resolveSymlinks(exe)
	switch {
	case npmOwnedExecutable(resolved):
		return &managedInstallError{manager: "npm", command: "npm install -g reasonix@latest"}
	case brewOwnedExecutable(resolved):
		return &managedInstallError{manager: "Homebrew", command: "brew upgrade --cask " + brewCaskName}
	}
	return nil
}
