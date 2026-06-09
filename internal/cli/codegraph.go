package cli

import (
	"context"
	"fmt"
	"os"

	"reasonix/internal/codegraph"
	"reasonix/internal/config"
	"reasonix/internal/netclient"
)

// codegraphCommand backs `reasonix codegraph` — managing the CodeGraph
// code-intelligence runtime that reasonix otherwise fetches lazily on first use.
func codegraphCommand(args []string) int {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "install":
		return codegraphInstall()
	case "status", "":
		return codegraphStatus()
	case "help", "-h", "--help":
		codegraphUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown codegraph subcommand %q\n\n", sub)
		codegraphUsage()
		return 2
	}
}

func codegraphInstall() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	client, err := netclient.NewHTTPClient(cfg.NetworkProxySpec(), netclient.TransportOptions{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	p, err := codegraph.InstallWithClient(context.Background(), client, func(m string) { fmt.Println(m) })
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("codegraph ready:", p)
	return 0
}

func codegraphStatus() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("%-13s %v\n", "enabled:", cfg.Codegraph.Enabled)
	fmt.Printf("%-13s %v\n", "auto_install:", cfg.Codegraph.AutoInstall)
	fmt.Printf("%-13s %s\n", "startup:", cfg.Codegraph.ResolvedTier())
	fmt.Printf("%-13s %s\n", "version:", codegraph.Version)
	fmt.Printf("%-13s %s\n", "cache:", codegraph.CacheDir())
	if p, ok := codegraph.Resolve(cfg.Codegraph.Path); ok {
		fmt.Printf("%-13s %s\n", "resolved:", p)
	} else {
		fmt.Printf("%-13s %s\n", "resolved:", "(not installed — run `reasonix codegraph install`)")
	}
	return 0
}

func codegraphUsage() {
	fmt.Print(`reasonix codegraph — manage the CodeGraph code-intelligence runtime

Usage:
  reasonix codegraph install   download + cache the runtime for this platform
  reasonix codegraph status    show config, cache dir, and resolved launcher

CodeGraph is fetched automatically on first use (unless [codegraph].auto_install
is false); this command installs it explicitly or reports where it resolves from.
`)
}
