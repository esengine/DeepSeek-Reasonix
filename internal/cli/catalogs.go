package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"reasonix/internal/projectiondb"
	"reasonix/internal/sessioncatalog"
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

func runSessionOrCatalogCommand(command string, args []string) int {
	configureCLIThemeFromConfig()
	if command != "catalogs" {
		return sessionOrSessionsCommand(command, args)
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
	items := []catalogInspection{
		{Name: "sessions", Info: projectiondb.Inspect(ctx, sessioncatalog.DefaultPath())},
	}
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
