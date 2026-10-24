package cli

import (
	"fmt"
	"os"
	"time"

	"reasonix/internal/base/i18n"
)

func reportRunBuildFailure(output *runOutputSink, format runOutputFormat, started time.Time, err error) int {
	if output != nil && format != runOutputText {
		if encodeErr := output.Finalize("", started, err); encodeErr != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, encodeErr)
		}
		return 1
	}
	fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
	return 1
}
