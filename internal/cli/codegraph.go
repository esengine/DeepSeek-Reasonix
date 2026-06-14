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
	case "update":
		return codegraphUpdate()
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

func codegraphUpdate() int {
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
	res, err := codegraph.UpdateWithClient(context.Background(), client, func(m string) { fmt.Println(m) })
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("codegraph updated: %s (%s)\n", res.Path, res.Version)
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
	if active := codegraph.ActiveVersion(); active != "" {
		fmt.Printf("%-13s %s\n", "active:", active)
	}
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
  reasonix codegraph update    download latest upstream runtime and make it active
  reasonix codegraph status    show config, cache dir, and resolved launcher

CodeGraph is fetched automatically on first use (unless [codegraph].auto_install
is false); install uses Reasonix's pinned runtime. Update is explicit because a
new CodeGraph release can change MCP tool schemas and prompt-cache shape.
`)
}
