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
	// Upstream off means a fleet dependency edge orders its endpoints without
	// delivering the dependency's answer, so a dependent must re-derive it.
	Upstream Module = "upstream"
	// RecallSearch off leaves recall able to read an address it was given and
	// unable to look one up, which is what the tool could do before the folded
	// region became searchable.
	RecallSearch Module = "recall-search"
)

// FoldIndexScale sizes the model-visible fold index against its shipped budget.
// It is a bench axis, not a module: the question it answers is what an ambient
// address list is worth, and four steps answer that where a free-form fraction
// would turn a measurement into a tuning knob.
type FoldIndexScale string

const (
	FoldIndexDefault FoldIndexScale = ""
	FoldIndexHalf    FoldIndexScale = "half"
	FoldIndexQuarter FoldIndexScale = "quarter"
	FoldIndexOff     FoldIndexScale = "off"
)

// Ratio scales a budget. The default returns it unchanged.
func (f FoldIndexScale) Ratio() float64 {
	switch f {
	case FoldIndexHalf:
		return 0.5
	case FoldIndexQuarter:
		return 0.25
	case FoldIndexOff:
		return 0
	default:
		return 1
	}
}

// ParseFoldIndexScale reads a bench flag value. "" and "default" ship as-is.
func ParseFoldIndexScale(spec string) (FoldIndexScale, error) {
	switch strings.ToLower(strings.TrimSpace(spec)) {
	case "", "default", "full":
		return FoldIndexDefault, nil
	case string(FoldIndexHalf):
		return FoldIndexHalf, nil
	case string(FoldIndexQuarter):
		return FoldIndexQuarter, nil
	case string(FoldIndexOff), "none", "0":
		return FoldIndexOff, nil
	default:
		return "", fmt.Errorf("unknown fold-index scale %q (want default, half, quarter, or off)", spec)
	}
}

// Modules returns every switchable module in the order arm names use.
func Modules() []Module {
	return []Module{Evidence, Planner, Subagent, Retrieval, Compaction, Upstream, RecallSearch}
}

// Set is the group of modules disabled for a run. The zero value is the
// control arm: everything on.
type Set struct {
	off map[Module]bool
	// foldIndex is an axis rather than a switch, so it lives beside the module
	// set instead of inside it. It still reaches Arm(): a run whose index was
	// quartered must not report the same identity as the control.
	foldIndex FoldIndexScale
}

// WithFoldIndex returns this set at the given index scale.
func (s Set) WithFoldIndex(scale FoldIndexScale) Set {
	s.foldIndex = scale
	return s
}

// FoldIndex is the scale this run sizes its fold index at.
func (s Set) FoldIndex() FoldIndexScale { return s.foldIndex }

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
			return Set{}, fmt.Errorf("unknown ablation module %q (want %s, or none/all)", field, ModuleList())
		}
		mods = append(mods, m)
	}
	return New(mods...), nil
}

// ParseArm reads both bench axes at once: the module spec and the fold-index
// scale. One call so a frontend cannot wire the first and forget the second,
// which would silently run an index arm under the control arm's name.
func ParseArm(modules, foldIndex string) (Set, error) {
	set, err := Parse(modules)
	if err != nil {
		return Set{}, err
	}
	scale, err := ParseFoldIndexScale(foldIndex)
	if err != nil {
		return Set{}, err
	}
	return set.WithFoldIndex(scale), nil
}

// ModuleList names every switchable module for flag help and error text, so
// those cannot drift from Modules() the way a hand-kept list does.
func ModuleList() string { return joinModules(Modules(), ", ") }

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

func (s Set) Empty() bool { return len(s.off) == 0 && s.foldIndex == FoldIndexDefault }

// Arm is the published name of this configuration: "full" for the control arm,
// otherwise "no-evidence+no-planner". Stable across runs so results from
// different machines group by the same key.
func (s Set) Arm() string {
	if s.Empty() {
		return "full"
	}
	parts := make([]string, 0, len(s.off)+1)
	for _, m := range s.disabled() {
		parts = append(parts, "no-"+string(m))
	}
	if s.foldIndex != FoldIndexDefault {
		parts = append(parts, "index-"+string(s.foldIndex))
	}
	return strings.Join(parts, "+")
}

// String round-trips back through Parse. It carries modules only — the fold
// index axis has its own flag and its own parser.
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
