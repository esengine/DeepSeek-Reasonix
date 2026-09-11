package cli

import (
	"context"
	"flag"
	"fmt"
	"os"

	"reasonix/internal/config"
	"reasonix/internal/usagecatalog"
)

func init() {
	registerCatalogCommand(catalogCommand{
		name: "usage", path: usagecatalog.DefaultPath, reindex: reindexUsageCatalog,
		completionFlags: []cliCompletionFlag{completionFlag("--json", cliCompletionNoValue)},
	})
}

func reindexUsageCatalog(args []string) int {
	fs := flag.NewFlagSet("catalogs reindex usage", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print status as JSON")
	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	status, err := usagecatalog.Rebuild(context.Background(), "", config.StatsDir())
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return printCatalogStatus(status, *jsonOut)
}
