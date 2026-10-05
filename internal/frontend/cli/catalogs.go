package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/frontend/termrender"
	"reasonix/internal/state/projectiondb"
)

type catalogInspection struct {
	Name string                  `json:"name"`
	Info projectiondb.Inspection `json:"info"`
}

type catalogCommand struct {
	name            string
	path            func() string
	reindex         func([]string) int
	completionFlags []cliCompletionFlag
}

var catalogCommands []catalogCommand

func registerCatalogCommand(command catalogCommand) {
	catalogCommands = append(catalogCommands, command)
}

// sessionDirTarget names one directory of session transcripts. The catalogs
// that index them (history, tasks) all start from the same list.
type sessionDirTarget struct {
	Path          string
	Scope         string
	WorkspaceRoot string
}

// defaultSessionCatalogTargets lists every session directory this install
// writes to: the global one, the global workspace, and each desktop project.
func defaultSessionCatalogTargets() []sessionDirTarget {
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
	targets := make([]sessionDirTarget, 0, len(saved.Projects)+2)
	add := func(target sessionDirTarget) {
		target.Path = filepath.Clean(strings.TrimSpace(target.Path))
		if target.Path == "." || target.Path == "" || seen[target.Path] {
			return
		}
		seen[target.Path] = true
		targets = append(targets, target)
	}
	add(sessionDirTarget{Path: config.SessionDir(), Scope: "global"})
	add(sessionDirTarget{
		Path:  config.ProjectSessionDir(filepath.Join(home, "global-workspace")),
		Scope: "global",
	})
	for _, savedProject := range saved.Projects {
		root := strings.TrimSpace(savedProject.Root)
		if root == "" {
			continue
		}
		add(sessionDirTarget{
			Path: config.ProjectSessionDir(root), Scope: "project", WorkspaceRoot: root,
		})
	}
	return targets
}

func runSessionOrCatalogCommand(command string, args []string) int {
	termrender.ConfigureThemeFromConfig()
	switch command {
	case "session":
		return sessionCommand(args)
	case "sessions":
		return runSessionsCommand(args)
	}
	return catalogsCommand(args)
}

func doctorCatalogsCommand(args []string) int {
	fs := flag.NewFlagSet("doctor catalogs", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print catalog diagnostics as JSON")
	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: reasonix doctor catalogs [--json]")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	items := make([]catalogInspection, 0, len(catalogCommands))
	for _, command := range catalogCommands {
		items = append(items, catalogInspection{Name: command.name, Info: projectiondb.Inspect(ctx, command.path())})
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(items); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	fmt.Println("Reasonix disposable catalogs")
	for _, item := range items {
		state := "missing"
		if item.Info.Exists && item.Info.Error == "" {
			state = item.Info.Integrity
		} else if item.Info.Error != "" {
			state = "error: " + item.Info.Error
		}
		fmt.Printf("  %-8s %-12s schema=%d size=%d path=%s\n", item.Name, state, item.Info.Schema, item.Info.Size, item.Info.Path)
	}
	return 0
}

func catalogsCommand(args []string) int {
	if len(args) < 2 || args[0] != "reindex" {
		fmt.Fprintln(os.Stderr, "usage: reasonix catalogs reindex <catalog> [options]")
		return 2
	}
	for _, command := range catalogCommands {
		if args[1] == command.name {
			return command.reindex(args[2:])
		}
	}
	fmt.Fprintln(os.Stderr, "unknown catalog:", args[1])
	return 2
}

func printCatalogStatus(status any, jsonOut bool) int {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(status); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	b, _ := json.Marshal(status)
	fmt.Println(string(b))
	return 0
}
