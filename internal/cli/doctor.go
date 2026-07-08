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
	if len(args) > 0 && args[0] == "redact-sessions" {
		return doctorRedactSessionsCommand(args[1:])
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

type stringListFlag []string

func (f *stringListFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, string(os.PathListSeparator))
}

func (f *stringListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("empty path")
	}
	*f = append(*f, value)
	return nil
}

func doctorRedactSessionsCommand(args []string) int {
	fs := flag.NewFlagSet("doctor redact-sessions", flag.ContinueOnError)
	var dirs stringListFlag
	dryRun := fs.Bool("dry-run", false, "show how many session files would be redacted without writing")
	jsonOut := fs.Bool("json", false, "print result as JSON")
	fs.Var(&dirs, "dir", "session directory to scan; repeat to scan multiple directories")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: reasonix doctor redact-sessions [--dry-run] [--json] [--dir PATH]")
		return 2
	}
	res := doctor.RedactSessions(doctor.RedactSessionsOptions{
		Dirs:   []string(dirs),
		DryRun: *dryRun,
	})
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	} else {
		action := "redacted"
		if *dryRun {
			action = "would redact"
		}
		fmt.Fprintf(os.Stdout, "session secret cleanup %s %d/%d files", action, res.FilesChanged, res.FilesScanned)
		if res.FilesSkipped > 0 {
			fmt.Fprintf(os.Stdout, " (%d skipped: active lease held)", res.FilesSkipped)
		}
		fmt.Fprintln(os.Stdout)
	}
	for _, msg := range res.Errors {
		fmt.Fprintln(os.Stderr, "warning:", msg)
	}
	if len(res.Errors) > 0 {
		return 1
	}
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
