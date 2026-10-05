package winuninstall

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

const (
	CurrentKeyName = "ReasonixReasonix"
	LegacyKeyName  = "Reasonix"
)

type Registration struct {
	DisplayName          string
	DisplayVersion       string
	Publisher            string
	InstallLocation      string
	DisplayIcon          string
	UninstallString      string
	QuietUninstallString string
}

type ReconcilePlan struct {
	Managed      bool
	Desired      Registration
	DeleteLegacy bool
}

// Plan returns the safe per-user uninstall-registration mutation for an
// installed Reasonix tree. Portable trees have no owned registration and no
// root uninstaller, so they deliberately produce a no-op plan.
func Plan(current, legacy *Registration, installRoot, version string, uninstallerPresent bool) (ReconcilePlan, error) {
	root := cleanWindowsPath(installRoot)
	if root == "" {
		return ReconcilePlan{}, fmt.Errorf("Windows install root is empty")
	}
	version = strings.TrimSpace(version)
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	if !semver.IsValid(version) {
		return ReconcilePlan{}, fmt.Errorf("Windows install version is invalid")
	}
	if !uninstallerPresent {
		return ReconcilePlan{}, nil
	}

	currentOwned := registrationOwnsRoot(current, root)
	legacyOwned := registrationOwnsRoot(legacy, root)
	// A legacy-only key may still point at the Tauri 0.53 uninstaller. The full
	// signed installer replaces that binary before migrating the key; the
	// update helper must not promote it into the current Wails identity.
	if !currentOwned {
		return ReconcilePlan{}, nil
	}

	uninstaller := joinWindowsPath(root, "uninstall.exe")
	return ReconcilePlan{
		Managed: true,
		Desired: Registration{
			DisplayName:          "Reasonix",
			DisplayVersion:       strings.TrimPrefix(version, "v"),
			Publisher:            "Reasonix",
			InstallLocation:      root,
			DisplayIcon:          joinWindowsPath(root, "reasonix-launcher.exe"),
			UninstallString:      quoteWindowsPath(uninstaller),
			QuietUninstallString: quoteWindowsPath(uninstaller) + " /S",
		},
		DeleteLegacy: legacyOwned,
	}, nil
}

func registrationOwnsRoot(reg *Registration, root string) bool {
	if reg == nil || !strings.EqualFold(strings.TrimSpace(reg.DisplayName), "Reasonix") {
		return false
	}
	if sameWindowsPath(reg.InstallLocation, root) {
		return true
	}
	return sameWindowsPath(uninstallExecutable(reg.UninstallString), joinWindowsPath(root, "uninstall.exe"))
}

func uninstallExecutable(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	if command[0] == '"' {
		if end := strings.Index(command[1:], `"`); end >= 0 {
			return command[1 : end+1]
		}
	}
	if end := strings.IndexAny(command, " \t"); end >= 0 {
		return command[:end]
	}
	return command
}

func sameWindowsPath(left, right string) bool {
	left = strings.ToLower(cleanWindowsPath(left))
	right = strings.ToLower(cleanWindowsPath(right))
	return left != "" && left == right
}

func cleanWindowsPath(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	value = strings.ReplaceAll(strings.TrimSpace(value), "/", `\`)
	for len(value) > 3 && strings.HasSuffix(value, `\`) {
		value = strings.TrimSuffix(value, `\`)
	}
	return value
}

func joinWindowsPath(root, name string) string {
	return cleanWindowsPath(root) + `\` + strings.TrimLeft(strings.ReplaceAll(name, "/", `\`), `\`)
}

func quoteWindowsPath(path string) string {
	return `"` + cleanWindowsPath(path) + `"`
}
