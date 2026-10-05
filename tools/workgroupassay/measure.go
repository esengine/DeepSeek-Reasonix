package main

import (
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/contract/event"
	"reasonix/internal/state/workgroup"
)

// assay is the five numbers the protocol asks for plus the two breakdowns that
// describe the sample. Computed apart from printing so the verdict can be
// checked against a sample built by hand — an instrument that has never
// produced a number is one nobody has verified.
type assay struct {
	Turns         int
	Groups        int
	Singletons    int
	RichTurns     int
	ZeroCallTurns int
	OneCallTurns  int
	Barriers      int
	GroupsPerTurn []int
	CallsPerGroup []int
	BySource      map[string][]int
	Routes        map[string]int
	// Illegal is the partition defect that stopped the assay. A sample this
	// fold cannot legally partition is not a sample to draw a verdict from.
	Illegal string
}

func (a assay) singletonShare() float64 { return share(a.Singletons, a.Groups) }
func (a assay) richShare() float64      { return share(a.RichTurns, a.Turns) }

// noGo is the frozen gate, both halves required: a partition can be coarse and
// still rare, or common and still thin, and those are different products.
func (a assay) noGo() bool {
	return a.singletonShare() > singletonShareGate && a.richShare() < richTurnShareGate
}

// assess folds every turn in the sample and counts. It refuses rather than
// reports when a turn cannot be legally partitioned.
func assess(sample []turn, rules workgroup.Rules) assay {
	a := assay{Turns: len(sample), BySource: map[string][]int{}, Routes: map[string]int{}}
	for _, t := range sample {
		calls, gs := workgroup.Fold(t.frames, rules)
		if bad := workgroup.DuplicateMembership(gs); bad != "" {
			a.Illegal = fmt.Sprintf("%s %s: %s", t.session, turnName(t), bad)
			return a
		}
		if bad := workgroup.SealedOverAnOpenCall(gs); bad != "" {
			a.Illegal = fmt.Sprintf("%s %s sealed over a running call: %s", t.session, turnName(t), bad)
			return a
		}
		a.Routes[routeOf(t)]++
		a.Barriers += barriersIn(calls)
		members, rich := 0, false
		for _, g := range gs {
			a.Groups++
			n := len(g.Members)
			a.CallsPerGroup = append(a.CallsPerGroup, n)
			a.BySource[g.Source] = append(a.BySource[g.Source], n)
			members += n
			if n == 1 {
				a.Singletons++
			}
			if n >= richTurnCalls {
				rich = true
			}
		}
		if rich {
			a.RichTurns++
		}
		a.GroupsPerTurn = append(a.GroupsPerTurn, len(gs))
		switch members {
		case 0:
			a.ZeroCallTurns++
		case 1:
			a.OneCallTurns++
		}
	}
	return a
}

// measure prints what assess found and the frozen verdict. Nothing else: an
// assay that grows extra numbers after seeing the first ones is choosing which
// of them to believe.
func measure(sample []turn) {
	a := assess(sample, workgroup.V1())
	if a.Illegal != "" {
		fmt.Printf("REFUSED: the sample holds a turn this fold cannot legally partition: %s\n", a.Illegal)
		return
	}
	fmt.Printf("\nsample: %d authored turns, routes %s, %d barrier(s)\n",
		a.Turns, describe(a.Routes), a.Barriers)
	fmt.Printf("\n1  eligible authored turns          %d\n", a.Turns)
	fmt.Printf("2  workgroups per authored turn     median %.1f  %s\n",
		median(a.GroupsPerTurn), histogram(a.GroupsPerTurn))
	fmt.Printf("3  calls per workgroup              median %.1f  %s\n",
		median(a.CallsPerGroup), histogram(a.CallsPerGroup))
	fmt.Printf("4  singleton workgroups             %d of %d = %.0f%%\n",
		a.Singletons, a.Groups, 100*a.singletonShare())
	fmt.Printf("5  turns with a %d+ call workgroup   %d of %d = %.0f%%\n",
		richTurnCalls, a.RichTurns, a.Turns, 100*a.richShare())

	fmt.Printf("\nsecondary, not part of the verdict\n")
	for _, src := range sortedKeys(a.BySource) {
		fmt.Printf("   %-10s groups %d, median size %.1f\n", src, len(a.BySource[src]), median(a.BySource[src]))
	}
	fmt.Printf("   turns with exactly one call      %d\n", a.OneCallTurns)

	// Reported, never gated: adding it to the gate after a sample has been seen
	// is redrawing the line, and leaving it out lets a low singleton rate read
	// as broad reach when a fold might touch half the turns.
	fmt.Printf("\ncoverage — reported, not gated\n")
	fmt.Printf("   turns with no assistant-owned call  %d of %d = %.0f%%\n",
		a.ZeroCallTurns, a.Turns, 100*share(a.ZeroCallTurns, a.Turns))

	fmt.Printf("\nagainst P2-2d-designed (n=%d, %s)\n", designedReference.Turns, designedReference.Note)
	fmt.Printf("   singleton workgroups   %+.0f pp   (%.0f%% here, %.0f%% there)\n",
		100*(a.singletonShare()-designedReference.SingletonShare),
		100*a.singletonShare(), 100*designedReference.SingletonShare)
	fmt.Printf("   turns with a %d+ group  %+.0f pp   (%.0f%% here, %.0f%% there)\n", richTurnCalls,
		100*(a.richShare()-designedReference.RichTurnShare),
		100*a.richShare(), 100*designedReference.RichTurnShare)
	fmt.Printf("   These are two sampling mechanisms, neither randomized nor paired,\n")
	fmt.Printf("   both small. The difference is the whole claim; it is not a test.\n")

	if designedSample {
		fmt.Printf("\nthis sample was designed, not observed: the turns come from a written task\n")
		fmt.Printf("list, so the group sizes carry its author's idea of what an agent does.\n")
	}
	fmt.Printf("\nverdict (frozen before the sample existed)\n")
	if a.noGo() {
		fmt.Printf("   NO-GO: most groups hold one call AND few turns have %d+ to fold.\n", richTurnCalls)
		fmt.Printf("   Drop the first-layer execution fold; make tool rows compact instead.\n")
		return
	}
	fmt.Printf("   The NO-GO gate did not trigger. That is not an automatic go:\n")
	fmt.Printf("   read the distributions above and the group shapes before building anything.\n")
}

func turnName(t turn) string { return fmt.Sprintf("authored turn %d", t.authored) }

// routeOf describes a turn by which producers worked in it, which is a sample
// description and never a selection criterion.
func routeOf(t turn) string {
	for _, e := range t.frames {
		if e.Source == string(event.UsageSourcePlanner) {
			return "plan_and_execute"
		}
	}
	return "executor_only"
}

func barriersIn(calls []workgroup.Call) int {
	n := 0
	for _, c := range calls {
		n += len(c.Interruptions)
	}
	return n
}

func share(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return float64(n) / float64(of)
}

func median(in []int) float64 {
	if len(in) == 0 {
		return 0
	}
	s := append([]int(nil), in...)
	slices.Sort(s)
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return float64(s[mid])
	}
	return float64(s[mid-1]+s[mid]) / 2
}

// histogram shows the whole shape, because a median hides whether a sample is
// two clusters or one.
func histogram(in []int) string {
	if len(in) == 0 {
		return "(none)"
	}
	counts := map[int]int{}
	for _, v := range in {
		counts[v]++
	}
	keys := make([]int, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%d×%d", counts[k], k))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func describe(counts map[string]int) string {
	parts := make([]string, 0, len(counts))
	for _, k := range sortedCountKeys(counts) {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, " ")
}

func sortedKeys(m map[string][]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func sortedCountKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
