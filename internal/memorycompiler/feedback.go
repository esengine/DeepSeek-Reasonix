package memorycompiler

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/fileutil"
	fileencoding "reasonix/internal/fileutil/encoding"
)

// OutcomeRef identifies one finished turn's persisted outcome so the next user
// message can retroactively revise it when the follow-up contradicts a recorded
// success.
type OutcomeRef struct {
	TraceID    string
	Strategy   string
	Outcome    string
	Injected   bool
	FinishedAt time.Time
}

// OutcomeRef captures the finished turn's persisted identity. Valid only after
// Finish; before that the outcome is empty and revision is a no-op.
func (t *Turn) OutcomeRef() OutcomeRef {
	if t == nil {
		return OutcomeRef{}
	}
	strategy := strings.TrimSpace(t.strategy)
	if strategy == "" {
		strategy = classifyStrategy(t.trace.Goal)
	}
	return OutcomeRef{
		TraceID:    t.trace.ID,
		Strategy:   strategy,
		Outcome:    t.trace.Outcome,
		Injected:   t.trace.Injected,
		FinishedAt: t.trace.CompletedAt,
	}
}

// correctiveExclusions are compounds that contain a corrective phrase but do
// not report a failed result ("不对外" exposes an API, it does not say the fix
// was wrong). They are blanked before pattern matching.
var correctiveExclusions = []string{"不对外", "不对称", "不对齐", "不对等"}

var correctivePatterns = []string{
	// zh
	"不对", "不是这样", "还是错", "还是报错", "还是不行", "还是失败", "还是有问题",
	"还是老样子", "没修好", "没有修好", "没修复", "没有修复", "没生效", "没有生效",
	"并没有修", "修错", "改错了", "问题依旧", "问题还在", "又报错", "又出错",
	// en
	"still broken", "still fails", "still failing", "still errors", "still the same",
	"not fixed", "didn't fix", "did not fix", "doesn't work", "does not work",
	"didn't work", "did not work", "wrong fix", "that's wrong", "that is wrong",
	"regressed", "made it worse",
}

// IsCorrectiveFeedback reports whether input reads as the user telling the
// agent the previous result was wrong. Matching is confined to the head of the
// message so a long new task that merely mentions an old failure deeper in its
// body does not trigger retroactive revision.
func IsCorrectiveFeedback(input string) bool {
	head := strings.ToLower(strings.TrimSpace(input))
	if head == "" {
		return false
	}
	if runes := []rune(head); len(runes) > 160 {
		head = string(runes[:160])
	}
	for _, ex := range correctiveExclusions {
		head = strings.ReplaceAll(head, ex, " ")
	}
	for _, p := range correctivePatterns {
		if strings.Contains(head, p) {
			return true
		}
	}
	return false
}

// ReviseOutcomeFromFeedback downgrades a previously recorded outcome to failure
// after the user's follow-up message reported the result wrong. The revision is
// applied only when the referenced trace is still present in this runtime's
// trace log, so a stale ref from another project directory cannot corrupt
// strategy counters. Returns true when a revision was persisted.
func (r *Runtime) ReviseOutcomeFromFeedback(ref OutcomeRef, feedback string) bool {
	if r == nil || strings.TrimSpace(ref.TraceID) == "" {
		return false
	}
	switch ref.Outcome {
	case "success", "partial_success":
	default:
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	reason := "user feedback contradicted recorded outcome: " + summarizeText(firstLine(feedback), 120)
	if !reviseTraceOutcomeInJSONL(filepath.Join(r.dir, tracesFile), ref.TraceID, "failure", reason) {
		return false
	}
	st := r.loadStateLocked()
	st.Strategies = ensureBuiltInStrategies(st.Strategies)
	// partial_success already counted as a strategy failure, so only a
	// recorded success moves the counters.
	if ref.Outcome == "success" {
		for i := range st.Strategies {
			if st.Strategies[i].ID != ref.Strategy {
				continue
			}
			if st.Strategies[i].Successes > 0 {
				st.Strategies[i].Successes--
			}
			st.Strategies[i].Failures++
			if ref.Injected {
				if st.Strategies[i].InjectedSuccesses > 0 {
					st.Strategies[i].InjectedSuccesses--
				}
				st.Strategies[i].InjectedFailures++
			}
			break
		}
	}
	// ":revision" keeps this entry distinct from the turn's own learning
	// (appendLearning dedupes by trace ID) and caps revisions at one per trace.
	st.Learnings = appendLearning(st.Learnings, SystemLearning{
		TraceID:              ref.TraceID + ":revision",
		BadStrategies:        []string{ref.Strategy},
		CausalFindings:       []string{"user follow-up contradicted trace " + ref.TraceID + " recorded as " + ref.Outcome},
		CompilerImprovements: []string{"verify " + ref.Strategy + " results before reporting success; the user reported the previous result wrong"},
		CreatedAt:            time.Now().UTC(),
	})
	st.UpdatedAt = time.Now().UTC()
	return writeJSON(filepath.Join(r.dir, stateFile), st) == nil
}

func reviseTraceOutcomeInJSONL(path, traceID, outcome, reason string) bool {
	b, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return false
	}
	lines := bytes.Split(bytes.TrimRight(b, "\n"), []byte("\n"))
	found := false
	for i, ln := range lines {
		var tr ExecutionTrace
		if json.Unmarshal(ln, &tr) != nil || tr.ID != traceID {
			continue
		}
		tr.Outcome = outcome
		tr.FailureReason = reason
		nb, err := json.Marshal(tr)
		if err != nil {
			return false
		}
		lines[i] = nb
		found = true
		break
	}
	if !found {
		return false
	}
	out := append(bytes.Join(lines, []byte("\n")), '\n')
	return fileutil.AtomicWriteFile(path, out, 0o600) == nil
}
