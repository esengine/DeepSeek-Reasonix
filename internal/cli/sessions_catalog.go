package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	if len(args) == 0 || args[0] != "reindex" {
		fmt.Fprintln(os.Stderr, "usage: reasonix sessions reindex [--dir PATH] [--json]")
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
