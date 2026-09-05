package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
)

// The skill-runner classification arms. They ask what kind of execution a
// runAs=subagent skill is, because the answer decides whether it belongs to the
// delegation class the task and fan-out arms established or is an execution
// class of its own. Nothing here is a fix: a classification that guessed would
// be answered by whichever plumbing happened to exist.
const (
	// armSkillCompleted lets a skill finish and the turn close, then asks what
	// each layer kept. It runs both entry points — the tool a model calls and
	// the controller's own — because their lineage differs by design and a row
	// that read one for the other would report intent as a defect.
	armSkillCompleted = "skill-completed"
	// armSkillRunning kills the process with the child mid-execution: the death
	// every other delegation arm takes.
	armSkillRunning = "skill-running"
	// armSkillQueued fills the ceiling first. It is the arm that says whether a
	// skill belongs to the same admission domain as everything else, which is a
	// different question from whether it leaves the same records.
	armSkillQueued = "skill-queued"
	// armSkillCancel stops a running skill through the turn that admitted it, so
	// the stop has the owner a person's Stop has.
	armSkillCancel = "skill-cancel"
	// The read-only entry point is a separate surface, not a lighter version of
	// the one above: it prepares no transcript and refuses continuation, so
	// asking it for durability would be demanding it break its own contract.
	// These three ask what it is instead.
	armReadOnlySkillCompleted = "readonly-skill-completed"
	armReadOnlySkillRunning   = "readonly-skill-running"
	armReadOnlySkillQueued    = "readonly-skill-queued"
)

func skillArm(name string) bool {
	switch name {
	case armSkillCompleted, armSkillRunning, armSkillQueued, armSkillCancel:
		return true
	}
	return readOnlySkillArm(name)
}

func readOnlySkillArm(name string) bool {
	switch name {
	case armReadOnlySkillCompleted, armReadOnlySkillRunning, armReadOnlySkillQueued:
		return true
	}
	return false
}

func queuedSkillArm(name string) bool {
	return name == armSkillQueued || name == armReadOnlySkillQueued
}

// runningSkillArm names the arms that die or are stopped with the child inside
// the provider, which is the only state a durable record could be missing from.
func runningSkillArm(name string) bool {
	return name == armSkillRunning || name == armSkillCancel || name == armReadOnlySkillRunning
}

const (
	skillSentinel = "PROBE-SKILL"
	// skillCallID is the tool call the model makes. It is the identity the
	// store would join to, so the arm names it rather than letting the round
	// pick one.
	skillCallID = "probe_skill"
	// probeSkillName is the skill on disk. Not a built-in: those carry their own
	// read-only promises and review contracts, and the arm needs a plain writer
	// so the scheduler sees a claim rather than a reader.
	probeSkillName = "probe-worker"
	// hostSkillTask is what the controller's own entry point is asked to do. It
	// completes, because that arm compares two lineages rather than two deaths.
	hostSkillTask = childDone + " host-initiated skill"
)

// writeProbeSkill puts a runAs=subagent skill in the workspace, where a project
// skill lives. It is a writer: a read-only one would clear the writer and claim
// checks by not being subject to them, and the arm could not say whether a skill
// is admitted the way every other writer is.
func writeProbeSkill(workspace string) error {
	dir := filepath.Join(workspace, ".reasonix", skill.SkillsDirname, probeSkillName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body := "---\nname: " + probeSkillName + "\n" +
		"description: The probe's own subagent, which answers whatever task it is handed.\n" +
		"runAs: subagent\n---\n\nAnswer the task you are given and stop.\n"
	return os.WriteFile(filepath.Join(dir, skill.SkillFile), []byte(body), 0o644)
}

// skillCall is the dispatch under measurement, made through the tool a model
// calls. The read-only arms enter through the other tool, which is a different
// runner rather than a flag on this one.
func skillCall(arm string) []provider.Chunk {
	name := "run_skill"
	if readOnlySkillArm(arm) {
		name = "read_only_skill"
	}
	task := skillChildSentinel(arm) + " skill body"
	args, _ := json.Marshal(map[string]any{"name": probeSkillName, "arguments": task})
	return []provider.Chunk{{
		Type:     provider.ChunkToolCall,
		ToolCall: &provider.ToolCall{ID: skillCallID, Name: name, Arguments: string(args)},
	}}
}

// skillChildSentinel is how this arm's child behaves. The completed arms need one
// that finishes; every other arm needs one that holds until the process dies.
func skillChildSentinel(arm string) string {
	if arm == armSkillCompleted || arm == armReadOnlySkillCompleted {
		return childDone
	}
	return childHang
}

// skillSentinels is what this arm's one prompt carries. The queued arms name the
// holder as well: its ceiling has to be occupied before the skill asks for a
// slot, or the refusal they are built to record never happens.
func skillSentinels(arm string) string {
	if queuedSkillArm(arm) {
		return fleetHolderSentinel + " " + skillSentinel
	}
	return skillSentinel
}

// runSkillClassConstruct drives one skill and dies where this arm is about. The
// completed arms let the turn close and then run the controller's own entry
// point, so one death holds both lineages.
func runSkillClassConstruct(ctx context.Context, root armRoot, arm, bootSystem string, ctrl *control.Controller, sink *graphSink, prov *scripted, turn int) error {
	prompt := fanOutTurn(turn+1, skillSentinels(arm))
	completed := arm == armSkillCompleted || arm == armReadOnlySkillCompleted
	switch {
	case completed:
		if err := ctrl.Run(ctx, prompt); err != nil {
			return fmt.Errorf("skill turn: %w", err)
		}
		if err := runHostInitiatedSkill(ctx, ctrl, arm); err != nil {
			return err
		}
	case arm == armSkillCancel:
		// A stop belongs to whoever admitted the turn, so the arm that is going
		// to be stopped is admitted the way a person's input is.
		ctrl.Send(prompt)
		if err := waitForSkill(sink, prov, arm); err != nil {
			return err
		}
	default:
		go func() { _ = ctrl.Run(ctx, prompt) }()
		if err := waitForSkill(sink, prov, arm); err != nil {
			return err
		}
	}
	stop := ""
	if arm == armSkillCancel {
		stop = stopSkill(ctrl, prov, arm)
	}
	obs := capture("construct", arm, bootSystem, ctrl, sink, root)
	child := skillChildSentinel(arm)
	obs.Progress[probeChildEntered] = []string{fmt.Sprint(prov.fleets.held(childEntered(child)))}
	obs.Progress[probeSkillChildLeft] = []string{fmt.Sprint(prov.fleets.held(childLeft(child)))}
	obs.Progress[probeSkillChildCtxDone] = []string{fmt.Sprint(prov.fleets.held(childCtxDone(child)))}
	obs.Progress[probeSkillStanding] = []string{skillStanding(sink, prov)}
	if stop != "" {
		obs.Progress[probeStopReached] = []string{stop}
	}
	if err := writeObservation(root, obs); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// runHostInitiatedSkill runs the same skill through the controller's own entry
// point, which is what a slash invocation reaches. Its lineage is deliberately
// different — the runner drops the call id for a host-initiated run — so the
// identity row has both to compare rather than one to judge.
func runHostInitiatedSkill(ctx context.Context, ctrl *control.Controller, arm string) error {
	if _, err := ctrl.RunSubagentProfile(ctx, probeSkillName, hostSkillTask, readOnlySkillArm(arm)); err != nil {
		return fmt.Errorf("host-initiated skill: %w", err)
	}
	return nil
}

// The records a skill's child leaves. They ride the observation because only the
// scripted provider knows them, and a store row read without them would be
// asking about work nothing proves ran.
const (
	probeSkillChildLeft    = "probe:skill-child-left"
	probeSkillChildCtxDone = "probe:skill-child-ctxdone"
	// probeSkillStanding is where the call itself stood: dispatched, answered,
	// and whether the ceiling was occupied. A refusal has no event of its own,
	// so this is the only positive evidence the admission row has.
	probeSkillStanding = "probe:skill-standing"
)

// waitForSkill blocks until the skill stands where this arm dies. A refusal is a
// state nothing announces, so the queued arms establish it from both sides — the
// call went out and has not answered, the ceiling is occupied, and the skill's
// own child never arrived — then hold still and ask again, because a child that
// was merely slow to start would have reached the provider by then.
func waitForSkill(sink *graphSink, prov *scripted, arm string) error {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if !queuedSkillArm(arm) {
			if prov.fleets.held(childEntered(childHang)) {
				return nil
			}
		} else if skillHeldBack(sink, prov) {
			time.Sleep(2 * time.Second)
			if !skillHeldBack(sink, prov) {
				return errUnexpected("a skill the scheduler was still holding back",
					"its child ran or its call answered")
			}
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errUnexpected("a skill child "+skillWanted(arm), skillStanding(sink, prov))
}

// skillHeldBack is the refusal expressed as facts rather than as an absence: the
// ceiling is occupied, the call is out, it has not answered, and no child of its
// own has reached the provider.
func skillHeldBack(sink *graphSink, prov *scripted) bool {
	_, dispatched := sink.toolDispatched(skillCallID)
	_, answered := sink.toolResult(skillCallID)
	return prov.fleets.held(childEntered(childHold)) && dispatched && !answered &&
		!prov.fleets.held(childEntered(childHang))
}

func skillStanding(sink *graphSink, prov *scripted) string {
	_, dispatched := sink.toolDispatched(skillCallID)
	_, answered := sink.toolResult(skillCallID)
	return fmt.Sprintf("ceiling=%t dispatched=%t answered=%t child=%t",
		prov.fleets.held(childEntered(childHold)), dispatched, answered,
		prov.fleets.held(childEntered(childHang)))
}

func skillWanted(arm string) string {
	if queuedSkillArm(arm) {
		return "held back by a full ceiling"
	}
	return "inside the provider"
}

// stopSkill stops the work the way a person does — the turn's own cancel — and
// waits to see what that reaches. Nothing is demanded: whether a stop arrives is
// what the arm measures, and an arm that failed when it did not would report its
// own finding as a broken premise.
func stopSkill(ctrl *control.Controller, prov *scripted, arm string) string {
	stopped := time.Now()
	ctrl.Cancel()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && ctrl.Running() {
		time.Sleep(50 * time.Millisecond)
	}
	live := "gone"
	if prov.fleets.held(childEntered(childHang)) && !prov.fleets.held(childLeft(childHang)) {
		live = "still running"
	}
	return fmt.Sprintf("turn=%s work=%s ctx=%t after=%s", turnStanding(ctrl, arm), live,
		prov.fleets.held(childCtxDone(childHang)), time.Since(stopped).Round(time.Second))
}

// skillClassRows are the seven dimensions, one row each. They are separate
// because a skill can share one domain and not another — the whole reason this
// is a classification and not a comparison.
func skillClassRows(arm string, before, after Observation) []row {
	return []row{
		skillAdmissionRow(arm, before),
		skillGraphRow(before),
		skillJournalRow(before, after),
		skillStoreRow(before, after),
		skillCancelRow(arm, before),
		skillRestartRow(arm, before, after),
		skillIdentityRow(arm, before, after),
		skillClassVerdictRow(arm, before, after),
	}
}

const skillAuthority = "the scheduler, read through what the child did"

// skillAdmissionRow is the scheduler dimension. There is no refusal hook on a
// skill's acquire, so the refusal is read the only way it can be: the ceiling was
// proven occupied and the child never arrived, against the sibling arm where the
// ceiling was free and it did.
func skillAdmissionRow(arm string, before Observation) row {
	entered := firstProgress(before, probeChildEntered) == "true"
	answer := "admitted: the child reached the provider"
	verdict := verdictHolds
	switch {
	case queuedSkillArm(arm) && !entered:
		answer = "refused: the ceiling was full and the child never arrived"
	case queuedSkillArm(arm):
		answer = "not held back: the child ran with the ceiling full"
		verdict = verdictViolated
	case !entered:
		answer = "the child never arrived with capacity free"
		verdict = verdictNotMeasured
	}
	return row{
		Semantic: "does it ask the session scheduler", Authority: skillAuthority,
		Artifact: "none observed", Reconstruction: "in-process only",
		Before: answer + " [" + firstProgress(before, probeSkillStanding) + "]",
		After:  "—", Verdict: verdict,
	}
}

// skillGraphRow is the live picture. A skill that never draws one is invisible
// to every frontend while it runs, which is a product fact rather than a
// durability one and is why it is its own row.
func skillGraphRow(before Observation) row {
	var drawn []string
	for _, n := range before.Graph.Nodes {
		if strings.HasPrefix(n.ID, skillCallID) {
			drawn = append(drawn, n.ID+":"+string(n.State))
		}
	}
	sort.Strings(drawn)
	// The whole graph's size rides along: "the filter found nothing" and "there
	// was nothing to find" are different readings, and only the second is what
	// this row means to report.
	return row{
		Semantic: "does the live graph hold it", Authority: liveAuthority,
		Artifact: "none observed", Reconstruction: "in-process only",
		Before: fmt.Sprintf("%s (graph held %d node(s))", orNone(join(drawn)), len(before.Graph.Nodes)),
		After:  "—", Verdict: presenceVerdict(len(drawn) > 0),
	}
}

// presenceVerdict reports what was found without calling absence a defect. What
// a skill owes each layer is the question; a row that answered it here would be
// asserting the classification it exists to establish.
func presenceVerdict(present bool) string {
	if present {
		return verdictHolds
	}
	return verdictAbsent
}

const verdictAbsent = "absent"

// skillJournalRow reports what the journal holds. It does not go through the
// ordinary value comparison: two empty columns are equal, and calling that
// persisted would report an execution nothing recorded as durably recorded —
// which is the substitution this whole probe exists to refuse.
func skillJournalRow(before, after Observation) row {
	return presenceRow("does the journal record it", journalAuthority, "<stem>.execution.jsonl",
		"ExecutionHistory over the whole session", skillExecutions(before), skillExecutions(after))
}

// presenceRow answers "is anything here" before it answers "is it the same".
// Absence on both sides is absence, never agreement.
func presenceRow(semantic, authority, artifact, reconstruction string, before, after []string) row {
	r := row{Semantic: semantic, Authority: authority, Artifact: artifact,
		Reconstruction: reconstruction, Before: orNone(join(before)), After: orNone(join(after))}
	switch {
	case len(before) == 0 && len(after) == 0:
		r.Verdict = verdictAbsent
	case len(after) == 0:
		r.Verdict = verdictLost
	case r.Before == r.After:
		r.Verdict = verdictPersisted
	default:
		r.Verdict = verdictLossy
	}
	return r
}

// skillExecutions are the journal entries this arm's skill could be recorded
// under. The whole journal is read rather than a prefix: a skill records nothing
// today, so a filter written around an id it never wrote would report absence by
// construction.
func skillExecutions(o Observation) []string {
	var out []string
	for _, e := range o.Executions {
		if strings.HasPrefix(e.ID, skillCallID) || strings.Contains(e.Name, probeSkillName) {
			out = append(out, e.ID)
		}
	}
	sort.Strings(out)
	return out
}

// skillStoreRow is the durable half. The two entry points differ here by design:
// one prepares a transcript, the other refuses to.
func skillStoreRow(before, after Observation) row {
	return presenceRow("what the store holds for it", storeAuthority, "subagents/<ref>.meta.json",
		"ListSubagentsByParent over the transcript stem",
		skillChildren(before), skillChildren(after))
}

// skillChildren are the store's records of skill children, by the kind the
// runner declared rather than by a name match: kind is what the store was told
// this execution is.
func skillChildren(o Observation) []string {
	var out []string
	for _, f := range o.Children.Facts {
		if f.Kind == "skill" {
			out = append(out, f.Name+"/"+orDash(f.ParentToolCallID)+"="+f.Status)
		}
	}
	sort.Strings(out)
	return out
}

// skillCancelRow is what a stop reached. Three facts, kept apart: a context that
// closed and a call that returned are different events, and work whose caller
// stopped waiting leaves the same absence behind as work that was cancelled.
func skillCancelRow(arm string, before Observation) row {
	if arm != armSkillCancel {
		return row{
			Semantic: "what a stop reaches", Authority: "the turn's own cancel",
			Artifact: "none observed", Reconstruction: "in-process only",
			Before: "not stopped in this arm", After: "—", Verdict: verdictNotMeasured,
		}
	}
	return row{
		Semantic: "what a stop reaches", Authority: "the turn's own cancel",
		Artifact: "subagents/<ref>.meta.json", Reconstruction: "read at one instant, after the record closed",
		Before: fmt.Sprintf("%s store=%s", firstProgress(before, probeStopReached),
			orNone(join(skillChildren(before)))),
		After: "—", Verdict: skillCancelVerdict(before),
	}
}

// skillCancelVerdict asks whether the terminal the store kept is the one that
// happened. The cause is observed, not read off a message: the child's own
// context closed, which is what a cancellation does and what a failure does not,
// so a record that files it as failed is naming the wrong ending.
func skillCancelVerdict(before Observation) string {
	cancelled := firstProgress(before, probeSkillChildCtxDone) == "true"
	terminal := ""
	for _, f := range before.Children.Facts {
		if f.Kind == "skill" {
			terminal = f.Status
		}
	}
	switch {
	case terminal == "":
		return verdictNotMeasured
	case cancelled && terminal != string(agent.SubagentCancelled):
		return verdictViolated
	default:
		return verdictHolds
	}
}

// skillRestartRow is what the next process can say. It is separate from the
// store row because "a record survives" and "the execution is explicable" are
// different claims: a completed child on disk explains nothing about a run that
// was cut.
func skillRestartRow(arm string, before, after Observation) row {
	stored, rebuilt, cut := skillChildren(after), skillRebuiltNodes(after), skillInterruptions(after)
	answer := fmt.Sprintf("store=%s rebuilt=%s cut=%s",
		orNone(join(stored)), orNone(join(rebuilt)), orNone(join(cut)))
	return row{
		Semantic: "what a restart can say about it", Authority: rebuildAuthority,
		Artifact:       "<stem>.execution.jsonl + subagents/<ref>.meta.json",
		Reconstruction: "execgraph.Rebuild over the journal, and the store beside it",
		Before:         skillAtDeath(arm, before), After: answer,
		Verdict: skillRestartVerdict(stored, rebuilt, cut),
	}
}

// skillInterruptions are the interruptions that name this skill. The whole list
// is wider than the arm: a queued arm fills its ceiling with a fan-out, and a
// fan-out records everything — reading its interruption as the skill's is how an
// arm reports a gap as closed by evidence belonging to something else.
func skillInterruptions(o Observation) []string {
	var out []string
	for _, e := range o.InterruptedExecutions {
		if strings.HasPrefix(e.ID, skillCallID) || strings.Contains(e.Name, probeSkillName) {
			out = append(out, e.ID)
		}
	}
	sort.Strings(out)
	return out
}

// skillRestartVerdict keeps two claims apart. That the store remembers a child
// finished is not that a restart can reconstruct the delegated execution: the
// first is a record, the second is provenance, and an arm that folded them would
// report a run nothing can place as recovered.
func skillRestartVerdict(stored, rebuilt, cut []string) string {
	switch {
	case len(rebuilt) > 0 || len(cut) > 0:
		return verdictHolds
	case len(stored) > 0:
		return verdictRecordOnly
	default:
		return verdictAbsent
	}
}

// verdictRecordOnly is a child the store kept for an execution no reconstruction
// names.
const verdictRecordOnly = "record-only"

// skillRebuiltNodes are the nodes a restart's rebuild draws for this skill.
func skillRebuiltNodes(o Observation) []string {
	var out []string
	for _, n := range rebuiltGraph(o).Graph.Nodes {
		if strings.HasPrefix(n.ID, skillCallID) || strings.Contains(n.Label, probeSkillName) {
			out = append(out, n.ID+":"+string(n.State))
		}
	}
	sort.Strings(out)
	return out
}

func skillAtDeath(arm string, before Observation) string {
	switch {
	case queuedSkillArm(arm):
		return "held back, never started"
	case firstProgress(before, probeChildEntered) != "true":
		return "never reached the provider"
	case skillFinished(before):
		return "ran and returned"
	default:
		return "executing inside the provider"
	}
}

// skillIdentityRow is the seventh dimension, and it reads the two entry points
// apart. A model-invoked skill has a call to join to; the controller's own entry
// point drops that id on purpose, so an empty one there is top-level provenance
// rather than a lost join — reading them together is what stops a correct value
// being reported as a defect.
func skillIdentityRow(arm string, before, after Observation) row {
	var got []string
	for _, f := range after.Children.Facts {
		if f.Kind != "skill" {
			continue
		}
		got = append(got, fmt.Sprintf("%s from=%s model=%s effort=%s",
			f.Name, orDash(f.ParentToolCallID), orDash(f.Model), orDash(f.Effort)))
	}
	sort.Strings(got)
	return row{
		Semantic: "what identity the record carries", Authority: storeAuthority,
		Artifact:       "subagents/<ref>.meta.json",
		Reconstruction: "the call the model made, against the entry point that made it",
		Before:         skillLineageExpected(arm), After: orNone(join(got)),
		Verdict: skillIdentityVerdict(arm, after),
	}
}

func skillLineageExpected(arm string) string {
	if arm == armSkillCompleted || arm == armReadOnlySkillCompleted {
		return "model-invoked joins " + skillCallID + "; host-initiated names none"
	}
	return "model-invoked joins " + skillCallID
}

// skillIdentityVerdict holds when the model-invoked record joins the call the
// model made. The host-initiated one is not held to that: its runner drops the
// id deliberately, and a row demanding it would be reporting a contract as a gap.
func skillIdentityVerdict(arm string, after Observation) string {
	joined, records := false, 0
	for _, f := range after.Children.Facts {
		if f.Kind != "skill" {
			continue
		}
		records++
		if f.ParentToolCallID == skillCallID {
			joined = true
		}
	}
	switch {
	case records == 0:
		return verdictNotMeasured
	case joined:
		return verdictHolds
	default:
		return verdictViolated
	}
}

// skillClassVerdictRow is the arm's answer in one line, and it is a conjunction
// rather than a comparison: an execution the scheduler admitted, whose child
// reached the provider, that no live picture, no journal and no active store
// record names, is lost while it is happening — not merely unrecorded.
func skillClassVerdictRow(arm string, before, after Observation) row {
	entered := firstProgress(before, probeChildEntered) == "true"
	graph := len(skillGraphNodes(before)) > 0
	journal := len(skillExecutions(before)) > 0
	stored := len(skillChildren(before)) > 0
	answer, verdict := "not this arm's question", verdictNotMeasured
	switch {
	case queuedSkillArm(arm):
		answer, verdict = skillQueuedNegative(before)
	case readOnlySkillArm(arm) && runningSkillArm(arm) && entered:
		answer, verdict = readOnlyClass(graph, journal, stored)
	case runningSkillArm(arm) && entered:
		if graph || journal || stored {
			answer, verdict = fmt.Sprintf("named by graph=%t journal=%t store=%t", graph, journal, stored), verdictHolds
		} else {
			answer, verdict = "LOST-SILENT: executing, and named by no layer", verdictViolated
		}
	case runningSkillArm(arm):
		answer = "the child never reached the provider"
	}
	return row{
		Semantic: "an execution in flight, named by nothing", Authority: crossAuthority,
		Artifact:       "none, which is the finding",
		Reconstruction: "scheduler admitted + child entered + no graph, journal or store record",
		Before:         answer, After: "—", Verdict: verdict,
	}
}

// readOnlyClass classifies the entry point that promised no durable side
// effects. Absence there is its contract kept, not a gap — but it still holds a
// scheduler slot, so whether anything draws it while it does is a separate
// answer, and a durable record would be the contract broken the other way.
func readOnlyClass(graph, journal, stored bool) (string, string) {
	switch {
	case journal || stored:
		return "an entry point that promised no durable side effects left one", verdictViolated
	case graph:
		return "ephemeral by contract, and drawn while it holds a slot", verdictEphemeralSeen
	default:
		return "ephemeral by contract, and unseen while it holds a slot", verdictEphemeralUnseen
	}
}

// The two readings of an execution that keeps nothing. Neither is a defect: what
// separates them is whether anyone can see the slot being held.
const (
	verdictEphemeralSeen   = "ephemeral-seen"
	verdictEphemeralUnseen = "ephemeral-unseen"
)

// skillQueuedNegative is the control the running row needs. An execution the
// scheduler never admitted produced nothing, so no layer owes it a record, and a
// convergence that satisfied the row above by recording every opening would
// break this one.
func skillQueuedNegative(before Observation) (string, string) {
	if len(skillChildren(before)) > 0 || len(skillExecutions(before)) > 0 {
		return "a record for work the scheduler never admitted", verdictViolated
	}
	return "held back, and no layer records it — as nothing that never ran should be", verdictHolds
}

func skillGraphNodes(o Observation) []string {
	var out []string
	for _, n := range o.Graph.Nodes {
		if strings.HasPrefix(n.ID, skillCallID) {
			out = append(out, n.ID)
		}
	}
	return out
}

// skillArmInvalid reports the premise this arm could not establish. A skill that
// never reached the provider measures nothing about executions in flight, and a
// ceiling that was not occupied measures nothing about admission.
func skillArmInvalid(arm string, before Observation) string {
	entered := firstProgress(before, probeChildEntered) == "true"
	switch {
	case queuedSkillArm(arm) && entered:
		return "the skill ran while the ceiling was meant to be full"
	case runningSkillArm(arm) && !entered:
		return "the skill's child never reached the provider, so nothing was executing at the death"
	case (arm == armSkillCompleted || arm == armReadOnlySkillCompleted) && !skillFinished(before):
		return "no skill child finished, so the arm compares nothing"
	}
	return ""
}

func skillFinished(before Observation) bool {
	return firstProgress(before, probeSkillChildLeft) == "true"
}
