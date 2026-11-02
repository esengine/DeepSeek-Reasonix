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
	// State is set only when the catalog exists and is not ok, so its presence
	// is the signal and a healthy catalog's JSON keeps the shape scripts read.
	State string `json:"state,omitempty"`
	// Supported is the schema this build migrates to; it explains degraded.
	Supported int `json:"supported,omitempty"`
	projectiondb.Recorded
}

type catalogCommand struct {
	name            string
	path            func() string
	reindex         func([]string) int
	supportedSchema func() int
	inspect         func(context.Context, string) (projectiondb.Recorded, error)
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
		items = append(items, inspectCatalog(ctx, command))
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
		fmt.Println(catalogLine(item))
	}
	// Inspection only reports. It exits 0 in every state because
	// `catalogs reindex --json` reports the same failures with 0: a script
	// reads the facts, and a person still has a command to run.
	return 0
}

// inspectCatalog answers two things the old line conflated: what this build
// would do with the file, and what the catalog itself recorded. Only a schema
// this build understands is asked what it recorded — a newer one may not have
// the tables.
func inspectCatalog(ctx context.Context, command catalogCommand) catalogInspection {
	item := catalogInspection{Name: command.name, Info: projectiondb.Inspect(ctx, command.path())}
	switch {
	case !item.Info.Exists:
		return item
	case item.Info.Error != "":
		item.State = "error: " + item.Info.Error
		return item
	case item.Info.Integrity != "" && item.Info.Integrity != "ok":
		item.State = "corrupt"
		return item
	}
	supported := 0
	if command.supportedSchema != nil {
		supported = command.supportedSchema()
	}
	if supported > 0 && item.Info.Schema > supported {
		// The runtime serves memory and keeps the file for the build that
		// wrote it; saying ok would tell the user their search is complete.
		item.State = "degraded"
		item.Supported = supported
		return item
	}
	if command.inspect == nil {
		return item
	}
	recorded, err := command.inspect(ctx, item.Info.Path)
	if err != nil {
		item.State = "error: " + err.Error()
		return item
	}
	item.Recorded = recorded
	if recorded.Failed > 0 || recorded.CorruptLines > 0 {
		item.State = "failed"
	}
	return item
}

// catalogLine keeps one line per catalog, facts included. The prefix is the
// shape it always was, so an older reader still finds schema, size and path;
// the facts append to it because a failure is what the line was missing.
func catalogLine(item catalogInspection) string {
	var facts []string
	for _, fact := range []struct {
		label string
		value int64
	}{
		{"failed", item.Failed},
		{"corrupt-lines", item.CorruptLines},
		{"pending", item.Pending},
		{"lag", item.LagBytes},
	} {
		if fact.value > 0 {
			facts = append(facts, fmt.Sprintf("%s=%d", fact.label, fact.value))
		}
	}
	if item.Supported > 0 && item.State == "degraded" {
		facts = append(facts, fmt.Sprintf("supported=%d", item.Supported))
	}
	if len(item.Failures) > 0 {
		facts = append(facts, "source="+item.Failures[0].Path+": "+item.Failures[0].LastError)
	}
	line := fmt.Sprintf("  %-8s %-12s schema=%d size=%d path=%s",
		item.Name, oneLine(item.displayState()), item.Info.Schema, item.Info.Size, item.Info.Path)
	if len(facts) > 0 {
		line += " " + strings.Join(facts, " ")
	}
	return oneLine(line)
}

// displayState is the state column's word. An empty State means the catalog is
// there and read cleanly: ok and missing stay out of the JSON, where exists and
// the old fields already said so.
func (item catalogInspection) displayState() string {
	if item.State != "" {
		return item.State
	}
	if !item.Info.Exists {
		return "missing"
	}
	return "ok"
}

// oneLine keeps the promise of one line per catalog: a recorded error carries
// whatever the failure wrapped.
func oneLine(text string) string {
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(text)
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
