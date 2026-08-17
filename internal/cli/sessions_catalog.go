package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/sessioncatalog"
)

func sessionOrSessionsCommand(command string, args []string) int {
	if command == "sessions" {
		return sessionsCommand(args)
	}
	return sessionCommand(args)
}

func sessionsCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: reasonix sessions <reindex|diagnose|cleanup> [--dir PATH] [--json]")
		return 2
	}
	switch args[0] {
	case "diagnose":
		return sessionsRecoveryCommand(args[1:], false)
	case "cleanup":
		return sessionsRecoveryCommand(args[1:], true)
	case "reindex":
	default:
		fmt.Fprintln(os.Stderr, "usage: reasonix sessions <reindex|diagnose|cleanup> [--dir PATH] [--json]")
		return 2
	}
	fs := flag.NewFlagSet("sessions reindex", flag.ContinueOnError)
	var dirs stringListFlag
	jsonOut := fs.Bool("json", false, "print the rebuilt catalog status as JSON")
	fs.Var(&dirs, "dir", "session directory to index; repeat for multiple directories")
	if code, ok := parseCommandFlags(fs, args[1:]); !ok {
		return code
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: reasonix sessions reindex [--dir PATH] [--json]")
		return 2
	}
	if len(dirs) == 0 {
		status, err := sessioncatalog.Rebuild(context.Background(), sessioncatalog.DefaultPath(), defaultSessionCatalogTargets())
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		return printSessionCatalogRebuild(status, *jsonOut)
	}
	targets := make([]sessioncatalog.DirectoryTarget, 0, len(dirs))
	seen := map[string]bool{}
	for _, dir := range dirs {
		dir = filepath.Clean(strings.TrimSpace(dir))
		if dir == "." || dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		targets = append(targets, sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"})
	}
	status, err := sessioncatalog.Rebuild(context.Background(), sessioncatalog.DefaultPath(), targets)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return printSessionCatalogRebuild(status, *jsonOut)
}

type sessionRecoveryReport struct {
	Directories     int      `json:"directories"`
	Groups          int      `json:"groups"`
	Branches        int      `json:"branches"`
	AdoptedGroups   int      `json:"adoptedGroups"`
	DivergedGroups  int      `json:"divergedGroups"`
	CleanupEligible int      `json:"cleanupEligible"`
	MovedToTrash    int      `json:"movedToTrash"`
	Busy            int      `json:"busy"`
	Errors          []string `json:"errors"`
	DryRun          bool     `json:"dryRun"`
}

func sessionsRecoveryCommand(args []string, cleanup bool) int {
	name := "sessions diagnose"
	if cleanup {
		name = "sessions cleanup"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	var dirs stringListFlag
	apply := fs.Bool("apply", false, "move safe covered recovery branches to recoverable trash")
	jsonOut := fs.Bool("json", false, "print the recovery report as JSON")
	fs.Var(&dirs, "dir", "session directory to inspect; repeat for multiple directories")
	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() != 0 || (!cleanup && *apply) {
		fmt.Fprintf(os.Stderr, "usage: reasonix %s [--dir PATH] [--json]", name)
		if cleanup {
			fmt.Fprint(os.Stderr, " [--apply]")
		}
		fmt.Fprintln(os.Stderr)
		return 2
	}
	if len(dirs) == 0 {
		for _, target := range defaultSessionCatalogTargets() {
			dirs = append(dirs, target.Path)
		}
	}
	report := sessionRecoveryReport{Errors: []string{}, DryRun: !cleanup || !*apply}
	seen := map[string]bool{}
	for _, rawDir := range dirs {
		dir := filepath.Clean(strings.TrimSpace(rawDir))
		if dir == "." || dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		report.Directories++
		catalog, err := sessioncatalog.Open(context.Background(), sessioncatalog.Options{InMemory: true, DisableRepair: true})
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
			continue
		}
		err = catalog.ReconcileDirectory(context.Background(), sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"})
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
			_ = catalog.Close(context.Background())
			continue
		}
		groups, err := catalog.ListRecoveryGroups(context.Background(), dir)
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
			_ = catalog.Close(context.Background())
			continue
		}
		for _, group := range groups {
			rootID, members := group.ID, group.Members
			report.Groups++
			report.Branches += len(members)
			canonical := ""
			diverged := false
			for _, member := range members {
				if member.RecoveryCanonical && (member.RecoveryRole == sessioncatalog.RecoveryRoleAdopted || member.RecoveryRole == sessioncatalog.RecoveryRolePreferred) {
					canonical = member.Path
				}
				if member.RecoveryRole == sessioncatalog.RecoveryRoleDiverged {
					diverged = true
				}
			}
			if canonical == "" {
				if diverged {
					report.DivergedGroups++
				}
				continue
			}
			report.AdoptedGroups++
			candidates := []string{}
			for _, member := range members {
				if member.Path != canonical && member.RecoveryRole == sessioncatalog.RecoveryRoleCoveredCopy {
					candidates = append(candidates, member.Path)
				}
			}
			report.CleanupEligible += len(candidates)
			if report.DryRun || len(candidates) == 0 {
				continue
			}
			if err := agent.ReparentRecoveryCanonical(canonical, rootID, dir); err != nil {
				if errors.Is(err, agent.ErrSessionLeaseHeld) {
					report.Busy += len(candidates)
				} else {
					report.Errors = append(report.Errors, err.Error())
				}
				continue
			}
			for _, candidate := range candidates {
				if err := agent.TrashRecoveryBranchCoveredBy(candidate, canonical, dir); err != nil {
					if errors.Is(err, agent.ErrSessionLeaseHeld) {
						report.Busy++
					} else {
						report.Errors = append(report.Errors, err.Error())
					}
					continue
				}
				report.MovedToTrash++
			}
		}
		_ = catalog.Close(context.Background())
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		fmt.Printf("recovery groups: %d (%d adopted, %d diverged)\n", report.Groups, report.AdoptedGroups, report.DivergedGroups)
		fmt.Printf("recovery branches: %d; safe cleanup: %d; moved: %d; busy: %d\n", report.Branches, report.CleanupEligible, report.MovedToTrash, report.Busy)
		if report.DryRun && cleanup {
			fmt.Println("dry run; pass --apply to move safe branches to recoverable trash")
		}
	}
	for _, message := range report.Errors {
		fmt.Fprintln(os.Stderr, "warning:", message)
	}
	if len(report.Errors) > 0 {
		return 1
	}
	return 0
}

func printSessionCatalogRebuild(status sessioncatalog.Status, jsonOut bool) int {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(status); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	fmt.Printf("rebuilt session catalog: %d sessions, revision %d\n", status.Indexed, status.Revision)
	return 0
}

func defaultSessionCatalogTargets() []sessioncatalog.DirectoryTarget {
	type project struct {
		Root string `json:"root"`
	}
	type projectFile struct {
		Projects []project `json:"projects"`
	}
	home := config.ReasonixHomeDir()
	var saved projectFile
	if data, err := os.ReadFile(filepath.Join(home, "desktop-projects.json")); err == nil {
		_ = json.Unmarshal(data, &saved)
	}
	seen := map[string]bool{}
	targets := make([]sessioncatalog.DirectoryTarget, 0, len(saved.Projects)+2)
	add := func(target sessioncatalog.DirectoryTarget) {
		target.Path = filepath.Clean(strings.TrimSpace(target.Path))
		if target.Path == "." || target.Path == "" || seen[target.Path] {
			return
		}
		seen[target.Path] = true
		targets = append(targets, target)
	}
	add(sessioncatalog.DirectoryTarget{Path: config.SessionDir(), Scope: "global"})
	add(sessioncatalog.DirectoryTarget{
		Path:  config.ProjectSessionDir(filepath.Join(home, "global-workspace")),
		Scope: "global",
	})
	for _, savedProject := range saved.Projects {
		root := strings.TrimSpace(savedProject.Root)
		if root == "" {
			continue
		}
		add(sessioncatalog.DirectoryTarget{
			Path: config.ProjectSessionDir(root), Scope: "project", WorkspaceRoot: root,
		})
	}
	return targets
}
