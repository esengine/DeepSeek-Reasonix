package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type costRow struct {
	scale    string
	detector string
	p50, p95 time.Duration
	files    int
	bytes    int64
}

// measureCost times each detector over synthetic workspaces of a given size and,
// when asked, over a real one. The synthetic skills are shaped like real ones —
// a directory per skill holding SKILL.md — because the walk is what is being
// measured and a flat directory would not have it.
func measureCost(scales []int, samples int, realRoot string) []costRow {
	var rows []costRow
	for _, n := range scales {
		root, err := os.MkdirTemp("", "catalog-detector-*")
		fatal(err)
		defer os.RemoveAll(root)
		fatal(writeSkills(filepath.Join(root, "project", ".reasonix", "skills"), n))
		// A separate home: pointing both at one directory walks it twice and
		// reports double the cost a real workspace pays.
		rows = append(rows, timeDetectors(fmt.Sprintf("synthetic n=%d", n),
			env{projectRoot: filepath.Join(root, "project"), homeDir: filepath.Join(root, "home"), apiWrites: new(int)}, samples)...)
	}
	if realRoot != "" {
		home, err := os.UserHomeDir()
		fatal(err)
		rows = append(rows, timeDetectors("real workspace", env{projectRoot: realRoot, homeDir: home, apiWrites: new(int)}, samples)...)
	}
	return rows
}

func timeDetectors(scale string, e env, samples int) []costRow {
	var rows []costRow
	files, bytes := walkCost(e.roots())
	for _, d := range detectors {
		var durations []time.Duration
		for range samples {
			start := time.Now()
			d.read(e)
			durations = append(durations, time.Since(start))
		}
		row := costRow{scale: scale, detector: d.name, p50: percentile(durations, 0.5), p95: percentile(durations, 0.95)}
		if d.walks {
			row.files, row.bytes = files, bytes
		}
		rows = append(rows, row)
	}
	return rows
}

func writeSkills(dir string, n int) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for i := range n {
		sub := filepath.Join(dir, fmt.Sprintf("probe-%03d", i))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(sub, "SKILL.md"), []byte(skillFile(i, fmt.Sprintf("probe skill %d, one line of index text.", i))), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func skillFile(i int, description string) string {
	return fmt.Sprintf("---\nname: probe-%03d\ndescription: %s\n---\n\nBody paragraph the index never renders.\n", i, description)
}

func renderCost(rows []costRow) string {
	var b strings.Builder
	b.WriteString("cost — one reading, as a turn would take it\n\n")
	fmt.Fprintf(&b, "%-16s %-15s %10s %10s %7s %9s\n", "scale", "detector", "p50", "p95", "files", "bytes")
	for _, r := range rows {
		fmt.Fprintf(&b, "%-16s %-15s %10s %10s %7d %9d\n", r.scale, r.detector, r.p50.Round(time.Microsecond), r.p95.Round(time.Microsecond), r.files, r.bytes)
	}
	return b.String()
}
