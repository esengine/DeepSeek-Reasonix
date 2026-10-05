package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/state/sessionstore"
)

// sessionsMaintenanceVerbs are 1.x's `reasonix sessions` verbs. Sessions are
// listed straight from their directories, so reindex has no catalog to rebuild
// and reports the directory count; diagnose and cleanup act on the recovery
// branches a lease conflict leaves behind.
var sessionsMaintenanceVerbs = map[string]bool{"reindex": true, "diagnose": true, "cleanup": true}

const sessionsMaintenanceUsage = "usage: reasonix sessions <reindex|diagnose|cleanup> [--dir PATH] [--json]"

type sessionsIndexStatus struct {
	State    string `json:"state"`
	Mode     string `json:"mode"`
	Revision uint64 `json:"revision"`
	Indexed  int64  `json:"indexed"`
	Total    int64  `json:"total"`
}

// sessionsRecoveryReport keeps 1.x's key names for what is still measured
// here; every session on disk is listed, so indexed always equals source.
type sessionsRecoveryReport struct {
	Directories       int      `json:"directories"`
	SourceSessions    int      `json:"sourceSessions"`
	IndexedSessions   int      `json:"indexedSessions"`
	UnindexedSessions int      `json:"unindexedSessions"`
	StaleDirectories  int      `json:"staleDirectories"`
	Branches          int      `json:"branches"`
	CleanupEligible   int      `json:"cleanupEligible"`
	MovedToTrash      int      `json:"movedToTrash"`
	Busy              int      `json:"busy"`
	Errors            []string `json:"errors"`
	DryRun            bool     `json:"dryRun"`
}

func sessionsMaintenanceCommand(args []string, stdout, stderr io.Writer) int {
	verb := args[0]
	fs := flag.NewFlagSet("sessions "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var dirs stringListFlag
	jsonOut := fs.Bool("json", false, "print the result as JSON")
	fs.Var(&dirs, "dir", "session directory; repeat for multiple directories")
	apply := false
	if verb == "cleanup" {
		fs.BoolVar(&apply, "apply", false, "move covered recovery branches to recoverable trash")
	}
	if code, ok := parseCommandFlags(fs, args[1:]); !ok {
		return code
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, sessionsMaintenanceUsage)
		return 2
	}
	targets := sessionsMaintenanceDirs(dirs)
	if verb == "reindex" {
		return sessionsReindex(targets, *jsonOut, stdout, stderr)
	}
	report := sessionsRecoveryReport{Errors: []string{}, DryRun: verb != "cleanup" || !apply}
	for _, dir := range targets {
		report.Directories++
		inspectSessionsDirectory(dir, &report)
	}
	writeSessionsRecoveryReport(stdout, report, *jsonOut, verb == "cleanup")
	for _, message := range report.Errors {
		fmt.Fprintln(stderr, "warning:", message)
	}
	if len(report.Errors) > 0 {
		return 1
	}
	return 0
}

func sessionsMaintenanceDirs(explicit []string) []string {
	var dirs []string
	seen := map[string]bool{}
	add := func(dir string) {
		dir = filepath.Clean(strings.TrimSpace(dir))
		if dir == "." || dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	if len(explicit) > 0 {
		for _, dir := range explicit {
			add(dir)
		}
		return dirs
	}
	for _, target := range defaultSessionCatalogTargets() {
		add(target.Path)
	}
	add(resolveCLISessionDirFor(""))
	return dirs
}

func sessionsReindex(dirs []string, jsonOut bool, stdout, stderr io.Writer) int {
	var total int64
	for _, dir := range dirs {
		sessions, err := sessionstore.ListSessionOrder(dir)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		total += int64(len(sessions))
	}
	if jsonOut {
		return writeSessionsJSON(stdout, stderr, sessionsIndexStatus{State: "ready", Mode: "directory", Indexed: total, Total: total})
	}
	fmt.Fprintf(stdout, "session catalog: sessions are listed from their directories, nothing to rebuild (%d sessions)\n", total)
	return 0
}

func inspectSessionsDirectory(dir string, report *sessionsRecoveryReport) {
	sessions, err := sessionstore.ListSessionOrder(dir)
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		return
	}
	report.SourceSessions += len(sessions)
	report.IndexedSessions += len(sessions)
	for _, session := range sessions {
		if meta, ok, err := sessionstore.LoadBranchMeta(session.Path); err == nil && ok && meta.Recovered {
			report.Branches++
			if sessionstore.SessionLeaseHeld(session.Path) {
				report.Busy++
			}
		}
	}
	candidates, err := sessionstore.ReclaimableRecoveryBranches(dir, time.Now(), 0)
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		return
	}
	report.CleanupEligible += len(candidates)
	if report.DryRun {
		return
	}
	for _, path := range candidates {
		err := sessionstore.TrashCoveredRecoveryBranch(path, dir)
		switch {
		case err == nil:
			report.MovedToTrash++
		case errors.Is(err, sessionstore.ErrSessionLeaseHeld):
			report.Busy++
		case errors.Is(err, sessionstore.ErrRecoveryBranchNotCovered):
		default:
			report.Errors = append(report.Errors, err.Error())
		}
	}
}

func writeSessionsRecoveryReport(w io.Writer, report sessionsRecoveryReport, jsonOut, cleanup bool) {
	if jsonOut {
		_ = writeSessionsJSON(w, io.Discard, report)
		return
	}
	fmt.Fprintf(w, "source sessions: %d; indexed sessions: %d; unindexed: %d; stale directories: %d\n", report.SourceSessions, report.IndexedSessions, report.UnindexedSessions, report.StaleDirectories)
	fmt.Fprintf(w, "recovery branches: %d; safe cleanup: %d; moved: %d; busy: %d\n", report.Branches, report.CleanupEligible, report.MovedToTrash, report.Busy)
	if report.DryRun && cleanup {
		fmt.Fprintln(w, "dry run; pass --apply to move safe branches to recoverable trash")
	}
}

func writeSessionsJSON(stdout, stderr io.Writer, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func runSessionsCommand(args []string) int {
	if len(args) > 0 && sessionsMaintenanceVerbs[args[0]] {
		return sessionsMaintenanceCommand(args, os.Stdout, os.Stderr)
	}
	return sessionCommand(args)
}
