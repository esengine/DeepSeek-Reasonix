package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"reasonix/internal/doctor"
)

func doctorCommand(args []string, version string) int {
	if len(args) > 0 && args[0] == "session" {
		return doctorSessionCommand(args[1:], version)
	}
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print diagnostics as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	report := doctor.Collect(doctor.Options{Version: version})
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	fmt.Print(doctor.RenderText(report))
	return 0
}

func doctorSessionCommand(args []string, version string) int {
	ref := ""
	outPath := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-h", "--help":
			fmt.Fprintln(os.Stdout, "usage: reasonix doctor session <branch-id-or-path> [--zip] [--out PATH]")
			fmt.Fprintln(os.Stdout, "")
			fmt.Fprintln(os.Stdout, "Bundles the session transcript, persistence sidecars, conflict diagnostics,")
			fmt.Fprintln(os.Stdout, "and the recovery parent chain into a zip for support. Unlike `reasonix doctor`,")
			fmt.Fprintln(os.Stdout, "bundled transcripts are NOT redacted; share only with a trusted support channel.")
			return 0
		case "--zip":
			// The subcommand currently writes a zip by default. Keep --zip as an
			// explicit, script-friendly marker so support replies can say exactly
			// what to run.
		case "--out":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --out requires a path")
				return 2
			}
			outPath = args[i]
		default:
			if v, ok := strings.CutPrefix(arg, "--out="); ok {
				if v == "" {
					fmt.Fprintln(os.Stderr, "error: --out requires a path")
					return 2
				}
				outPath = v
				continue
			}
			if strings.HasPrefix(arg, "-") {
				fmt.Fprintf(os.Stderr, "error: unknown doctor session flag %s\n", arg)
				return 2
			}
			if ref != "" {
				fmt.Fprintln(os.Stderr, "usage: reasonix doctor session <branch-id-or-path> [--zip] [--out PATH]")
				return 2
			}
			ref = arg
		}
	}
	if ref == "" {
		fmt.Fprintln(os.Stderr, "usage: reasonix doctor session <branch-id-or-path> [--zip] [--out PATH]")
		return 2
	}
	result, err := doctor.WriteSessionBundle(doctor.SessionBundleOptions{
		Version:    version,
		SessionRef: ref,
		OutputPath: outPath,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Println(result.Path)
	fmt.Fprintln(os.Stderr, "note: the bundle contains full session transcripts without redaction; share it only with a trusted support channel")
	return 0
}
