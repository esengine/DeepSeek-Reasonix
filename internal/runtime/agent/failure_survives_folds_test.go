package agent

import (
	"fmt"
	"strings"
)

// goTestFailure is shaped like the real thing on purpose: a failing `go test`
// log opens with "=== RUN", so the keep policy's text-prefix arm cannot see it
// and only ToolExecution can. Synthetic "error: ..." fixtures survive either
// way and would have hidden this.
func goTestFailure() string {
	lines := make([]string, 0, 40)
	for i := range 40 {
		if i == 20 {
			lines = append(lines, "--- FAIL: TestCanary (0.01s)")
			continue
		}
		lines = append(lines, fmt.Sprintf("=== RUN   TestCase%03d", i))
	}
	return strings.Join(lines, "\n")
}
