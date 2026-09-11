// Package ablation switches individual Reasonix subsystems off so a benchmark
// can attribute a change in solve rate to one of them.
package ablation

import (
	"fmt"
	"sort"
	"strings"
)

type Module string

const (
	Evidence   Module = "evidence"
	Planner    Module = "planner"
	Subagent   Module = "subagent"
	Retrieval  Module = "retrieval"
	Compaction Module = "compaction"
	// FullFold off means a fold reads the previous projection instead of
	// re-deriving its digest from the canonical transcript.
	FullFold Module = "full-fold"
)

// Modules returns every switchable module in the order arm names use.
func Modules() []Module {
	return []Module{Evidence, Planner, Subagent, Retrieval, Compaction, FullFold}
}

// Set is the group of modules disabled for a run. The zero value is the
// control arm: everything on.
type Set struct {
	off map[Module]bool
}

// Parse reads a spec such as "evidence,planner". "" and "none" mean the control
// arm; "all" disables every module.
func Parse(spec string) (Set, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" || strings.EqualFold(spec, "none") {
		return Set{}, nil
	}
	if strings.EqualFold(spec, "all") {
		return New(Modules()...), nil
	}
	known := map[Module]bool{}
	for _, m := range Modules() {
		known[m] = true
	}
	var mods []Module
	for _, field := range strings.FieldsFunc(spec, func(r rune) bool { return r == ',' || r == ' ' }) {
		m := Module(strings.ToLower(strings.TrimSpace(field)))
		if !known[m] {
			return Set{}, fmt.Errorf("unknown ablation module %q (want %s, or none/all)", field, joinModules(Modules(), ", "))
		}
		mods = append(mods, m)
	}
	return New(mods...), nil
}

// New returns a Set with the given modules disabled.
func New(mods ...Module) Set {
	if len(mods) == 0 {
		return Set{}
	}
	off := make(map[Module]bool, len(mods))
	for _, m := range mods {
		off[m] = true
	}
	return Set{off: off}
}

func (s Set) Off(m Module) bool { return s.off[m] }

func (s Set) Empty() bool { return len(s.off) == 0 }

// Arm is the published name of this configuration: "full" for the control arm,
// otherwise "no-evidence+no-planner". Stable across runs so results from
// different machines group by the same key.
func (s Set) Arm() string {
	if s.Empty() {
		return "full"
	}
	parts := make([]string, 0, len(s.off))
	for _, m := range s.disabled() {
		parts = append(parts, "no-"+string(m))
	}
	return strings.Join(parts, "+")
}

// String round-trips back through Parse.
func (s Set) String() string {
	if s.Empty() {
		return "none"
	}
	return joinModules(s.disabled(), ",")
}

func (s Set) disabled() []Module {
	order := map[Module]int{}
	for i, m := range Modules() {
		order[m] = i
	}
	out := make([]Module, 0, len(s.off))
	for m := range s.off {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return order[out[i]] < order[out[j]] })
	return out
}

func joinModules(mods []Module, sep string) string {
	parts := make([]string, len(mods))
	for i, m := range mods {
		parts[i] = string(m)
	}
	return strings.Join(parts, sep)
}
