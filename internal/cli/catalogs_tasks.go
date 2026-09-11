package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/taskcatalog"
)

func init() {
	registerCatalogCommand(catalogCommand{
		name: "tasks", path: taskcatalog.DefaultPath, reindex: reindexTaskCatalog,
		completionFlags: []cliCompletionFlag{
			completionFlag("--json", cliCompletionNoValue),
			completionFlag("--project", cliCompletionPathValue),
		},
	})
}

func reindexTaskCatalog(args []string) int {
	fs := flag.NewFlagSet("catalogs reindex tasks", flag.ContinueOnError)
	var projects stringListFlag
	jsonOut := fs.Bool("json", false, "print status as JSON")
	fs.Var(&projects, "project", "project root to index; repeat for multiple projects")
	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	if len(projects) == 0 {
		for _, target := range defaultSessionCatalogTargets() {
			if strings.TrimSpace(target.WorkspaceRoot) != "" {
				projects = append(projects, target.WorkspaceRoot)
			}
		}
	}
	items := make([]taskcatalog.Project, 0, len(projects))
	for _, root := range projects {
		items = append(items, taskcatalog.Project{Root: root, Label: filepath.Base(root)})
	}
	status, err := taskcatalog.Rebuild(context.Background(), "", items)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return printCatalogStatus(status, *jsonOut)
}
