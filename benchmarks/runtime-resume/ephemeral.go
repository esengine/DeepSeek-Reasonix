package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"reasonix/internal/agentgraph"
	"reasonix/internal/control"
	"reasonix/internal/provider"
)

// The two ephemeral entry points, read against the execution read model's own
// authority: the snapshot is what a reader starts from and what a gap restores,
// and the stream is a projection of the same facts. A node only the stream has
// is therefore not a lighter node but one the authority cannot account for, and
// these arms ask whether either entry point produces one.
const (
	armEphemeralTask  = "ephemeral-task-authority"
	armEphemeralSkill = "ephemeral-skill-authority"
)

func ephemeralArm(name string) bool {
	return name == armEphemeralTask || name == armEphemeralSkill
}

const (
	ephemeralSentinel = "PROBE-EPHEMERAL"
	// ephemeralCallID is the call the model makes. Both entry points use it, so
	// the two arms differ by which tool ran and by nothing else.
	ephemeralCallID = "probe_ephemeral"
)

// ephemeralCall dispatches the entry point this arm measures. Its child hangs,
// because every row here is about a run that is still happening: an ephemeral
// execution that has ended owes no reader anything.
func ephemeralCall(arm string) []provider.Chunk {
	name, args := "read_only_task", map[string]any{
		"prompt": childHang + " ephemeral research", "description": "still executing at death",
	}
	if arm == armEphemeralSkill {
		name, args = "read_only_skill", map[string]any{
			"name": probeSkillName, "arguments": childHang + " ephemeral research",
		}
	}
	raw, _ := json.Marshal(args)
	return []provider.Chunk{{
		Type:     provider.ChunkToolCall,
		ToolCall: &provider.ToolCall{ID: ephemeralCallID, Name: name, Arguments: string(raw)},
	}}
}

// runEphemeralConstruct drives one ephemeral run and reads three surfaces at the
// same instant, while its child is inside the provider: what the stream drew,
// what the authority holds, and what a reader is left with once a gap makes it
// start from the authority again.
func runEphemeralConstruct(ctx context.Context, root armRoot, arm, bootSystem string, ctrl *control.Controller, sink *graphSink, prov *scripted, turn int) error {
	go func() { _ = ctrl.Run(ctx, fanOutTurn(turn+1, ephemeralSentinel)) }()
	if err := waitForEphemeral(prov); err != nil {
		return err
	}
	obs := capture("construct", arm, bootSystem, ctrl, sink, root)
	obs.Progress[probeChildEntered] = []string{fmt.Sprint(prov.fleets.held(childEntered(childHang)))}
	// One instant, three readings. Taken here because two of them cannot be
	// taken later at all: the authority is recomputed per call, and what a gap
	// leaves is a claim about the moment the gap happens.
	live, _ := sink.snapshot()
	authority := ctrl.ExecutionGraph()
	obs.Progress[probeEphemeralLive] = []string{ephemeralNodes(live)}
	obs.Progress[probeEphemeralAuthority] = []string{ephemeralNodes(authority.Graph)}
	obs.Progress[probeEphemeralAfterGap] = []string{ephemeralNodes(afterGap(authority.Graph))}
	if err := writeObservation(root, obs); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// The three readings one instant produces. They ride the observation because
// none of them survives the process, and the arm's whole question is what a
// reader could see while the run was happening.
const (
	probeEphemeralLive      = "probe:ephemeral-live"
	probeEphemeralAuthority = "probe:ephemeral-authority"
	probeEphemeralAfterGap  = "probe:ephemeral-after-gap"
)

// afterGap is what a view holds once a dropped stream makes it start from the
// authority again: the snapshot, and only the snapshot. A resync replaces the
// folded state rather than merging into it — that is what makes the snapshot the
// authority — so whatever the fold held and it does not has left the screen.
func afterGap(authority agentgraph.Graph) agentgraph.Graph { return authority }

func waitForEphemeral(prov *scripted) error {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if prov.fleets.held(childEntered(childHang)) {
			// Let the picture settle: a reading taken the instant the child
			// arrives could miss a delta that was already on its way.
			time.Sleep(time.Second)
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errUnexpected("an ephemeral child inside the provider", "it never reached one")
}

// ephemeralNodes renders the nodes belonging to this arm's run, worker and group
// alike: a group card drawn for work the authority cannot account for is the
// same defect as a worker one.
func ephemeralNodes(g agentgraph.Graph) string {
	var out []string
	for _, n := range g.Nodes {
		if strings.HasPrefix(n.ID, ephemeralCallID) {
			out = append(out, n.ID+":"+orDash(string(n.State)))
		}
	}
	sort.Strings(out)
	return orNone(join(out))
}

func ephemeralRows(arm string, before, after Observation) []row {
	return []row{
		ephemeralLiveRow(before),
		ephemeralAuthorityRow(before),
		ephemeralGapRow(before),
		ephemeralRestartRow(before, after),
		ephemeralDurableRow(before, after),
		ephemeralContractRow(arm, before),
	}
}

const authorityName = "Controller.ExecutionGraph (the snapshot a reader starts from)"

func ephemeralLiveRow(before Observation) row {
	drawn := firstProgress(before, probeEphemeralLive)
	return row{
		Semantic: "what the stream drew while it ran", Authority: liveAuthority,
		Artifact: "none observed", Reconstruction: "in-process only",
		Before: drawn, After: "—", Verdict: presenceVerdict(drawn != "none"),
	}
}

func ephemeralAuthorityRow(before Observation) row {
	held := firstProgress(before, probeEphemeralAuthority)
	return row{
		Semantic: "what the authority held at the same instant", Authority: authorityName,
		Artifact:       "<stem>.execution.jsonl + subagents/<ref>.meta.json",
		Reconstruction: "execgraph.Rebuild over durable facts, recomputed on demand",
		Before:         held, After: "—", Verdict: presenceVerdict(held != "none"),
	}
}

// ephemeralGapRow is the one that decides. A reader that loses the stream starts
// from the authority again, so whatever the fold held and the authority does not
// leaves the screen — while the child it named is still holding a slot.
func ephemeralGapRow(before Observation) row {
	left := firstProgress(before, probeEphemeralAfterGap)
	return row{
		Semantic: "what a reader is left with after a gap", Authority: authorityName,
		Artifact:       "none: the fold is replaced, never merged",
		Reconstruction: "a resync starts from the authority",
		Before:         left, After: "—", Verdict: presenceVerdict(left != "none"),
	}
}

func ephemeralRestartRow(before, after Observation) row {
	return row{
		Semantic: "what a restart can say about it", Authority: rebuildAuthority,
		Artifact:       "<stem>.execution.jsonl",
		Reconstruction: "execgraph.Rebuild in the next process",
		Before:         "executing inside the provider",
		After:          ephemeralNodes(rebuiltGraph(after).Graph),
		Verdict:        presenceVerdict(ephemeralNodes(rebuiltGraph(after).Graph) != "none"),
	}
}

// ephemeralDurableRow is the contract both entry points already promise. It is
// read rather than assumed, because a convergence that made one of them visible
// by recording it would satisfy the rows above and break this one.
func ephemeralDurableRow(before, after Observation) row {
	return presenceRow("journal and store", crossAuthority,
		"<stem>.execution.jsonl, subagents/<ref>.meta.json",
		"what an ephemeral entry point promises to leave: nothing",
		ephemeralDurable(before), ephemeralDurable(after))
}

func ephemeralDurable(o Observation) []string {
	var out []string
	for _, e := range o.Executions {
		if strings.HasPrefix(e.ID, ephemeralCallID) {
			out = append(out, "journal:"+e.ID)
		}
	}
	for _, f := range o.Children.Facts {
		if strings.HasPrefix(f.ParentToolCallID, ephemeralCallID) {
			out = append(out, "store:"+f.ParentToolCallID+"="+f.Status)
		}
	}
	sort.Strings(out)
	return out
}

// ephemeralContractRow states the authority contract as a conjunction. A node
// the stream drew, an authority that cannot account for it, and a child still
// executing: that is not a lighter kind of node, it is one that leaves the
// screen at the next gap while the work it named goes on holding a slot.
func ephemeralContractRow(arm string, before Observation) row {
	live := firstProgress(before, probeEphemeralLive) != "none"
	authority := firstProgress(before, probeEphemeralAuthority) != "none"
	running := firstProgress(before, probeChildEntered) == "true"
	answer, verdict := "no node was drawn: nothing claims what the authority cannot hold", verdictHolds
	switch {
	case !running:
		answer, verdict = "the child never reached the provider", verdictNotMeasured
	case live && !authority:
		answer, verdict = "drawn by the stream, absent from the authority, and still executing", verdictViolated
	case live && authority:
		answer, verdict = "drawn, and the authority accounts for it", verdictHolds
	}
	return row{
		Semantic: "a node the authority cannot account for", Authority: crossAuthority,
		Artifact:       "none, which is the point",
		Reconstruction: "the stream and the snapshot, read at one instant",
		Before:         answer, After: "—", Verdict: verdict,
	}
}

// ephemeralArmInvalid reports the premise this arm could not establish.
func ephemeralArmInvalid(before Observation) string {
	if firstProgress(before, probeChildEntered) != "true" {
		return "the ephemeral child never reached the provider, so nothing was executing at the reading"
	}
	return ""
}
