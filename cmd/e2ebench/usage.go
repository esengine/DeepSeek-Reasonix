package main

import (
	"flag"
	"fmt"
	"strings"
)

// printUsage is the harness's help text. It names one thing -mode audit does
// not do — replay the experiment — and what a replay would need instead.
func printUsage() {
	fmt.Fprintf(flag.CommandLine.Output(), "e2ebench — Reasonix end-to-end benchmark.\n\n")
	fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", flag.CommandLine.Name())
	flag.PrintDefaults()
	fmt.Fprintf(flag.CommandLine.Output(), "\nExamples:\n")
	fmt.Fprintf(flag.CommandLine.Output(), "  # Run the committed suite:\n")
	fmt.Fprintf(flag.CommandLine.Output(), "  %[1]s\n\n", strings.Replace(flag.CommandLine.Name(), "e2ebench", "go run ./cmd/e2ebench", 1))
	fmt.Fprintf(flag.CommandLine.Output(), "  # Grade a PR's diff with a retry budget:\n")
	fmt.Fprintf(flag.CommandLine.Output(), "  %[1]s -mode diff -base origin/main-v2 -repo . -attempts 3 -timeout 1800\n", strings.Replace(flag.CommandLine.Name(), "e2ebench", "go run ./cmd/e2ebench", 1))
	fmt.Fprintf(flag.CommandLine.Output(), "\n  # Run the same suite with the delivery contract:\n")
	fmt.Fprintf(flag.CommandLine.Output(), "  %[1]s -profile delivery\n", strings.Replace(flag.CommandLine.Name(), "e2ebench", "go run ./cmd/e2ebench", 1))
	fmt.Fprintf(flag.CommandLine.Output(), "\n  # Keep a batch's rows and manifest, then audit its figures against them:\n")
	fmt.Fprintf(flag.CommandLine.Output(), "  %[1]s -batch runs/today\n", strings.Replace(flag.CommandLine.Name(), "e2ebench", "go run ./cmd/e2ebench", 1))
	fmt.Fprintf(flag.CommandLine.Output(), "  %[1]s -mode audit -batch runs/today\n\n", strings.Replace(flag.CommandLine.Name(), "e2ebench", "go run ./cmd/e2ebench", 1))
	fmt.Fprintf(flag.CommandLine.Output(), "Auditing is not replaying: -mode audit re-derives the report's headline figures\n"+
		"from results.jsonl and checks them against batch.json, naming every figure that moved.\n"+
		"It never re-executes the model, tools or environment, so it cannot show the experiment\n"+
		"reproduces. A real replay would need the commit, corpus fingerprint, model and provider\n"+
		"configuration, seed, flags, raw results and runtime version; it is not implemented.\n")
}
