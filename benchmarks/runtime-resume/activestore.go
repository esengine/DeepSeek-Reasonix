package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"reasonix/internal/agentgraph"
	"reasonix/internal/control"
	"reasonix/internal/execjournal"
)

// armFleetActiveStore asks whether the sub-agent store can be asked about a
// fan-out item whose slot was granted and whose child reached the provider. It
// is not a parity arm: the store is what every recovery path reads, so what is
// measured is one store answering differently about executions it holds the
// same evidence for.
const armFleetActiveStore = "fleet-active-store"

// activeFleetCallID is the fan-out under measurement. The lone delegation in
// the same turn keeps taskCallID, so the two entry points stay separable in
// every artifact without a naming scheme of their own.
const activeFleetCallID = "probe_fleet_active"

// probeFleetChildEntered is the fan-out's half of the entry evidence. The lone
// delegation's rides probeChildEntered, which already means exactly this for
// the delegation under measurement.
const probeFleetChildEntered = "probe:fleet-child-entered"

// The four populations one death holds. They are read off the journal rather
// than off the item ids the dispatch declared: which of the two held-back items
// wins the freed slot is the scheduler's to decide, and an arm that named them
// in advance would be reporting a race as a premise.
type population string

const (
	popSettled population = "fan-out settled"
	popRunning population = "fan-out executing"
	popRefused population = "fan-out refused"
	popLone    population = "lone executing"
	popNone    population = ""
)

// measured are the four in the order they are reported, which is also the order
// they become true: an item settles, the slot it frees admits one of the two
// behind it, and the delegation holding the other slot outlives all of them.
func measured() []population { return []population{popSettled, popRunning, popRefused, popLone} }

// populationOf places one journal entry in its population, or in none. Started
// and open is executing; started and settled is done; queued without a start is an
// item the scheduler held back and never admitted.
func populationOf(e execjournal.Entry) population {
	switch {
	case ofLoneTask(e.ID):
		if e.Started() && e.Open() {
			return popLone
		}
	case ofActiveFleet(e.ID):
		switch {
		case e.Started() && !e.Open():
			return popSettled
		case e.Started() && e.Open():
			return popRunning
		case !e.Started() && e.Queued():
			return popRefused
		}
	}
	return popNone
}

func ofActiveFleet(id string) bool { return strings.HasPrefix(id, activeFleetCallID+"/") }

// executionsIn are the ids this observation's journal places in a population,
// in a stable order.
func executionsIn(o Observation, pop population) []string {
	var out []string
	for _, e := range o.Executions {
		if populationOf(e) == pop {
			out = append(out, e.ID)
		}
	}
	sort.Strings(out)
	return out
}

// storedStatus is what the store says about one execution, empty when it has
// never heard of it. The join is the execution id the child recorded as its
// parent call, which is the same identity the journal opened it under.
func storedStatus(o Observation, id string) string {
	for _, f := range o.Children.Facts {
		if f.ParentToolCallID == id {
			return f.Status
		}
	}
	return ""
}

// noRecord is what the store says about an execution it never recorded. It is
// spelled out rather than left blank because a blank column reads as a row that
// was not measured, which is the opposite of what an absent record means here.
const noRecord = "no record"

func storeReading(o Observation, ids []string) string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		status := storedStatus(o, id)
		if status == "" {
			status = noRecord
		}
		out = append(out, id+"→"+status)
	}
	return orNone(join(out))
}

// acknowledged reports whether the store holds a record for every one of these
// executions. An empty set is not acknowledgement: a population the death never
// produced is a premise that failed, and its row says not-measured.
func acknowledged(o Observation, ids []string) bool {
	for _, id := range ids {
		if storedStatus(o, id) == "" {
			return false
		}
	}
	return len(ids) > 0
}

func activeStoreArm(name string) bool { return name == armFleetActiveStore }

// activeStoreRows read one store from four sides. Each population has the same
// two columns — what the dying process's store said, and what the next one's
// does — so the answers can only differ by which execution is being asked about.
func activeStoreRows(before, after Observation) []row {
	return []row{
		populationsRow(before),
		childEntryRow(before),
		settledStoreRow(before, after),
		executingStoreRow(before, after),
		loneStoreRow(before, after),
		refusedStoreRow(before, after),
		listingRow(before, after),
		rebuildUnmovedRow(before, after),
	}
}

// populationsRow is the arm's premise, not a durability claim: one death has to
// hold all four states at once, or the comparisons below are between different
// runs rather than between entry points.
func populationsRow(before Observation) row {
	var got []string
	for _, pop := range measured() {
		got = append(got, fmt.Sprintf("%s=%d", pop, len(executionsIn(before, pop))))
	}
	return row{
		Semantic: "the four populations this death holds", Authority: journalAuthority,
		Artifact: "<stem>.execution.jsonl", Reconstruction: "in-process only",
		Before: join(got), After: "—", Verdict: activePremiseVerdict(before),
	}
}

func activePremiseVerdict(before Observation) string {
	if activeStoreInvalid(before) != "" {
		return verdictNotMeasured
	}
	return verdictHolds
}

// childEntryRow is the half of the evidence no artifact carries. A slot grant
// says the scheduler admitted the item; only the child's own arrival at the
// provider says the execution happened, and a store row read without it would
// be asking about work that may never have run.
func childEntryRow(before Observation) row {
	fleet := firstProgress(before, probeFleetChildEntered)
	lone := firstProgress(before, probeChildEntered)
	return row{
		Semantic: "the children that reached the provider", Authority: "the scripted provider, in-process",
		Artifact: "none observed", Reconstruction: "in-process only",
		Before:  fmt.Sprintf("fan-out=%s lone=%s", fleet, lone),
		After:   "—",
		Verdict: enteredVerdict(fleet == "true" && lone == "true"),
	}
}

func enteredVerdict(both bool) string {
	if both {
		return verdictHolds
	}
	return verdictNotMeasured
}

func firstProgress(o Observation, key string) string {
	if vals := o.Progress[key]; len(vals) > 0 {
		return vals[0]
	}
	return "unrecorded"
}

// settledStoreRow is the control that makes the rest readable. It is the same
// entry point, the same fan-out and the same store, differing only in that this
// item finished — so if it too were absent, the finding would be about
// persistence in general rather than about executions in flight.
func settledStoreRow(before, after Observation) row {
	ids := executionsIn(before, popSettled)
	return row{
		Semantic: "a settled fan-out item, in the store", Authority: storeAuthority,
		Artifact: "subagents/<ref>.meta.json", Reconstruction: "ListSubagentsByParent, joined by execution id",
		Before:  storeReading(before, ids),
		After:   storeReading(after, ids),
		Verdict: controlVerdict(acknowledged(before, ids) && acknowledged(after, ids), len(ids) > 0),
	}
}

// executingStoreRow is the arm. The conjunction is stated in the row rather
// than left to the reader: the journal proves the slot was granted, the child
// proves the execution happened, and this column is what the store can say
// about it. All three, or the row is not the finding it looks like.
func executingStoreRow(before, after Observation) row {
	ids := executionsIn(before, popRunning)
	entered := firstProgress(before, probeFleetChildEntered) == "true"
	return row{
		Semantic: "an executing fan-out item, in the store", Authority: storeAuthority,
		Artifact:       "subagents/<ref>.meta.json",
		Reconstruction: "STARTED in the journal, entered at the provider, and asked of the store",
		Before:         storeReading(before, ids),
		After:          storeReading(after, ids),
		Verdict:        executingVerdict(before, after, ids, entered),
	}
}

// executingVerdict refuses the two readings that would overstate the row. An
// execution nothing proves entered is not a silent store, and a population the
// death never produced is not evidence of anything.
func executingVerdict(before, after Observation, ids []string, entered bool) string {
	switch {
	case len(ids) == 0 || !entered:
		return verdictNotMeasured
	case acknowledged(before, ids) || acknowledged(after, ids):
		return verdictHolds
	default:
		return verdictViolated
	}
}

// loneStoreRow is the other entry point, in the same death and the same store.
// It is what makes "selectively blind" a statement about the store rather than
// a comparison between two probe runs.
func loneStoreRow(before, after Observation) row {
	ids := executionsIn(before, popLone)
	return row{
		Semantic: "an executing lone delegation, in the store", Authority: storeAuthority,
		Artifact: "subagents/<ref>.meta.json", Reconstruction: "the same read, one entry point over",
		Before:  storeReading(before, ids),
		After:   storeReading(after, ids),
		Verdict: controlVerdict(acknowledged(before, ids) && acknowledged(after, ids), len(ids) > 0),
	}
}

// refusedStoreRow is the negative control, and it bounds any fix this arm might
// support. An item the scheduler never admitted produced nothing, so the store
// is owed no record for it — and a change that recorded every opening would
// satisfy the row above by reintroducing exactly that.
func refusedStoreRow(before, after Observation) row {
	ids := executionsIn(before, popRefused)
	silent := !acknowledged(before, ids) && !acknowledged(after, ids)
	return row{
		Semantic: "a refused fan-out item, in the store", Authority: crossAuthority,
		Artifact:       "subagents/<ref>.meta.json, <stem>.execution.jsonl",
		Reconstruction: "queued, never started: the store must stay silent",
		Before:         storeReading(before, ids),
		After:          storeReading(after, ids),
		Verdict:        controlVerdict(silent, len(ids) > 0),
	}
}

func controlVerdict(ok bool, measured bool) string {
	switch {
	case !measured:
		return verdictNotMeasured
	case ok:
		return verdictHolds
	default:
		return verdictViolated
	}
}

// listingRow is the surface a person or a model reaches for. It reads through
// the workspace filter and the lineage walk the tool applies, which the store
// read above does not — so it is checked against that read first: a listing
// that cannot even see the settled item is a filter answering, not a store.
func listingRow(before, after Observation) row {
	settled := executionsIn(before, popSettled)
	return row{
		Semantic: "what list_subagents would show", Authority: "SubagentStore.ListForParent",
		Artifact:       "subagents/<ref>.meta.json",
		Reconstruction: "the read behind the tool, filtered by workspace and lineage",
		Before:         listingReading(before),
		After:          listingReading(after),
		Verdict:        listingVerdict(before, after, settled),
	}
}

func listingReading(o Observation) string {
	if o.Listing.Err != "" {
		return "error: " + o.Listing.Err
	}
	out := make([]string, 0, len(o.Listing.Rows))
	for _, r := range o.Listing.Rows {
		out = append(out, orDash(r.From)+"="+r.Status)
	}
	sort.Strings(out)
	return orNone(join(out))
}

// listingVerdict answers only once the listing has been shown to work. Its
// filters can drop every row for reasons that have nothing to do with this arm,
// and an empty listing read as an invisible execution is the substitution the
// probe exists to refuse.
func listingVerdict(before, after Observation, settled []string) string {
	if len(settled) == 0 || !listingHas(after, settled) {
		return verdictNotMeasured
	}
	if listingHas(before, executionsIn(before, popRunning)) {
		return verdictHolds
	}
	return verdictViolated
}

func listingHas(o Observation, ids []string) bool {
	for _, id := range ids {
		found := false
		for _, r := range o.Listing.Rows {
			if r.From == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return len(ids) > 0
}

// rebuildUnmovedRow pins what a fix must not move. A store that learned to
// record executions in flight would leave stale records behind for a restart to
// fold, and the one outcome this whole line of work exists to prevent is a
// rebuild that answers with a node the dead process was running.
func rebuildUnmovedRow(before, after Observation) row {
	rebuilt := rebuiltGraph(after)
	var ghosts []string
	for _, n := range rebuilt.Graph.Nodes {
		if n.State == agentgraph.StateRunning {
			ghosts = append(ghosts, n.ID)
		}
	}
	var cut []string
	for _, i := range rebuilt.Interrupted {
		if ofActiveFleet(i.Execution) || ofLoneTask(i.Execution) {
			cut = append(cut, i.Execution+":"+interruptionKind(i))
		}
	}
	sort.Strings(ghosts)
	sort.Strings(cut)
	return row{
		Semantic: "no ghost, and how the rebuild names the cut", Authority: rebuildAuthority,
		Artifact:       "<stem>.execution.jsonl + subagents/<ref>.meta.json",
		Reconstruction: "execgraph.Rebuild over both, after the death",
		Before:         fmt.Sprintf("%d executing at death", len(executionsIn(before, popRunning))+len(executionsIn(before, popLone))),
		After:          fmt.Sprintf("ghosts=%s cut=%s", orNone(join(ghosts)), orNone(join(cut))),
		Verdict:        controlVerdict(len(ghosts) == 0 && len(cut) > 0, len(before.Executions) > 0),
	}
}

// activeStoreInvalid reports the premise this arm could not establish. All four
// populations in one death is the whole design: without them the rows compare
// different runs, and a row that compares nothing must not be read as green.
func activeStoreInvalid(before Observation) string {
	for _, pop := range measured() {
		if len(executionsIn(before, pop)) == 0 {
			return "the death held nothing in population " + string(pop)
		}
	}
	if firstProgress(before, probeFleetChildEntered) != "true" {
		return "no fan-out child reached the provider, so no execution is proven to have happened"
	}
	return ""
}

// runActiveStoreConstruct drives one delegation and one fan-out under a ceiling
// they share, and dies with all four populations standing. The delegation goes
// first and hangs: it is what keeps the ceiling from emptying under the fan-out,
// so the item refused admission stays refused rather than starting a moment
// later and taking the arm's negative control with it.
func runActiveStoreConstruct(ctx context.Context, root armRoot, arm, bootSystem string, ctrl *control.Controller, sink *graphSink, prov *scripted, turn int) error {
	prompt := fanOutTurn(turn+1, taskSentinel+" "+fleetActiveSentinel)
	go func() { _ = ctrl.Run(ctx, prompt) }()
	if err := waitForPopulations(ctrl, prov); err != nil {
		return err
	}
	obs := capture("construct", arm, bootSystem, ctrl, sink, root)
	// Whether each child reached the provider is known only here, and it is half
	// of what makes a silent store a finding rather than a missing record for
	// work that never ran.
	obs.Progress[probeFleetChildEntered] = []string{fmt.Sprint(prov.fleets.held(childEntered(childHang)))}
	obs.Progress[probeChildEntered] = []string{fmt.Sprint(prov.fleets.held(childEntered(childHold)))}
	if err := writeObservation(root, obs); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// waitForPopulations blocks until the journal holds all four states, then holds
// still and asks again. A refusal is a state nothing announces, so the second
// reading is what separates an item the scheduler is holding back from one that
// was merely slow to be admitted.
func waitForPopulations(ctrl *control.Controller, prov *scripted) error {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if populationsStanding(ctrl, prov) {
			time.Sleep(2 * time.Second)
			if !populationsStanding(ctrl, prov) {
				return errUnexpected("a refusal that was still standing",
					journalStates(control.ExecutionHistory(ctrl.SessionPath())))
			}
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errUnexpected("one death holding all four populations",
		journalStates(control.ExecutionHistory(ctrl.SessionPath())))
}

func populationsStanding(ctrl *control.Controller, prov *scripted) bool {
	held := map[population]bool{}
	for _, e := range control.ExecutionHistory(ctrl.SessionPath()) {
		held[populationOf(e)] = true
	}
	return held[popSettled] && held[popRunning] && held[popRefused] && held[popLone] &&
		prov.fleets.held(childEntered(childHang)) && prov.fleets.held(childEntered(childHold))
}

func journalStates(entries []execjournal.Entry) string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, fmt.Sprintf("%s:started=%t,queued=%t,open=%t", e.ID, e.Started(), e.Queued(), e.Open()))
	}
	sort.Strings(out)
	return orNone(join(out))
}
