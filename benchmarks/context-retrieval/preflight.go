package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/base/retrieval"
	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/provider"
)

// Preflight is what makes a real run worth paying for. An answer the model can
// already read, or a cue not where the matrix says, turns an arm into a
// different experiment without saying so.

// armScales are the fold-index arms an index task is measured across.
var armScales = []string{"default", "half", "quarter", "off"}

// experimentArm is one measured configuration of a task.
type experimentArm struct {
	name      string
	scale     string
	searchOff bool
}

// armsFor is the single declaration of which arms a task runs at, so preflight
// and the run itself cannot check one set and measure another.
func armsFor(t contextTask) []experimentArm {
	if t.Experiment == experimentSearch {
		return []experimentArm{
			{"search-on", "default", false},
			{"search-off", "default", true},
		}
	}
	arms := make([]experimentArm, 0, len(armScales))
	for _, scale := range armScales {
		arms = append(arms, experimentArm{"index-" + scale, scale, false})
	}
	return arms
}

func scaleOf(name string) ablation.FoldIndexScale {
	s, err := ablation.ParseFoldIndexScale(name)
	if err != nil {
		panic("unknown scale " + name) // the list above is a constant
	}
	return s
}

// armFor is the arm a task is measured under at one fold-index scale.
func armFor(scale string, searchOff bool) ablation.Set {
	set := ablation.Set{}
	if searchOff {
		set = ablation.New(ablation.RecallSearch)
	}
	return set.WithFoldIndex(scaleOf(scale))
}

type finding struct {
	Task, Arm, Problem string
}

func (f finding) String() string { return fmt.Sprintf("%s [%s]: %s", f.Task, f.Arm, f.Problem) }

// checkTask builds the task under every arm it will be run at and reports what
// does not hold.
func checkTask(t contextTask, root string) ([]finding, error) {
	var out []finding
	for _, arm := range armsFor(t) {
		// Deterministic values: a preflight has to reproduce, and a tier
		// calibration measured against one run's nonces is measured against
		// nothing.
		inst, err := instantiateTask(t, seededRand(t.ID, arm.name, "preflight"))
		if err != nil {
			return nil, err
		}
		dir := filepath.Join(root, t.ID, arm.name)
		f, err := buildFixture(inst, armFor(arm.scale, arm.searchOff), filepath.Join(dir, "session.jsonl"))
		if err != nil {
			return nil, err
		}
		visible := visibleText(f)

		// The answer must be unreadable in every arm, or the task measures
		// reading rather than recall.
		for _, marker := range inst.AnswerMarkers {
			if len(marker) < 3 {
				continue // a bare number is not a nonce; the whole set still has to match
			}
			if strings.Contains(visible, marker) {
				out = append(out, finding{t.ID, arm.name, fmt.Sprintf("answer marker %q is already visible", marker)})
			}
		}

		// The corpus has to be findable at all, or a miss in a real run says
		// nothing about the model.
		rank, err := probeRank(f, inst.ProbeQuery)
		if err != nil {
			return nil, err
		}
		switch {
		case rank == 0:
			out = append(out, finding{t.ID, arm.name, fmt.Sprintf("probe query %q does not find the target at all", inst.ProbeQuery)})
		case rank > 5:
			out = append(out, finding{t.ID, arm.name, fmt.Sprintf("probe query %q ranks the target %d", inst.ProbeQuery, rank)})
		}

		if t.Experiment == experimentIndex {
			out = append(out, checkCueVisibility(t, inst, arm.scale, arm.name, visible)...)
		} else if line := indexLineFor(f, f.Target); line != "" {
			out = append(out, finding{t.ID, arm.name,
				"a search task is addressed by the fold index, so it measures the index and not search: " + line})
		}

		// The projection is what the agent believes. This is what the model is
		// told, and it is the one that has to hold.
		req, err := captureRequest(f, armFor(arm.scale, arm.searchOff), inst.Prompt)
		if err != nil {
			return nil, fmt.Errorf("%s [%s]: capture: %w", t.ID, arm.name, err)
		}
		out = append(out, checkCapturedRequest(t, inst, arm.name, arm.scale, arm.searchOff, req)...)
	}
	return out, nil
}

// checkCapturedRequest holds one arm's real provider request to its contract:
// the answer unreadable, the cue exactly where its tier says, the recall tool
// offering only what this arm can do, and no host-side field surviving the
// boundary.
func checkCapturedRequest(t contextTask, inst fixtureInstance, armName, scale string, searchOff bool, req provider.Request) []finding {
	var out []finding
	for _, marker := range inst.AnswerMarkers {
		if len(marker) < 3 {
			continue // a bare number is not a nonce; the set as a whole still is
		}
		for _, at := range findInSurface(req, marker) {
			out = append(out, finding{t.ID, armName, fmt.Sprintf("answer marker %q leaked at %s", marker, at)})
		}
	}
	if t.Experiment == experimentIndex {
		wantVisible, wantHidden := tierScales(t.CueTier)
		where := findInSurface(req, inst.CueMarker)
		switch {
		case contains(wantVisible, scale) && len(where) == 0:
			out = append(out, finding{t.ID, armName, fmt.Sprintf("cue %q is not provider-visible; the tier expects it", inst.CueMarker)})
		case contains(wantHidden, scale) && len(where) > 0:
			out = append(out, finding{t.ID, armName, fmt.Sprintf("cue %q is provider-visible at %s: %s", inst.CueMarker, where[0], scale)})
		}
	}

	schema, ok := recallToolSchema(req)
	if !ok {
		out = append(out, finding{t.ID, armName, "recall is not in the provider's tool list"})
		return append(out, boundaryFindings(t, armName, req)...)
	}
	params := ""
	if b, err := json.Marshal(schema.Parameters); err == nil {
		params = string(b)
	}
	// Both arms keep the read half: the ablation removes search, not recall.
	if !strings.Contains(params, `"positions"`) {
		out = append(out, finding{t.ID, armName, "recall lost its positions parameter"})
	}
	hasQuery := strings.Contains(params, `"query"`)
	switch {
	case searchOff && hasQuery:
		out = append(out, finding{t.ID, armName, "recall still offers a query parameter"})
	case !searchOff && !hasQuery:
		out = append(out, finding{t.ID, armName, "recall has no query parameter"})
	}
	if searchOff {
		for _, phrase := range searchWording {
			for _, at := range findInSurface(req, phrase) {
				out = append(out, finding{t.ID, armName, fmt.Sprintf("search is offered to an arm without it: %q at %s", phrase, at)})
			}
		}
	}
	return append(out, boundaryFindings(t, armName, req)...)
}

func boundaryFindings(t contextTask, armName string, req provider.Request) []finding {
	var out []finding
	for _, at := range localOnlyLeaks(req) {
		out = append(out, finding{t.ID, armName, "host-only field crossed the provider boundary at " + at})
	}
	return out
}

// checkCueVisibility holds an index task to its tier: the cue is addressed at
// the scales the matrix names and at no others. A policy change that moves it
// fails here rather than turning two arms into one experiment.
func checkCueVisibility(t contextTask, inst fixtureInstance, scale, armName, visible string) []finding {
	wantVisible, wantHidden := tierScales(t.CueTier)
	has := strings.Contains(visible, inst.CueMarker)
	switch {
	case contains(wantVisible, scale) && !has:
		return []finding{{t.ID, armName, fmt.Sprintf("cue %q should be addressed at %s and is not", inst.CueMarker, scale)}}
	case contains(wantHidden, scale) && has:
		return []finding{{t.ID, armName, fmt.Sprintf("cue %q should be gone at %s and is not", inst.CueMarker, scale)}}
	}
	return nil
}

func contains(list []string, want string) bool { return slices.Contains(list, want) }

// probeRank runs the task's own query against the folded region the way recall
// would, and reports where the target lands.
func probeRank(f builtFixture, query string) (int, error) {
	terms, err := retrieval.QueryTerms(query)
	if err != nil {
		return 0, err
	}
	canonical := f.Session.Snapshot()
	covered := min(f.State.Projection.CoveredCount, len(canonical))
	docs := make([]map[string]int, 0, covered)
	positions := make([]int, 0, covered)
	lengths := make([]int, 0, covered)
	for i, m := range canonical[:covered] {
		var text strings.Builder
		text.WriteString(m.Content)
		for _, tc := range m.ToolCalls {
			text.WriteString(" " + tc.Name + " " + string(tc.Arguments))
		}
		tokens := retrieval.Tokens(text.String())
		if len(tokens) == 0 {
			continue
		}
		docs = append(docs, retrieval.Counts(tokens))
		positions = append(positions, i)
		lengths = append(lengths, len(tokens))
	}
	df := retrieval.DocumentFrequency(docs)
	avg := 0.0
	for _, l := range lengths {
		avg += float64(l)
	}
	if len(lengths) > 0 {
		avg /= float64(len(lengths))
	}
	type hit struct {
		pos   int
		score float64
	}
	var hits []hit
	for i, counts := range docs {
		if s := retrieval.BM25Score(counts, lengths[i], terms, df, len(docs), avg); s > 0 {
			hits = append(hits, hit{positions[i], s})
		}
	}
	for i := range hits {
		for j := i + 1; j < len(hits); j++ {
			if hits[j].score > hits[i].score {
				hits[i], hits[j] = hits[j], hits[i]
			}
		}
	}
	// A tool result answers at its caller's position, so either counts.
	for i, h := range hits {
		if h.pos == f.Target || h.pos == f.Target+1 {
			return i + 1, nil
		}
	}
	return 0, nil
}

// indexLineFor returns the fold-index line addressing a position, if any.
func indexLineFor(f builtFixture, pos int) string {
	want := fmt.Sprintf("#%d ", pos)
	for line := range strings.SplitSeq(visibleIndexOnly(f), "\n") {
		if strings.Contains(line, want) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// visibleIndexOnly is just the fold-index sections of the projection.
func visibleIndexOnly(f builtFixture) string {
	var b strings.Builder
	for _, m := range visibleContext(f) {
		if i := strings.Index(m.Content, "## Folded work index"); i >= 0 {
			b.WriteString(m.Content[i:])
			b.WriteString("\n")
		}
	}
	return b.String()
}
