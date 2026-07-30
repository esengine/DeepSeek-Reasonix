package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/capdiag"
)

func doctorCapabilitiesCommand(args []string) int {
	fs := flag.NewFlagSet("doctor capabilities", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", "", "workspace root (default: current directory)")
	jsonOut := fs.Bool("json", false, "print a single JSON object to stdout")
	live := fs.Bool("live", false, "start automatic MCP servers in an isolated Host (may network)")
	timeoutStr := fs.String("timeout", "", "per-server live probe timeout (1s-60s; requires --live; default 5s)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: reasonix doctor capabilities [--root PATH] [--json] [--live] [--timeout 5s]")
		return 2
	}

	var timeout time.Duration
	if *timeoutStr != "" {
		if !*live {
			fmt.Fprintln(os.Stderr, "error: --timeout requires --live")
			return 2
		}
		d, err := time.ParseDuration(*timeoutStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid --timeout: %v\n", err)
			return 2
		}
		if d < capdiag.MinLiveTimeout || d > capdiag.MaxLiveTimeout {
			fmt.Fprintf(os.Stderr, "error: --timeout must be between %s and %s\n",
				capdiag.MinLiveTimeout, capdiag.MaxLiveTimeout)
			return 2
		}
		timeout = d
	} else if *live {
		timeout = capdiag.DefaultLiveTimeout
	}

	ws := *root
	if ws == "" {
		if wd, err := os.Getwd(); err == nil {
			ws = wd
		} else {
			ws = "."
		}
	}
	if abs, err := filepath.Abs(ws); err == nil {
		ws = abs
	}

	if *live {
		fmt.Fprintln(os.Stderr, capdiag.LiveWarningMessage())
	}

	report := capdiag.Collect(capdiag.Options{
		Root:        ws,
		Live:        *live,
		LiveTimeout: timeout,
	})

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	} else {
		fmt.Fprint(os.Stdout, capdiag.RenderText(report))
	}

	if capdiag.HasErrorSeverity(report) {
		return 1
	}
	return 0
}
