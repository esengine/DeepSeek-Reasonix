// Command reasonix-legacy-migrator is the one-shot flat→versioned install
// migrator used by v1.20+ packaging. In compatibility payloads it is still
// named reasonix-guard(.exe) so 1.18–1.19.1 updaters can hand off; the source
// and behavior are intentionally separate from the old Guard recovery product.
//
// It only: acquires a migration lock, validates the flat release unit, creates
// versions/<version>, writes current.json, rewrites entry points, starts the
// thin launcher, and self-deletes when possible. It never chooses safe mode,
// counts crashes, or auto-rolls back.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"reasonix/internal/desktoplauncher"
	"reasonix/internal/fileutil"
	"reasonix/internal/installlayout"
	"reasonix/internal/repair"
)

var version = "dev"

const migrationLockName = ".reasonix-layout-migrate.lock"

func main() {
	if runningAsLauncher() {
		os.Exit(desktoplauncher.Run(os.Args[1:], version))
	}
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 1 {
		switch args[0] {
		case "version", "--version", "-v":
			fmt.Println("reasonix-legacy-migrator", version)
			return 0
		case "help", "--help", "-h":
			fmt.Println("usage: reasonix-legacy-migrator [--install-root PATH] [--version VERSION] [--activate-staging PATH]")
			return 0
		}
	}

	installRoot := ""
	activeVersion := strings.TrimSpace(version)
	activateStaging := ""
	relaunch := true
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--install-root":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --install-root requires a path")
				return 2
			}
			i++
			installRoot = args[i]
		case "--version":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --version requires a value")
				return 2
			}
			i++
			activeVersion = args[i]
		case "--activate-staging":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --activate-staging requires a path")
				return 2
			}
			i++
			activateStaging = args[i]
		case "launch", "--detach", "--safe-mode":
			// Accept legacy argv from old shortcuts; no product behavior.
			continue
		case "--no-relaunch":
			relaunch = false
			continue
		case "--app":
			if i+1 < len(args) {
				i++
			}
			continue
		}
	}
	if installRoot == "" {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		installRoot = filepath.Dir(exe)
	}
	installRoot = filepath.Clean(installRoot)
	if activateStaging != "" {
		activeVersion, err := normalizeActiveVersion(activeVersion)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		if err := activateInstallerStaging(installRoot, activeVersion, activateStaging); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		if relaunch {
			_ = startLauncher(installRoot)
		}
		return 0
	}

	if err := migrateWithRelaunch(installRoot, activeVersion, relaunch); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func migrate(installRoot, activeVersion string) error {
	return migrateWithRelaunch(installRoot, activeVersion, true)
}

func migrateWithRelaunch(installRoot, activeVersion string, relaunch bool) error {
	var err error
	activeVersion, err = normalizeActiveVersion(activeVersion)
	if err != nil {
		return err
	}

	unlock, err := acquireMigrationLock(installRoot)
	if err != nil {
		return err
	}
	defer unlock()

	// Pointer already committed: only cleanup, never overwrite the active version.
	// A present but corrupt pointer is fatal; treating it like an absent pointer
	// could overwrite evidence of a partial activation and migrate stale files.
	currentPath := filepath.Join(installRoot, installlayout.CurrentFileName)
	if _, statErr := os.Lstat(currentPath); statErr == nil {
		ptr, err := installlayout.ReadCurrent(installRoot)
		if err != nil {
			return err
		}
		if err := finalizeLegacyPendingUpdate(installRoot, ptr.ActiveVersion); err != nil {
			return err
		}
		_ = cleanupLegacyFlatFiles(installRoot)
		_ = archiveInstallRootLegacyMarkers(installRoot)
		if relaunch {
			_ = startLauncher(installRoot) // best-effort; layout already committed
			selfDelete()
		}
		return nil
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("migrate: inspect current.json: %w", statErr)
	}

	desktopName := installlayout.DesktopBinaryName()
	cliName := installlayout.CLIBinaryName()
	helperName := installlayout.UpdateHelperBinaryName()
	flatDesktop := filepath.Join(installRoot, desktopName)
	if _, err := os.Lstat(flatDesktop); err != nil {
		return fmt.Errorf("migrate: flat desktop binary missing: %w", err)
	}

	members := []installlayout.Member{
		{Name: desktopName, Path: flatDesktop},
	}
	if p := optionalRegular(filepath.Join(installRoot, cliName)); p != "" {
		members = append(members, installlayout.Member{Name: cliName, Path: p})
	} else {
		// CLI may be absent on some portable trees; synthesize from desktop only
		// is not allowed — require the whitelist. Prefer copying desktop as a
		// last-resort placeholder is forbidden; fail closed.
		return fmt.Errorf("migrate: flat CLI binary %s is required", cliName)
	}
	requiredNames := []string{desktopName, cliName}
	if runtime.GOOS == "windows" {
		requiredNames = append(requiredNames, helperName)
		if p := optionalRegular(filepath.Join(installRoot, helperName)); p != "" {
			members = append(members, installlayout.Member{Name: helperName, Path: p})
		} else {
			return fmt.Errorf("migrate: flat update helper %s is required", helperName)
		}
	}

	// Old Linux updaters only publish desktop, CLI, and the compatibility
	// migrator. On Unix the migrator is also a thin-launcher multicall binary,
	// so it can create the permanent entry point without another payload member.
	if err := ensureLauncherEntry(installRoot); err != nil {
		return err
	}
	// v1.18-v1.19 helpers leave an installed transaction awaiting Guard health.
	// The flat release unit is still intact here, so commit that exact verified
	// transaction before moving its files into the version directory.
	if err := finalizeLegacyPendingUpdate(installRoot, activeVersion); err != nil {
		return err
	}

	if err := installlayout.ActivateVersion(installlayout.ActivationRequest{
		InstallRoot:   installRoot,
		Version:       activeVersion,
		RequestID:     "legacy-migrate",
		Members:       members,
		RequiredNames: requiredNames,
	}); err != nil {
		return fmt.Errorf("migrate: activate versioned layout: %w", err)
	}

	_ = cleanupLegacyFlatFiles(installRoot)
	_ = archiveInstallRootLegacyMarkers(installRoot)
	if relaunch {
		// Launcher start is best-effort: layout activation is the commit point.
		if err := startLauncher(installRoot); err != nil {
			fmt.Fprintln(os.Stderr, "warning: start launcher after migrate:", err)
		}
		// Unix can unlink the running migrator. On Windows the parent thin launcher
		// removes it after this process exits.
		selfDelete()
	}
	return nil
}

func normalizeActiveVersion(activeVersion string) (string, error) {
	if err := installlayout.ValidateVersionName(activeVersion); err != nil {
		// Accept bare "dev" builds in development only.
		if activeVersion == "" || activeVersion == "dev" {
			activeVersion = "v0.0.0-dev"
			if err := installlayout.ValidateVersionName(activeVersion); err != nil {
				return "", err
			}
		} else if !strings.HasPrefix(activeVersion, "v") {
			activeVersion = "v" + activeVersion
			if err := installlayout.ValidateVersionName(activeVersion); err != nil {
				return "", err
			}
		} else {
			return "", err
		}
	}
	return activeVersion, nil
}

func activateInstallerStaging(installRoot, activeVersion, stagingRoot string) error {
	installRoot = filepath.Clean(strings.TrimSpace(installRoot))
	stagingRoot = filepath.Clean(strings.TrimSpace(stagingRoot))
	if !pathWithinInstallRoot(installRoot, stagingRoot) || stagingRoot == installRoot {
		return fmt.Errorf("installer staging must be inside the install root")
	}
	info, err := os.Lstat(stagingRoot)
	if err != nil {
		return fmt.Errorf("inspect installer staging: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("installer staging must be a real directory")
	}

	desktopName := installlayout.DesktopBinaryName()
	cliName := installlayout.CLIBinaryName()
	requiredNames := []string{desktopName, cliName}
	if runtime.GOOS == "windows" {
		requiredNames = append(requiredNames, installlayout.UpdateHelperBinaryName())
	}
	members := make([]installlayout.Member, 0, len(requiredNames))
	for _, name := range requiredNames {
		members = append(members, installlayout.Member{Name: name, Path: filepath.Join(stagingRoot, name)})
	}

	launcherName := installlayout.LauncherBinaryName()
	launcherSource := filepath.Join(stagingRoot, launcherName)
	rootMembers := []installlayout.Member{
		{Name: launcherName, Path: launcherSource},
		{Name: cliName, Path: filepath.Join(stagingRoot, cliName)},
	}
	requiredRootNames := []string{launcherName, cliName}
	if alias := installlayout.PortableAliasName(); alias != "" {
		rootMembers = append(rootMembers, installlayout.Member{Name: alias, Path: launcherSource})
		requiredRootNames = append(requiredRootNames, alias)
	}

	if err := installlayout.ActivateVersion(installlayout.ActivationRequest{
		InstallRoot:       installRoot,
		Version:           activeVersion,
		RequestID:         "signed-installer-" + activeVersion,
		Members:           members,
		RequiredNames:     requiredNames,
		RootMembers:       rootMembers,
		RequiredRootNames: requiredRootNames,
	}); err != nil {
		return fmt.Errorf("activate signed installer staging: %w", err)
	}
	return nil
}

func runningAsLauncher() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Base(exe), installlayout.LauncherBinaryName())
}

func optionalRegular(path string) string {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	return path
}

func acquireMigrationLock(installRoot string) (func(), error) {
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(installRoot, migrationLockName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			// Stale lock from a dead migrator: if older than 10 minutes, steal it.
			info, statErr := os.Stat(path)
			if statErr == nil && time.Since(info.ModTime()) > 10*time.Minute {
				_ = os.Remove(path)
				f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("migrate: acquire lock: %w", err)
		}
	}
	_, _ = fmt.Fprintf(f, "pid=%d\nversion=%s\n", os.Getpid(), version)
	_ = f.Close()
	return func() { _ = os.Remove(path) }, nil
}

func ensureLauncherEntry(installRoot string) error {
	launcher := installlayout.LauncherBinaryName()
	path := filepath.Join(installRoot, launcher)
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("migrate: thin launcher %s is not a regular file", launcher)
		}
		return nil
	}
	// On Windows also accept Reasonix.exe as the alias.
	if runtime.GOOS == "windows" {
		if _, err := os.Lstat(filepath.Join(installRoot, "Reasonix.exe")); err == nil {
			return nil
		}
		return fmt.Errorf("migrate: thin launcher %s is missing; install a signed package", launcher)
	}

	// Legacy Linux archives cannot deliver a fourth member through the old
	// updater. Copy this signed multicall migrator as the permanent thin launcher.
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("migrate: resolve migrator executable: %w", err)
	}
	info, err := os.Lstat(exe)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("migrate: migrator executable is not a regular file")
	}
	body, err := os.ReadFile(exe)
	if err != nil {
		return fmt.Errorf("migrate: read launcher source: %w", err)
	}
	if err := fileutil.AtomicWriteFile(path, body, 0o755); err != nil {
		return fmt.Errorf("migrate: install thin launcher: %w", err)
	}
	return nil
}

func finalizeLegacyPendingUpdate(installRoot, activeVersion string) error {
	tx, err := repair.ReadPendingUpdate()
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("migrate: read legacy pending update: %w", err)
	}
	if tx.TargetKind != "file" {
		return fmt.Errorf("migrate: legacy pending update target kind %q is not supported", tx.TargetKind)
	}
	if strings.TrimSpace(tx.ToVersion) != strings.TrimSpace(activeVersion) {
		return fmt.Errorf("migrate: pending update targets %s, not %s", tx.ToVersion, activeVersion)
	}
	for _, target := range append([]repair.UpdateTransactionFile{{TargetPath: tx.TargetPath}}, tx.Files...) {
		if !pathWithinInstallRoot(installRoot, target.TargetPath) {
			return fmt.Errorf("migrate: pending update target escapes install root")
		}
	}
	id := repair.UpdateTransactionID(tx)
	if err := repair.MarkUpdateHealthyExact(activeVersion, tx.CreatedAt, id); err != nil {
		return fmt.Errorf("migrate: commit legacy pending update: %w", err)
	}
	if current, err := repair.ReadPendingUpdate(); err == nil {
		if repair.UpdateTransactionID(current) == id {
			return fmt.Errorf("migrate: legacy pending update remained after commit")
		}
		return fmt.Errorf("migrate: pending update changed during commit")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("migrate: verify legacy pending update commit: %w", err)
	}
	return nil
}

func pathWithinInstallRoot(installRoot, target string) bool {
	root := filepath.Clean(strings.TrimSpace(installRoot))
	target = filepath.Clean(strings.TrimSpace(target))
	if root == "" || target == "" || !filepath.IsAbs(root) || !filepath.IsAbs(target) {
		return false
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return false
	}
	return true
}

func cleanupLegacyFlatFiles(installRoot string) error {
	// Remove flat desktop/CLI/helper/guard sidecars after the version tree is
	// active. Never delete the thin launcher or current.json.
	names := []string{
		installlayout.DesktopBinaryName(),
		installlayout.CLIBinaryName(),
		installlayout.UpdateHelperBinaryName(),
	}
	if runtime.GOOS == "windows" {
		names = append(names, "reasonix-guard.exe")
	} else {
		names = append(names, "reasonix-guard")
	}
	for _, name := range names {
		path := filepath.Join(installRoot, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		_ = os.Remove(path)
	}
	return nil
}

func archiveInstallRootLegacyMarkers(installRoot string) error {
	// Very old portable builds wrote markers beside the binary. Current repair
	// state under Reasonix home is finalized through repair transaction APIs.
	markers := []string{
		"pending-update.json",
		"startup-state.json",
	}
	ts := time.Now().UTC().Format("20060102T150405Z")
	destRoot := filepath.Join(installRoot, "repair", "legacy-v1", ts)
	moved := false
	for _, name := range markers {
		src := filepath.Join(installRoot, name)
		if _, err := os.Lstat(src); err != nil {
			continue
		}
		if err := os.MkdirAll(destRoot, 0o755); err != nil {
			return err
		}
		dest := filepath.Join(destRoot, name)
		if err := os.Rename(src, dest); err != nil {
			// Cross-device: copy+remove best effort.
			data, readErr := os.ReadFile(src)
			if readErr != nil {
				continue
			}
			if writeErr := fileutil.AtomicWriteFile(dest, data, 0o600); writeErr == nil {
				_ = os.Remove(src)
			}
		}
		moved = true
	}
	if moved {
		meta, _ := json.MarshalIndent(map[string]any{
			"migratedAt": time.Now().UTC().Format(time.RFC3339),
			"from":       "flat-v1",
			"to":         installlayout.InstallLayoutVersionedV1,
		}, "", "  ")
		_ = fileutil.AtomicWriteFile(filepath.Join(destRoot, "migration.json"), append(meta, '\n'), 0o644)
	}
	return nil
}

func startLauncher(installRoot string) error {
	launcher := "reasonix-launcher"
	if runtime.GOOS == "windows" {
		launcher += ".exe"
	}
	path := filepath.Join(installRoot, launcher)
	if _, err := os.Lstat(path); err != nil {
		if runtime.GOOS == "windows" {
			path = filepath.Join(installRoot, "Reasonix.exe")
		}
	}
	if _, err := os.Lstat(path); err != nil {
		// Migration succeeded; user can start manually.
		return nil
	}
	cmd := exec.Command(path)
	cmd.Dir = installRoot
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if runtime.GOOS == "windows" {
		return cmd.Start()
	}
	return cmd.Start()
}

func selfDelete() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	// Do not delete if we are not named like a compatibility guard payload.
	base := strings.ToLower(filepath.Base(exe))
	if base != "reasonix-guard" && base != "reasonix-guard.exe" {
		return
	}
	_ = os.Remove(exe)
}
