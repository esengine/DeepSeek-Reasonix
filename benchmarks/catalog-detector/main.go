// Which detector can own "the skills listing is owed again"? The answer decides
// whether a projection debt is a flag every writer must remember to set, or a
// question the host answers for itself. Owed means one thing here: the rendered
// available-skills block differs from the one the model last received, so a
// touched mtime and a body edit are not owed. This probe writes only inside its
// own temporary workspaces.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"time"
)

func main() {
	scales := flag.String("scales", "0,10,50,200", "synthetic skill counts to measure")
	samples := flag.Int("samples", 51, "cost samples per detector per scale")
	real := flag.String("real", "", "also measure this workspace as it is on disk")
	flag.Parse()

	costs := measureCost(parseScales(*scales), *samples, *real)
	verdicts := measureVerdicts()

	fmt.Println(renderCost(costs))
	fmt.Println()
	fmt.Println(renderVerdicts(verdicts))
	if failed(verdicts) {
		// Not an error: a detector that misses is the finding, and the exit
		// code stays zero so a study can report one.
		fmt.Println("\nA detector with a miss cannot own correctness; one with a false positive cannot own cache stability.")
	}
}

func parseScales(s string) []int {
	var out []int
	for _, f := range splitComma(s) {
		var n int
		if _, err := fmt.Sscanf(f, "%d", &n); err == nil {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

func splitComma(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}

func percentile(samples []time.Duration, p float64) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	idx := int(float64(len(samples)-1) * p)
	return samples[idx]
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "catalog-detector:", err)
		os.Exit(1)
	}
}
