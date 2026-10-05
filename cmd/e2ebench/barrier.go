package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/safety/evidence"
)

// The terminal-barrier counterfactual: what a barrier at a child's first
// accepted complete_subtask would have cut off. It reads what the host already
// wrote down and classifies with the host's own readers, never a second one.

// barrierVerdict is the strongest explanation for what followed a child's
// first accepted report. A child can satisfy several; the strongest wins.
type barrierVerdict string

const (
	verdictPrematureMutation     barrierVerdict = "PREMATURE_MUTATION"
	verdictPrematureOpaque       barrierVerdict = "PREMATURE_OPAQUE_WORK"
	verdictPrematureVerification barrierVerdict = "PREMATURE_VERIFICATION"
	verdictReportEvolution       barrierVerdict = "SEMANTIC_REPORT_EVOLUTION"
	verdictAmbiguousObservation  barrierVerdict = "AMBIGUOUS_OBSERVATION"
	verdictSafeRedundant         barrierVerdict = "SAFE_REDUNDANT"
)

type barrierCall struct {
	seq      int
	name     string
	args     string
	readOnly bool
	output   string
}

type barrierChild struct {
	task    string
	id      string
	calls   []barrierCall // ordered, reports included
	reports []barrierCall
}

// barrierRow is one child's counterfactual, in the terms the question was
// asked in: what would have been cut, and what explains it.
type barrierRow struct {
	Task            string `json:"task"`
	Child           string `json:"child"`
	Reports         int    `json:"reports"`
	Accepted        int    `json:"accepted"`
	FirstReportCall int    `json:"first_report_call"`
	TotalCalls      int    `json:"total_calls"`
	ObserveAfter    int    `json:"observe_after"`
	VerifyAfter     int    `json:"verify_after"`
	MutateAfter     int    `json:"mutate_proven_after"`
	OpaqueAfter     int    `json:"mutate_unknown_after"`
	ReportAfter     int    `json:"report_after"`
	ClaimsLowered   int    `json:"claims_lowered"`
	Closed          int    `json:"closed"`
	NeedsWork       int    `json:"needs_work"`
	StatusFirst     string `json:"adjudicated_status_first"`
	StatusLast      string `json:"adjudicated_status_last"`
	CriteriaDelta   int    `json:"criteria_delta"`
	EvidenceDelta   int    `json:"evidence_delta"`
	UnresolvedDelta int    `json:"unresolved_delta"`

	Verdict barrierVerdict `json:"verdict"`
}

// reportShape is the structural content of one report, for comparing two of
// them without comparing prose.
type reportShape struct {
	status     string
	criteria   int
	evidence   int
	unresolved int
}

func parseReportShape(args string) (reportShape, bool) {
	var p struct {
		Status             string `json:"status"`
		AcceptanceCriteria []struct {
			Evidence []json.RawMessage `json:"evidence"`
		} `json:"acceptance_criteria"`
		Unresolved []string `json:"unresolved"`
	}
	if json.Unmarshal([]byte(args), &p) != nil {
		return reportShape{}, false
	}
	shape := reportShape{status: p.Status, criteria: len(p.AcceptanceCriteria), unresolved: len(p.Unresolved)}
	for _, c := range p.AcceptanceCriteria {
		shape.evidence += len(c.Evidence)
	}
	return shape, true
}

// adjudicatedStatus and claimsLowered read the host's own answer to a report.
// The prefix is written by the tool, never by the model. Two vocabularies are
// recognised so one anchor spans both arms: the run that said "accepted" for
// every outcome, and the one that names the verdict.
func closureVerdict(output string) (verdict string, ok bool) {
	switch {
	case strings.HasPrefix(output, "complete_subtask closed:"):
		return "closed", true
	case strings.HasPrefix(output, "complete_subtask needs_work:"):
		return "needs_work", true
	case strings.HasPrefix(output, "complete_subtask accepted"):
		// The arm that had one word for both: a lowered claim was the same
		// answer as an intact one, and only the trailing sentence differed.
		if claimsLowered(output) > 0 {
			return "needs_work", true
		}
		return "closed", true
	}
	return "", false
}

func adjudicatedStatus(output string) (string, bool) {
	if _, ok := closureVerdict(output); !ok {
		return "", false
	}
	for field := range strings.FieldsSeq(output) {
		if rest, ok := strings.CutPrefix(field, "status="); ok {
			return rest, true
		}
	}
	return "", true
}

func claimsLowered(output string) int {
	i := strings.Index(output, "the host lowered ")
	if i < 0 {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(output[i:], "the host lowered %d", &n); err != nil {
		return 0
	}
	return n
}

// bashCommand extracts a shell command so the host's verification classifier
// can judge it. A tool that is not bash carries no command to judge.
func bashCommand(name, args string) string {
	if name != "bash" {
		return ""
	}
	var p struct {
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(args), &p) != nil {
		return ""
	}
	return p.Command
}

func (c barrierChild) row() barrierRow {
	row := barrierRow{Task: c.task, Child: c.id, Reports: len(c.reports), TotalCalls: len(c.calls), FirstReportCall: -1}
	var firstShape, lastShape reportShape
	haveFirst := false
	for _, r := range c.reports {
		verdict, ok := closureVerdict(r.output)
		if !ok {
			continue
		}
		row.Accepted++
		row.ClaimsLowered += claimsLowered(r.output)
		if verdict == "closed" {
			row.Closed++
		} else {
			row.NeedsWork++
		}
		status, _ := adjudicatedStatus(r.output)
		shape, parsed := parseReportShape(r.args)
		if parsed {
			lastShape = shape
		}
		// The anchor is the host saying the sub-task is done, so the two arms
		// answer the same question: what followed that, not what followed the
		// first well-formed call.
		if verdict == "closed" && !haveFirst && parsed {
			firstShape, haveFirst = shape, true
			row.FirstReportCall, row.StatusFirst = r.seq, status
		}
		row.StatusLast = status
	}
	if !haveFirst {
		return row
	}
	row.CriteriaDelta = lastShape.criteria - firstShape.criteria
	row.EvidenceDelta = lastShape.evidence - firstShape.evidence
	row.UnresolvedDelta = lastShape.unresolved - firstShape.unresolved
	for _, call := range c.calls {
		if call.seq <= row.FirstReportCall {
			continue
		}
		// Verification is read first on purpose: a command whose extent the
		// host cannot establish classes as a mutation, and a check is exactly
		// such a command. Proven and unknown then stay apart.
		switch class := evidence.ToolCallMutationClass(call.name, json.RawMessage(call.args), call.readOnly); {
		case call.name == completeSubtaskName:
			row.ReportAfter++
		case evidence.CommandRunsVerification(bashCommand(call.name, call.args)):
			row.VerifyAfter++
		case class == evidence.MutationProven:
			row.MutateAfter++
		case class == evidence.MutationUnknown:
			row.OpaqueAfter++
		default:
			row.ObserveAfter++
		}
	}
	row.Verdict = row.verdict()
	return row
}

const completeSubtaskName = "complete_subtask"

// verdict names the strongest explanation. Order matters: a mutation after an
// accepted report makes that report stale whatever else followed, and only a
// child whose later reports say the same thing is safe to cut.
func (r barrierRow) verdict() barrierVerdict {
	switch {
	case r.MutateAfter > 0:
		return verdictPrematureMutation
	case r.OpaqueAfter > 0:
		return verdictPrematureOpaque
	case r.VerifyAfter > 0:
		return verdictPrematureVerification
	case r.CriteriaDelta != 0 || r.EvidenceDelta != 0 || r.UnresolvedDelta != 0 || r.StatusFirst != r.StatusLast:
		return verdictReportEvolution
	case r.ObserveAfter > 0:
		return verdictAmbiguousObservation
	default:
		return verdictSafeRedundant
	}
}

// collectBarrierChildren groups a trajectory's tool results by the child that
// produced them. The nesting sink stamps every child event with its parent id,
// so attribution is read, never inferred from ordering.
func collectBarrierChildren(path string) ([]barrierChild, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	task := strings.TrimSuffix(filepath.Base(path), ".trajectory.jsonl")
	byID := map[string]*barrierChild{}
	var order []string
	seq := 0
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec struct {
			Event *struct {
				Kind string `json:"kind"`
				Tool *struct {
					ID       string `json:"id"`
					ParentID string `json:"parentId"`
					Name     string `json:"name"`
					Args     string `json:"args"`
					Output   string `json:"output"`
					ReadOnly bool   `json:"readOnly"`
				} `json:"tool"`
			} `json:"event"`
		}
		if json.Unmarshal([]byte(line), &rec) != nil || rec.Event == nil {
			continue
		}
		if rec.Event.Kind != "tool_result" || rec.Event.Tool == nil || rec.Event.Tool.ParentID == "" {
			continue
		}
		seq++
		t := rec.Event.Tool
		child, ok := byID[t.ParentID]
		if !ok {
			child = &barrierChild{task: task, id: t.ParentID}
			byID[t.ParentID] = child
			order = append(order, t.ParentID)
		}
		call := barrierCall{seq: seq, name: t.Name, args: t.Args, readOnly: t.ReadOnly, output: t.Output}
		child.calls = append(child.calls, call)
		if t.Name == completeSubtaskName {
			child.reports = append(child.reports, call)
		}
	}
	out := make([]barrierChild, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

// runBarrierMode answers one question: what would a barrier at the first
// accepted report have cut off. It reports every child that produced one and
// says nothing about children that produced none.
func runBarrierMode(dir string) (string, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "**", "*.trajectory.jsonl"))
	if err != nil {
		return "", err
	}
	direct, err := filepath.Glob(filepath.Join(dir, "*.trajectory.jsonl"))
	if err != nil {
		return "", err
	}
	paths = append(paths, direct...)
	slices.Sort(paths)
	if len(paths) == 0 {
		return "", fmt.Errorf("no *.trajectory.jsonl under %s", dir)
	}
	var rows []barrierRow
	for _, p := range paths {
		children, err := collectBarrierChildren(p)
		if err != nil {
			return "", fmt.Errorf("%s: %w", p, err)
		}
		for _, c := range children {
			if len(c.reports) == 0 {
				continue
			}
			rows = append(rows, c.row())
		}
	}
	return renderBarrier(rows), nil
}

func renderBarrier(rows []barrierRow) string {
	var b strings.Builder
	b.WriteString("# Terminal-barrier counterfactual\n\n")
	b.WriteString("Anchor: each child's first accepted `complete_subtask`. Columns count what\n")
	b.WriteString("followed it, classified with the host's own mutation and verification readers.\n\n")
	b.WriteString("| task | child | reports | closed | needs_work | first@ | total | observe | verify | mutate | opaque | report | lowered | status | Δcrit | Δev | Δunres | verdict |\n")
	b.WriteString("|---|---|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|---|--:|--:|--:|---|\n")
	tally := map[barrierVerdict]int{}
	for _, r := range rows {
		tally[r.Verdict]++
		child := r.Child
		if i := strings.LastIndex(child, "/"); i >= 0 {
			child = child[i+1:]
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d | %s→%s | %+d | %+d | %+d | %s |\n",
			r.Task, child, r.Reports, r.Closed, r.NeedsWork, r.FirstReportCall, r.TotalCalls,
			r.ObserveAfter, r.VerifyAfter, r.MutateAfter, r.OpaqueAfter, r.ReportAfter, r.ClaimsLowered,
			r.StatusFirst, r.StatusLast, r.CriteriaDelta, r.EvidenceDelta, r.UnresolvedDelta, r.Verdict)
	}
	b.WriteString("\n## Verdicts\n\n")
	for _, v := range []barrierVerdict{verdictPrematureMutation, verdictPrematureOpaque,
		verdictPrematureVerification, verdictReportEvolution, verdictAmbiguousObservation,
		verdictSafeRedundant} {
		fmt.Fprintf(&b, "- %s: %d\n", v, tally[v])
	}
	fmt.Fprintf(&b, "\nchildren with an accepted report: %d\n", len(rows))
	return b.String()
}
