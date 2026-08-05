// Command reasonix-launcher is the permanent thin desktop entry point for the
// versioned install layout (v1.20+). It reads InstallRoot/current.json, starts
// the active reasonix-desktop binary, and exits. It never counts crashes,
// chooses previous versions, or enters a product safe mode.
//
// Migration compatibility: old shortcuts may still pass "launch", "--detach",
// or "--safe-mode". Those tokens are stripped and ignored.
package main

import (
	"os"

	"reasonix/internal/desktoplauncher"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	return desktoplauncher.Run(args, version)
}

func resolveDesktopPath(installRoot string) (string, error) {
	return desktoplauncher.ResolveDesktopPath(installRoot)
}

func stripLegacyLaunchArgs(args []string) []string {
	return desktoplauncher.StripLegacyLaunchArgs(args)
}
