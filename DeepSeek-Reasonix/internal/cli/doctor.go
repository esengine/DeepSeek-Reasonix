package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"reasonix/internal/doctor"
)

func doctorCommand(args []string, version string) int {
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
