// Command reasonix is a config- and plugin-driven coding agent CLI.
package main

import (
	"os"

	"reasonix/internal/cli"

	// Blank imports wire compile-time built-ins into their registries.
	_ "reasonix/internal/provider/anthropic"
	_ "reasonix/internal/provider/openai"
	_ "reasonix/internal/tool/builtin"
)

// Build identity is injected at build time via -ldflags.
var (
	version      = "dev"
	buildNumber  = ""
	buildTimeUTC = ""
	gitCommit    = ""
	gitDirty     = ""
	buildProfile = ""
	buildTarget  = ""
)

func main() {
	os.Exit(cli.RunWithBuildInfo(os.Args[1:], cli.BuildInfo{
		Version:      version,
		BuildNumber:  buildNumber,
		BuildTimeUTC: buildTimeUTC,
		GitCommit:    gitCommit,
		GitDirty:     gitDirty,
		BuildProfile: buildProfile,
		BuildTarget:  buildTarget,
	}))
}
