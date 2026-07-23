// Command reasonix-guard diagnoses and repairs Reasonix without loading the
// desktop shell. It is packaged beside the desktop application so recovery
// remains available when Wails, WebView, or user configuration cannot start.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/repair"
)

var version = "dev"

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) == 0 {
		return runLaunch(nil)
	}
	switch args[0] {
	case "check":
		return runCheck(args[1:], false)
	case "repair":
		return runCheck(args[1:], true)
	case "launch":
		return runLaunch(args[1:])
	case "recover":
		return runRecover(args[1:])
	case "snapshots":
		return runSnapshots(args[1:])
	case "restore":
		return runRestore(args[1:])
	case "undo":
		return runUndo(args[1:])
	case "diagnose":
		return runDiagnose(args[1:])
	case "rebuild":
		return runRebuild(args[1:])
	case "assist":
		return runAssist(args[1:])
	case "apply-plan":
		return runApplyPlan(args[1:])
	case "version", "--version", "-v":
		fmt.Println("reasonix-guard", version)
		return 0
	case "help", "--help", "-h":
		usage()
		return 0
	default:
		if isMacBundleLauncher() {
			return runLaunch(append([]string{"--"}, args...))
		}
		fmt.Fprintln(os.Stderr, "unknown command:", args[0])
		usage()
		return 2
	}
}

func runDiagnose(args []string) int {
	fs := flag.NewFlagSet("reasonix-guard diagnose", flag.ContinueOnError)
	root := fs.String("root", ".", "project root to inspect")
	network := fs.Bool("network", false, "probe provider endpoints and credentials")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	report, err := repair.Diagnose(context.Background(), repair.DiagnoseOptions{Root: *root, Network: *network})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *jsonOut {
		if code := printJSON(report); code != 0 {
			return code
		}
	} else {
		fmt.Println("Reasonix Guard diagnostics")
		if len(report.Findings) == 0 {
			fmt.Println("  ok: no issues found")
		}
		for _, finding := range report.Findings {
			fmt.Printf("  %-7s %-32s %s\n", finding.Severity, finding.Code, finding.Message)
		}
	}
	if report.HasErrors() {
		return 1
	}
	return 0
}

func runRebuild(args []string) int {
	fs := flag.NewFlagSet("reasonix-guard rebuild", flag.ContinueOnError)
	target := fs.String("target", "", "tabs|projects|window|zoom|all")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || strings.TrimSpace(*target) == "" {
		return 2
	}
	applied, err := repair.RebuildDerivedState(*target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *jsonOut {
		return printJSON(applied)
	}
	for _, path := range applied {
		fmt.Println("quarantined:", path)
	}
	return 0
}

func runSnapshots(args []string) int {
	fs := flag.NewFlagSet("reasonix-guard snapshots", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	snapshots, err := repair.ListConfigSnapshots()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *jsonOut {
		return printJSON(snapshots)
	}
	for _, snap := range snapshots {
		fmt.Printf("%s  %s  %s\n", snap.ID, snap.Version, snap.RecordedAt)
	}
	return 0
}

func runRestore(args []string) int {
	fs := flag.NewFlagSet("reasonix-guard restore", flag.ContinueOnError)
	snapshot := fs.String("snapshot", "", "snapshot id to restore")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || strings.TrimSpace(*snapshot) == "" {
		return 2
	}
	tx, err := repair.RestoreConfigSnapshot(*snapshot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Println("restored config snapshot; undo transaction:", tx.ID)
	return 0
}

func runUndo(args []string) int {
	fs := flag.NewFlagSet("reasonix-guard undo", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	tx, err := repair.UndoLastRepair()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *jsonOut {
		return printJSON(tx)
	}
	fmt.Println("undid repair:", tx.ID)
	return 0
}

func printJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func runCheck(args []string, apply bool) int {
	name := "reasonix-guard check"
	if apply {
		name = "reasonix-guard repair"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	root := fs.String("root", ".", "project root to inspect")
	includeProject := fs.Bool("project", false, "allow repair to quarantine project reasonix.toml")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	report, err := repair.InspectAndRepairConfig(repair.ConfigOptions{Root: *root, Apply: apply, IncludeProject: *includeProject})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return 1
		}
	} else {
		fmt.Println("Reasonix Guard")
		for _, check := range report.Checks {
			status := "ok"
			if !check.Exists {
				status = "missing"
			} else if !check.Valid {
				status = "invalid"
			}
			fmt.Printf("  %-8s %-8s %s\n", check.Scope, status, check.Path)
			if check.Error != "" {
				fmt.Println("    ", check.Error)
			}
		}
		for _, action := range report.Applied {
			fmt.Println("  applied:", action)
		}
	}
	for _, check := range report.Checks {
		if check.Exists && !check.Valid {
			return 1
		}
	}
	return 0
}

// failedInstallBlocksLaunch decides whether startup must stop after a failed
// RecoverFailedInstall. The failure marker means the installer may have left
// the release unit partially replaced, so the binaries cannot be presumed
// coherent until a rollback completes: any error with the restore unfinished
// fails closed. MixedInstall alone is not the signal — a rollback that failed
// before touching anything (e.g. while staging) reports MixedInstall=false yet
// leaves the installer's half-written unit in place. Only a completed restore
// (RolledBack, with just the marker cleanup failing) is safe to launch.
func failedInstallBlocksLaunch(result repair.UpdateRollbackResult, err error) bool {
	return err != nil && !result.RolledBack
}

func runLaunch(args []string) int {
	fs := flag.NewFlagSet("reasonix-guard launch", flag.ContinueOnError)
	app := fs.String("app", "", "desktop executable path")
	safeMode := fs.Bool("safe-mode", false, "force Safe Mode")
	detach := fs.Bool("detach", packagedDetachedLauncher(), "start the desktop and exit the launcher")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	// An update helper that watched the installer fail records a marker (it
	// cannot roll back itself from outside the install directory). Restore the
	// previous release unit immediately instead of waiting for a crash loop.
	if result, failure, err := repair.RecoverFailedInstall(); err != nil {
		fmt.Fprintln(os.Stderr, "update rollback after failed install failed:", err)
		if failedInstallBlocksLaunch(result, err) {
			fmt.Fprintln(os.Stderr, "error: the failed installer may have left a mix of two releases and the rollback did not complete; refusing to start. Re-run reasonix-guard to retry the rollback, or reinstall Reasonix.")
			return 1
		}
	} else if failure != nil && result.RolledBack {
		fmt.Fprintf(os.Stderr, "Reasonix Guard restored %s after the %s installer failed.\n", result.ToVersion, result.FromVersion)
	}
	tracker := repair.NewStartupTracker("")
	useSafeMode := *safeMode
	if !*safeMode && tracker.SafeModeRecommended() {
		if result, err := repair.RollbackPendingUpdate(); err != nil {
			fmt.Fprintln(os.Stderr, "update rollback failed:", err)
			if result.MixedInstall {
				fmt.Fprintln(os.Stderr, "error: the installation now mixes two releases; refusing to start. Re-run reasonix-guard to retry the rollback, or reinstall Reasonix.")
				return 1
			}
			// The compensated install is still a coherent (new-version) release
			// unit, so a Safe Mode boot is a sound fallback while the pending
			// transaction stays on disk for the next rollback attempt.
			useSafeMode = true
		} else if result.RolledBack {
			fmt.Fprintf(os.Stderr, "Reasonix Guard restored %s after %s failed to start.\n", result.ToVersion, result.FromVersion)
			_ = tracker.MarkClean()
		} else {
			switch nativeRecoveryChoice() {
			case recoveryRepair:
				if _, err := repair.InspectAndRepairConfig(repair.ConfigOptions{Root: ".", Apply: true}); err != nil {
					fmt.Fprintln(os.Stderr, "repair failed:", err)
				}
				useSafeMode = true
			case recoverySafeMode:
				useSafeMode = true
			case recoveryQuit:
				return 0
			}
		}
	}
	path := strings.TrimSpace(*app)
	if path == "" {
		path = siblingDesktopExecutable()
	}
	if path == "" {
		fmt.Fprintln(os.Stderr, "error: cannot locate reasonix-desktop; pass --app PATH")
		return 1
	}
	childArgs := append([]string(nil), fs.Args()...)
	if useSafeMode {
		childArgs = append([]string{"--safe-mode"}, childArgs...)
	}
	cmd := exec.Command(path, childArgs...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if *detach {
		if err := cmd.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		return 0
	}
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func packagedDetachedLauncher() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	name := strings.ToLower(filepath.Base(exe))
	return name == "reasonix-launcher.exe" || name == "reasonix.exe"
}

func runRecover(args []string) int {
	fs := flag.NewFlagSet("reasonix-guard recover", flag.ContinueOnError)
	root := fs.String("root", ".", "project root to inspect")
	includeProject := fs.Bool("project", false, "allow repair to quarantine project reasonix.toml")
	app := fs.String("app", "", "desktop executable path")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	switch nativeRecoveryChoice() {
	case recoveryRepair:
		if _, err := repair.InspectAndRepairConfig(repair.ConfigOptions{Root: *root, Apply: true, IncludeProject: *includeProject}); err != nil {
			fmt.Fprintln(os.Stderr, "repair failed:", err)
			return 1
		}
		launch := []string{"--safe-mode"}
		if strings.TrimSpace(*app) != "" {
			launch = append(launch, "--app", *app)
		}
		return runLaunch(launch)
	case recoverySafeMode:
		launch := []string{"--safe-mode"}
		if strings.TrimSpace(*app) != "" {
			launch = append(launch, "--app", *app)
		}
		return runLaunch(launch)
	default:
		return 0
	}
}

func siblingDesktopExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	name := "reasonix-desktop"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	candidates := []string{filepath.Join(filepath.Dir(exe), name)}
	if runtime.GOOS == "darwin" {
		candidates = append(candidates, filepath.Clean(filepath.Join(filepath.Dir(exe), "..", "MacOS", name)))
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func isMacBundleLauncher() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	exe, err := os.Executable()
	return err == nil && strings.Contains(filepath.ToSlash(exe), ".app/Contents/MacOS/")
}

func usage() {
	fmt.Println("usage: reasonix-guard <check|repair|diagnose|rebuild|assist|apply-plan|launch|recover|snapshots|restore|undo> [options]")
}
