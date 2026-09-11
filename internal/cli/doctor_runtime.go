package cli

import (
	"flag"
	"fmt"
	"os"

	"reasonix/internal/boot"
)

func doctorRuntimeCommand(args []string) int {
	fs := flag.NewFlagSet("doctor runtime", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print runtime diagnostics as JSON")
	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	// Process-wide gate/metrics report (no live BuildResult in CLI doctor).
	report := boot.CollectRuntimeDoctor(nil)
	if *jsonOut {
		raw, err := boot.RenderRuntimeDoctorJSON(report)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(string(raw))
		return 0
	}
	fmt.Print(boot.RenderRuntimeDoctorText(report))
	return 0
}
