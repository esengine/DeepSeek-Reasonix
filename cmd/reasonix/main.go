// Command reasonix is a config- and plugin-driven coding agent CLI.
package main

import (
	"os"
	"runtime/debug"

	"reasonix/internal/cli"
	"reasonix/internal/config"
	"reasonix/internal/crashreport"

	// Blank imports wire compile-time built-ins into their registries.
	_ "reasonix/internal/provider/anthropic"
	_ "reasonix/internal/provider/openai"
	_ "reasonix/internal/tool/builtin"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

var runCLI = cli.Run

func main() {
	os.Exit(runWithCrashCapture(os.Args[1:], version))
}

func runWithCrashCapture(args []string, buildVersion string) (exitCode int) {
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = crashreport.CapturePanic(config.ReasonixHomeDir(), buildVersion, recovered, debug.Stack())
			panic(recovered)
		}
	}()
	return runCLI(args, buildVersion)
}
