// contextbench.go — what the model did with the affordance, read off structure.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"reasonix/internal/base/tokencount"
	"reasonix/internal/contract/provider"
)

// Every judgement here reads a tool call's name and its JSON arguments, or a
// canonical position, and never the model's prose. "I will search the history"
// is not a search; a recall call with a query is.

// failure stages, in funnel order. Exactly one is reported per run.
const (
	stageReadMissed   = "NoSearch"           // never asked, and never read the target either
	stageSearchMiss   = "SearchMiss"         // searched, target never came back
	stageHitNotRead   = "HitNotRead"         // target was returned and never opened
	stageAnswerWrong  = "ReadButAnswerWrong" // opened it and still answered wrong
	stageRecovered    = "Recovered"
	stageContaminated = "Contaminated"

	// Routing is judged by model round, not by the linear order of tool events:
	// two calls in one round are one parallel fan-out, and calling that
	// "workspace first" would report a choice the model never made.
	routeMemoryFirst    = "MemoryFirst"
	routeWorkspaceFirst = "WorkspaceFirst"
	routeParallel       = "Parallel"
	routeNeither        = "Neither"
	stageCueRead        = "DirectRead"  // read the target from an index address, no search needed
	stageNoRetrieval    = "NoRetrieval" // answered without touching recall at all
	toolRecall          = "recall"
)

// searchAttempt is one query the model wrote, and what it got back. Kept per
// attempt rather than aggregated: the question is why a retrieval that the host
// probe ranks first takes the model more than one try.
type searchAttempt struct {
	Ordinal     int    `json:"ordinal"`
	Round       int    `json:"round"`
	Query       string `json:"query"`
	TargetRank  int    `json:"target_rank"` // 0 = the target did not come back
	ResultCount int    `json:"result_count"`
	// Reformulation classifies this query against the one before it, and
	// MissingProbeTerms are the host probe's terms this query left out. Both
	// are derived from token sets: no judge, and nothing read out of prose.
	Reformulation     string   `json:"reformulation,omitempty"`
	MissingProbeTerms []string `json:"missing_probe_terms,omitempty"`
}

// contextMetrics is one run's behaviour.
type contextMetrics struct {
	Task string `json:"task"`
	Arm  string `json:"arm"`

	SearchCalls int `json:"search_calls"`
	// SearchHits counts searches that returned anything; TargetSearchHits
	// counts searches that returned the target. The gap is query quality.
	SearchHits       int `json:"search_hits"`
	TargetSearchHits int `json:"target_search_hits"`
	FirstTargetRank  int `json:"first_target_rank"` // 0 = never returned
	// FirstHitCoverage: how much of the answer the first hit handed over.
	// rank=1 with coverage 1/3 is a window too short to answer with, not a
	// model that would not stop.
	FirstHitCoverage int `json:"first_hit_coverage"`
	AnswerValues     int `json:"answer_values"`

	ReadCalls    int  `json:"read_calls"`
	TargetRead   bool `json:"target_read"`
	ReadAfterHit bool `json:"read_after_hit"`
	// RecallReadWithoutSearch: the index addressed the target, so no search was
	// needed. Not "direct read" — read_file is a read too, and in an audit the
	// two mean opposite things.
	RecallReadWithoutSearch bool `json:"recall_read_without_search"`
	SearchThenRecallRead    bool `json:"search_then_recall_read"`

	FirstSearchRound int `json:"first_search_round"`
	FirstReadRound   int `json:"first_read_round"`
	RetrievalRounds  int `json:"retrieval_rounds"`

	// RecallReturnedTokens is what recall paged back in. The index saves
	// resident prefix; this is what is paid dynamically instead.
	RecallReturnedTokens int `json:"recall_returned_tokens"`

	Searches []searchAttempt `json:"searches,omitempty"`
	// PostHitSearches are queries issued after the target had already been
	// returned: not a query problem, a result-to-read problem.
	PostHitSearches int `json:"post_hit_searches"`

	AnswerRecovered bool `json:"answer_recovered"`
	// AnswerAmbiguous marks an answer that offered more than one candidate for
	// a value. Markers matching proves the right fact appears; it does not
	// prove it was submitted as the answer. Recorded, never acted on.
	AnswerAmbiguous bool     `json:"answer_ambiguous,omitempty"`
	AmbiguousVar    string   `json:"ambiguous_var,omitempty"`
	MissingMarkers  []string `json:"missing_markers,omitempty"`
	// FinalAnswer is kept bounded so a surprising run can be read back. A
	// metric nobody can audit is a claim, not a measurement.
	FinalAnswer   string `json:"final_answer,omitempty"`
	FailureStage  string `json:"failure_stage"`
	TimeoutReason string `json:"timeout_reason,omitempty"`

	// UnexpectedWorkTools are investigation tools used instead of recall.
	// Reaching for the workspace is behaviour, not noise: how a model routes
	// its uncertainty when memory is missing is part of what is measured.
	UnexpectedWorkTools     []string `json:"unexpected_work_tools,omitempty"`
	EscapeCalls             int      `json:"escape_calls"`
	EscapeBeforeFirstRecall int      `json:"escape_before_first_recall"`
	EscapeAfterFirstRecall  int      `json:"escape_after_first_recall"`
	// Contaminated: the answer was reachable through something other than
	// recall. Asserting this beats trusting the isolation, and a run that trips
	// it leaves the statistics rather than inflating one.
	Contaminated bool   `json:"contaminated,omitempty"`
	LeakedVia    string `json:"leaked_via,omitempty"`
	LeakedMarker string `json:"leaked_marker,omitempty"`
	LeakedArgs   string `json:"leaked_args,omitempty"`
	LeakedOutput string `json:"leaked_output,omitempty"`
	// EscapeFoundAnswer separates "looked at the workspace" from "found the
	// answer there": different facts about the same call.
	EscapeFoundAnswer bool `json:"escape_found_answer,omitempty"`

	CueVisible bool `json:"cue_visible"`

	// trajectory is kept out of the metric line and written beside it, so a
	// future question can be asked of this batch instead of a new one.
	trajectory []provider.Message

	// CueDirectRead: the cue was addressed, the first memory action was a
	// positions read, and it covered the target. Crediting any positions call
	// would hand the index a search's work.
	CueDirectRead bool `json:"cue_direct_read"`

	// Sufficiency, not hit. A search that returns the target at rank 1 has not
	// necessarily returned the answer: the snippet is bounded. The stopping
	// boundary is the round where everything needed was readable.
	FirstSufficientRound        int    `json:"first_sufficient_round"`
	SufficientVia               string `json:"sufficient_via"` // snippet | read | never
	RoundsAfterSufficient       int    `json:"rounds_after_sufficient"`
	SearchesAfterSufficient     int    `json:"searches_after_sufficient"`
	ReadsAfterSufficient        int    `json:"reads_after_sufficient"`
	EscapesAfterSufficient      int    `json:"escapes_after_sufficient"`
	RecallTokensAfterSufficient int    `json:"recall_tokens_after_sufficient"`
	StoppingClass               string `json:"stopping_class"`

	// What happened after the answer was already in hand. A timeout that spent
	// seven rounds past its target is a stopping problem, and extending the
	// budget would only buy the loop more time.
	TargetFirstSeenRound    int  `json:"target_first_seen_round"`
	RoundsAfterTarget       int  `json:"rounds_after_target"`
	SearchesAfterTarget     int  `json:"searches_after_target"`
	RecallTokensAfterTarget int  `json:"recall_tokens_after_target"`
	TargetHeldAtEnd         bool `json:"target_held_at_end"`

	// Routing is where the model looked first when its memory was missing.
	Routing          string `json:"routing"`
	FirstRecallRound int    `json:"first_recall_round"`
	FirstEscapeRound int    `json:"first_escape_round"`
}

// hitPosition matches the address recall prints for a search hit. Reading our
// own rendered format, not guessing at the model's words.
var hitPosition = regexp.MustCompile(`(?m)^#(\d+) `)

// scoreRun reads the messages one turn appended and reports what happened.
// runScorer walks one turn's messages, holding the state a judgement needs:
// which calls are open, whether the target has come back, and which search
// each result belongs to.
type runScorer struct {
	m            contextMetrics
	inst         fixtureInstance
	target       int
	calls        map[string]provider.ToolCall
	pending      map[string]int
	round        int
	sawTargetHit bool
	// seen accumulates everything recall has shown the model, so sufficiency is
	// judged on what it could read rather than on which call produced it.
	seen       strings.Builder
	sufficient bool
}

// scoreRun reads the messages one turn appended and reports what happened.
func scoreRun(msgs []provider.Message, inst fixtureInstance, target int, arm string, cueVisible bool) contextMetrics {
	s := &runScorer{
		m:    contextMetrics{Task: inst.Task.ID, Arm: arm, CueVisible: cueVisible},
		inst: inst, target: target,
		calls: map[string]provider.ToolCall{}, pending: map[string]int{},
	}
	for _, msg := range msgs {
		if len(msg.ToolCalls) > 0 {
			s.round++
		}
		for _, tc := range msg.ToolCalls {
			s.calls[tc.ID] = tc
			s.observeCall(tc)
		}
		if msg.Role == provider.RoleTool {
			s.observeResult(msg)
		}
	}
	return s.finish(cueVisible)
}

// observeCall records one tool call: a recall search, a recall read, or a
// reach for the workspace.
func (s *runScorer) observeCall(tc provider.ToolCall) {
	m := &s.m
	if tc.Name != toolRecall {
		if !containsString(m.UnexpectedWorkTools, tc.Name) {
			m.UnexpectedWorkTools = append(m.UnexpectedWorkTools, tc.Name)
		}
		m.EscapeCalls++
		if s.sufficient {
			m.EscapesAfterSufficient++
		}
		if m.FirstEscapeRound == 0 {
			m.FirstEscapeRound = s.round
		}
		if m.SearchCalls+m.ReadCalls == 0 {
			m.EscapeBeforeFirstRecall++
		} else {
			m.EscapeAfterFirstRecall++
		}
		return
	}
	if m.FirstRecallRound == 0 {
		m.FirstRecallRound = s.round
	}
	query, positions := recallArgs(tc.Arguments)
	switch {
	case query != "":
		m.SearchCalls++
		if m.FirstSearchRound == 0 {
			m.FirstSearchRound = s.round
		}
		if s.sawTargetHit {
			m.PostHitSearches++
			m.SearchesAfterTarget++
		}
		if s.sufficient {
			m.SearchesAfterSufficient++
		}
		m.Searches = append(m.Searches, searchAttempt{Ordinal: m.SearchCalls, Round: s.round, Query: query})
		s.pending[tc.ID] = len(m.Searches) - 1
	case len(positions) > 0:
		m.ReadCalls++
		if s.sufficient {
			m.ReadsAfterSufficient++
		}
		if m.FirstReadRound == 0 {
			m.FirstReadRound = s.round
		}
		// A read covers the target when it names its position: recall returns
		// the tool results under the caller's own address.
		if containsInt(positions, s.target) {
			m.TargetRead = true
			switch {
			case s.sawTargetHit:
				m.ReadAfterHit, m.SearchThenRecallRead = true, true
			case m.SearchCalls == 0:
				m.RecallReadWithoutSearch = true
			}
		}
	}
}

// observeResult reads what a call returned: contamination, page-in cost, and
// whether this search brought the target back.
func (s *runScorer) observeResult(msg provider.Message) {
	m := &s.m
	call, ok := s.calls[msg.ToolCallID]
	if !ok {
		return
	}
	if call.Name != toolRecall {
		// Codenames only: a three-digit epoch appears in any long file by
		// chance, and one did. A model holding the codename holds the numbers
		// beside it anyway.
		for _, marker := range s.inst.AnswerMarkers {
			if len(marker) >= 6 && strings.Contains(msg.Content, marker) {
				m.Contaminated, m.LeakedVia, m.LeakedMarker = true, call.Name, marker
				m.EscapeFoundAnswer = true
				m.LeakedArgs, m.LeakedOutput = truncate(call.Arguments, 200), truncate(msg.Content, 300)
			}
		}
		return
	}
	cost := tokencount.Text(msg.Content)
	m.RecallReturnedTokens += cost
	if s.sawTargetHit {
		m.RecallTokensAfterTarget += cost
	}
	if s.sufficient {
		m.RecallTokensAfterSufficient += cost
	}
	// Everything recall showed the model, judged as a whole: the answer may
	// arrive across a snippet and a read, and either way it is readable.
	s.seen.WriteString(msg.Content)
	s.seen.WriteString("\n")
	if !s.sufficient && allMarkersIn(s.seen.String(), s.inst.AnswerMarkers) {
		s.sufficient = true
		m.FirstSufficientRound = s.round
		query, _ := recallArgs(call.Arguments)
		m.SufficientVia = "read"
		if query != "" {
			m.SufficientVia = "snippet"
		}
	}
	if query, _ := recallArgs(call.Arguments); query == "" {
		return
	}
	positions := searchHitPositions(msg.Content)
	if len(positions) > 0 {
		m.SearchHits++
	}
	rank := indexOfInt(positions, s.target)
	if i, ok := s.pending[msg.ToolCallID]; ok {
		m.Searches[i].TargetRank, m.Searches[i].ResultCount = rank, len(positions)
	}
	if rank == 0 {
		return
	}
	m.TargetSearchHits++
	if !s.sawTargetHit {
		m.TargetFirstSeenRound = s.round
		m.FirstHitCoverage = answerCoverage(msg.Content, s.inst.AnswerMarkers)
	}
	s.sawTargetHit = true
	if m.FirstTargetRank == 0 {
		m.FirstTargetRank = rank
	}
}

// finish derives what only the whole walk can say.
func (s *runScorer) finish(cueVisible bool) contextMetrics {
	m := s.m
	m.RetrievalRounds = m.SearchCalls + m.ReadCalls
	m.TargetHeldAtEnd = s.sawTargetHit || m.TargetRead
	if m.TargetFirstSeenRound > 0 {
		m.RoundsAfterTarget = s.round - m.TargetFirstSeenRound
	}
	m.Routing = routing(m.FirstRecallRound, m.FirstEscapeRound)
	m.CueDirectRead = cueVisible && m.RecallReadWithoutSearch
	if m.SufficientVia == "" {
		m.SufficientVia = "never"
	}
	if m.FirstSufficientRound > 0 {
		m.RoundsAfterSufficient = s.round - m.FirstSufficientRound
	}
	m.AnswerValues = len(s.inst.AnswerMarkers)
	m.StoppingClass = stoppingClass(m)
	classifySearches(&m, s.inst)
	return m
}

// scoreAnswer grades the final text on the task's markers. Nonces, matched
// exactly: no judge model, and nothing a general answer could satisfy.
func (m *contextMetrics) scoreAnswer(final string, inst fixtureInstance) {
	lower := strings.ToLower(final)
	for _, marker := range inst.AnswerMarkers {
		if !strings.Contains(lower, strings.ToLower(marker)) {
			m.MissingMarkers = append(m.MissingMarkers, marker)
		}
	}
	m.AnswerRecovered = len(m.MissingMarkers) == 0
	m.AnswerAmbiguous, m.AmbiguousVar = ambiguousAnswer(final, inst)
	m.FailureStage = m.stage()
	if len(final) > 1200 {
		final = final[:1200] + "…"
	}
	m.FinalAnswer = final
}

// stage places the run in the funnel. One label, chosen at the first step that
// did not happen, so a table of stages reads as where the retrieval broke.
func (m contextMetrics) stage() string {
	switch {
	case m.Contaminated:
		return stageContaminated
	case m.AnswerRecovered && m.RecallReadWithoutSearch:
		return stageCueRead
	case m.AnswerRecovered:
		return stageRecovered
	case m.TargetRead:
		return stageAnswerWrong
	case m.TargetSearchHits > 0:
		return stageHitNotRead
	case m.SearchCalls > 0:
		return stageSearchMiss
	case m.ReadCalls > 0:
		return stageReadMissed
	default:
		return stageNoRetrieval
	}
}

// recallArgs pulls the two fields that say which operation a recall call is.
func recallArgs(args string) (query string, positions []int) {
	var parsed struct {
		Query     string `json:"query"`
		Positions []int  `json:"positions"`
	}
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		return "", nil
	}
	return strings.TrimSpace(parsed.Query), parsed.Positions
}

// searchHitPositions reads the addresses out of a search result, in rank order.
func searchHitPositions(text string) []int {
	var out []int
	for _, match := range hitPosition.FindAllStringSubmatch(text, -1) {
		if n, err := strconv.Atoi(match[1]); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func containsInt(list []int, want int) bool { return slices.Contains(list, want) }

// indexOfInt returns a 1-based rank, or 0 when absent.
func indexOfInt(list []int, want int) int {
	for i, v := range list {
		if v == want {
			return i + 1
		}
	}
	return 0
}

func containsString(list []string, want string) bool { return slices.Contains(list, want) }

// funnel summarizes one arm's runs the way the experiment is read.
type funnel struct {
	Arm                                       string
	Runs                                      int
	Scored, Contaminated, Errored             int
	Searched, TargetHit, TargetRead, Answered int
	CueReads                                  int
	SearchCalls, ReadCalls, RecallTokens      int
	Escapes, EscapeCalls                      int
	Stages, Routes                            map[string]int
}

func summarize(arm string, runs []contextMetrics) funnel {
	f := funnel{Arm: arm, Runs: len(runs), Stages: map[string]int{}, Routes: map[string]int{}}
	for _, r := range runs {
		if r.Contaminated {
			f.Contaminated++
			f.Stages[r.FailureStage]++
			continue
		}
		if r.FailureStage == "RunError" {
			f.Errored++
			f.Stages[r.FailureStage]++
			continue
		}
		f.Scored++
		if r.SearchCalls > 0 {
			f.Searched++
		}
		if r.TargetSearchHits > 0 {
			f.TargetHit++
		}
		if r.TargetRead {
			f.TargetRead++
		}
		if r.AnswerRecovered {
			f.Answered++
		}
		if r.RecallReadWithoutSearch {
			f.CueReads++
		}
		if len(r.UnexpectedWorkTools) > 0 {
			f.Escapes++
		}
		f.EscapeCalls += r.EscapeCalls
		f.SearchCalls += r.SearchCalls
		f.ReadCalls += r.ReadCalls
		f.RecallTokens += r.RecallReturnedTokens
		f.Stages[r.FailureStage]++
		if r.FailureStage != "RunError" {
			f.Routes[r.Routing]++
		}
	}
	return f
}

func (f funnel) line() string {
	per := func(n int) float64 {
		if f.Scored == 0 {
			return 0
		}
		return float64(n) / float64(f.Scored)
	}
	line := fmt.Sprintf("%-14s runs=%-3d answered=%d/%d  searched=%d  target-hit=%d  cue-read=%d  search/task=%.2f  recall-tok/task=%.0f  escape/task=%.2f",
		f.Arm, f.Scored, f.Answered, f.Scored, f.Searched, f.TargetHit, f.CueReads,
		per(f.SearchCalls), per(f.RecallTokens), per(f.EscapeCalls))
	if f.Contaminated > 0 {
		line += fmt.Sprintf("  CONTAMINATED=%d", f.Contaminated)
	}
	if f.Errored > 0 {
		line += fmt.Sprintf("  ERRORED=%d", f.Errored)
	}
	return line
}

func writeJSONLine(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// routing says where the model looked first, comparing rounds rather than
// event order. Same round is a parallel fan-out, not a preference.
func routing(recallRound, escapeRound int) string {
	switch {
	case recallRound == 0 && escapeRound == 0:
		return routeNeither
	case recallRound == 0:
		return routeWorkspaceFirst
	case escapeRound == 0:
		return routeMemoryFirst
	case recallRound < escapeRound:
		return routeMemoryFirst
	case escapeRound < recallRound:
		return routeWorkspaceFirst
	default:
		return routeParallel
	}
}

// ambiguousAnswer reports an answer naming more than one candidate of a
// codename's shape. Only codenames are checked: they carry a stem this run
// declared, so two distinct "fence-…" values in one answer is one answer too
// many. Numbers have no such shape and are left alone.
func ambiguousAnswer(final string, inst fixtureInstance) (bool, string) {
	for _, spec := range inst.Task.Vars {
		if spec.Kind != varCodename {
			continue
		}
		seen := map[string]bool{}
		for _, match := range regexp.MustCompile(regexp.QuoteMeta(spec.Word)+`-[a-z0-9]{3,}`).FindAllString(final, -1) {
			seen[match] = true
		}
		if len(seen) > 1 {
			return true, spec.Name
		}
	}
	return false, ""
}

func allMarkersIn(text string, markers []string) bool {
	if len(markers) == 0 {
		return false
	}
	lower := strings.ToLower(text)
	for _, marker := range markers {
		if !strings.Contains(lower, strings.ToLower(marker)) {
			return false
		}
	}
	return true
}

// Stopping classes. The boundary is sufficiency, not the target coming back:
// a rank-1 hit whose snippet was too short to answer leaves a read entirely
// justified, and counting that as waste would blame the model for the bound.
const (
	stopSnippet       = "SnippetStop"             // enough arrived, and the turn ended
	stopSnippetRead   = "SnippetThenRead"         // the snippet was short; the read finished it
	stopDefensiveRead = "DefensiveRead"           // enough had arrived, one read confirmed it
	stopPostRetrieval = "PostSufficientRetrieval" // enough had arrived, and retrieval continued
	stopRunaway       = "PostSufficientRunaway"   // it continued for rounds, or ran out of budget
	stopNeverEnough   = "NeverSufficient"
)

// runawayRounds is where continuing stops looking like confirmation.
const runawayRounds = 3

func stoppingClass(m contextMetrics) string {
	if m.FirstSufficientRound == 0 {
		return stopNeverEnough
	}
	after := m.SearchesAfterSufficient + m.ReadsAfterSufficient + m.EscapesAfterSufficient
	switch {
	case after == 0 && m.SufficientVia == "snippet":
		return stopSnippet
	case after == 0:
		return stopSnippetRead
	case m.RoundsAfterSufficient >= runawayRounds || m.FailureStage == "RunError":
		return stopRunaway
	case after == 1 && m.ReadsAfterSufficient == 1:
		return stopDefensiveRead
	default:
		return stopPostRetrieval
	}
}

// reportStopping is the funnel that matters once retrieval succeeds: what the
// turn did after it already had everything it needed.
func reportStopping(all []contextMetrics) string {
	classes := map[string]int{}
	var extraRounds, extraTokens []float64
	sufficient, answeredNext := 0, 0
	for _, m := range all {
		if m.Contaminated {
			continue
		}
		classes[m.StoppingClass]++
		if m.FirstSufficientRound == 0 {
			continue
		}
		sufficient++
		extraRounds = append(extraRounds, float64(m.RoundsAfterSufficient))
		extraTokens = append(extraTokens, float64(m.RecallTokensAfterSufficient))
		if m.RoundsAfterSufficient <= 1 {
			answeredNext++
		}
	}
	var b strings.Builder
	b.WriteString("\n## After the evidence was already enough\n")
	fmt.Fprintf(&b, "  sufficient in %d runs; ended within one round in %d\n", sufficient, answeredNext)
	fmt.Fprintf(&b, "  extra rounds  mean %.1f  median %.1f  p90 %.0f  max %.0f\n",
		mean(extraRounds), median(extraRounds), percentile(extraRounds, 0.9), maxOf(extraRounds))
	fmt.Fprintf(&b, "  extra recall tokens  mean %.0f  median %.0f  p90 %.0f  max %.0f\n",
		mean(extraTokens), median(extraTokens), percentile(extraTokens, 0.9), maxOf(extraTokens))
	b.WriteString("  " + countsLine(classes) + "\n")
	return b.String()
}
