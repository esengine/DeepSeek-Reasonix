package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/skill"
)

type verdict struct {
	arm      string
	comment  string
	owed     bool
	detector string
	changed  bool
}

func (v verdict) label() string {
	switch {
	case v.owed == v.changed:
		return "correct"
	case v.owed:
		return "MISS"
	default:
		return "false-positive"
	}
}

// measureVerdicts runs every arm against a fresh workspace and records, for each
// detector, whether it saw the transition. Truth is the rendered block itself:
// the arm is owed when the listing the model would receive is not the listing it
// already has.
func measureVerdicts() []verdict {
	var out []verdict
	for _, a := range arms {
		root, err := os.MkdirTemp("", "catalog-detector-arm-*")
		fatal(err)
		defer os.RemoveAll(root)
		fatal(writeSkills(filepath.Join(root, "project", ".reasonix", "skills"), 3))
		e := env{projectRoot: filepath.Join(root, "project"), homeDir: filepath.Join(root, "home"), apiWrites: new(int)}

		before := map[string]string{}
		for _, d := range detectors {
			before[d.name] = d.read(e)
		}
		blockBefore := skill.IndexBlock(e.store().List())

		fatal(a.apply(e))

		owed := skill.IndexBlock(e.store().List()) != blockBefore
		for _, d := range detectors {
			out = append(out, verdict{a.name, a.comment, owed, d.name, d.read(e) != before[d.name]})
		}
	}
	return out
}

func renderVerdicts(vs []verdict) string {
	var b strings.Builder
	b.WriteString("verdicts — owed is measured, not declared: the rendered block differs\n\n")
	fmt.Fprintf(&b, "%-18s %-6s %-15s %-15s %s\n", "arm", "owed", "detector", "verdict", "what the arm is")
	for _, v := range vs {
		fmt.Fprintf(&b, "%-18s %-6v %-15s %-15s %s\n", v.arm, v.owed, v.detector, v.label(), v.comment)
	}
	return b.String()
}

func failed(vs []verdict) bool {
	for _, v := range vs {
		if v.label() != "correct" {
			return true
		}
	}
	return false
}
