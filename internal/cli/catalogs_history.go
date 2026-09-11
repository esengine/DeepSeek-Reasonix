package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/config"
	"reasonix/internal/historycatalog"
)

func init() {
	registerCatalogCommand(catalogCommand{
		name: "history", path: historycatalog.DefaultPath, reindex: reindexHistoryCatalog,
		completionFlags: []cliCompletionFlag{
			completionFlag("--json", cliCompletionNoValue),
			completionFlag("--dir", cliCompletionPathValue),
		},
	})
}

func reindexHistoryCatalog(args []string) int {
	fs := flag.NewFlagSet("catalogs reindex history", flag.ContinueOnError)
	var dirs stringListFlag
	jsonOut := fs.Bool("json", false, "print status as JSON")
	fs.Var(&dirs, "dir", "session directory to index; repeat for multiple directories")
	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	ctx := context.Background()
	roots := []historycatalog.Root{}
	if len(dirs) > 0 {
		for _, dir := range dirs {
			roots = append(roots, historycatalog.Root{Path: filepath.Clean(dir), Source: "global", Scope: "global"})
		}
	} else {
		for _, target := range defaultSessionCatalogTargets() {
			roots = append(roots, historycatalog.Root{Path: target.Path, Source: target.Scope, Scope: target.Scope, WorkspaceRoot: target.WorkspaceRoot})
			roots = append(roots, historycatalog.Root{Path: filepath.Join(target.Path, "subagents"), Source: target.Scope, Scope: target.Scope, WorkspaceRoot: target.WorkspaceRoot, Subagents: true})
		}
		roots = append(roots, historycatalog.Root{Path: config.ArchiveDir(), Source: "archive", Scope: "global", Archive: true})
	}
	status, err := historycatalog.Rebuild(ctx, historycatalog.Options{}, roots)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return printCatalogStatus(status, *jsonOut)
}
