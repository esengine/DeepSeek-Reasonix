package main

import (
	"fmt"
	"slices"
	"strings"
)

// renderCapabilitySets says which capability versions the runs used. A report
// is only a measurement of one version when every run used the same set; more
// than one set means the capabilities moved during the run, and a metric over
// the whole report is an average over versions that no comparison can use.
func renderCapabilitySets(results []result) string {
	sets := map[string]int{}
	recorded := 0
	for _, r := range results {
		if r.Trajectory == nil || r.Trajectory.CapabilitiesHash == "" {
			continue
		}
		recorded++
		sets[r.Trajectory.CapabilitiesHash]++
	}
	if recorded == 0 {
		return ""
	}
	if len(sets) == 1 {
		for hash := range sets {
			return fmt.Sprintf("**Capabilities**: one set across %d runs (%s)\n\n", recorded, short(hash))
		}
	}
	parts := make([]string, 0, len(sets))
	for hash, n := range sets {
		parts = append(parts, fmt.Sprintf("%s ×%d", short(hash), n))
	}
	slices.Sort(parts)
	return fmt.Sprintf("**Capabilities**: %d different sets across %d runs — not one version: %s\n\n", len(sets), recorded, strings.Join(parts, " · "))
}

func short(hash string) string {
	hash = strings.TrimPrefix(hash, "sha256:")
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}
