package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"reasonix/internal/event"
)

type acceptedDecision struct {
	ID        string
	Question  string
	Answer    string
	Ambiguity decisionAmbiguity
}

type decisionAmbiguity struct {
	Headers map[string]struct{}
	Options map[string]struct{}
	Terms   map[string]struct{}
}

func decisionIDForQuestions(qs []event.AskQuestion) string {
	h := sha256.New()
	for _, q := range qs {
		_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(q.Prompt))))
		_, _ = h.Write([]byte{0})
		for _, opt := range q.Options {
			_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(opt.Label))))
			_, _ = h.Write([]byte{0})
		}
	}
	return "dec-" + hex.EncodeToString(h.Sum(nil)[:8])
}

type turnStateContextKey struct{}

func withTurnState(ctx context.Context, turn *turnRuntime) context.Context {
	if turn == nil {
		return ctx
	}
	return context.WithValue(ctx, turnStateContextKey{}, turn)
}

func turnStateFrom(ctx context.Context) *turnRuntime {
	turn, _ := ctx.Value(turnStateContextKey{}).(*turnRuntime)
	return turn
}

func rememberDecisionForQuestions(ctx context.Context, id, question, answer string, qs []event.AskQuestion) {
	turn := turnStateFrom(ctx)
	if turn == nil || id == "" {
		return
	}
	turn.loop.rememberDecisionAmbiguity(id, question, answer, decisionAmbiguityForQuestions(qs))
}

func existingDecision(ctx context.Context, id string) (acceptedDecision, bool) {
	turn := turnStateFrom(ctx)
	if turn == nil || id == "" {
		return acceptedDecision{}, false
	}
	return turn.loop.decision(id)
}

func firstExistingDecision(ctx context.Context) (acceptedDecision, bool) {
	turn := turnStateFrom(ctx)
	if turn == nil {
		return acceptedDecision{}, false
	}
	decisions := turn.loop.snapshotDecisions()
	if len(decisions) == 0 {
		return acceptedDecision{}, false
	}
	return decisions[0], true
}

func matchingExistingDecision(ctx context.Context, qs []event.AskQuestion) (acceptedDecision, bool) {
	turn := turnStateFrom(ctx)
	if turn == nil {
		return acceptedDecision{}, false
	}
	candidate := decisionAmbiguityForQuestions(qs)
	for _, decision := range turn.loop.snapshotDecisions() {
		if sameDecisionAmbiguity(decision.Ambiguity, candidate) {
			return decision, true
		}
	}
	return acceptedDecision{}, false
}

func decisionAmbiguityForQuestions(qs []event.AskQuestion) decisionAmbiguity {
	out := decisionAmbiguity{
		Headers: map[string]struct{}{},
		Options: map[string]struct{}{},
		Terms:   map[string]struct{}{},
	}
	for _, q := range qs {
		addDecisionPhrase(out.Headers, q.Header)
		addDecisionTerms(out.Terms, q.Header)
		addDecisionTerms(out.Terms, q.Prompt)
		for _, option := range q.Options {
			addDecisionPhrase(out.Options, option.Label)
			addDecisionTerms(out.Terms, option.Label)
		}
	}
	return out
}

func addDecisionPhrase(dst map[string]struct{}, value string) {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		dst[b.String()] = struct{}{}
	}
}

func addDecisionTerms(dst map[string]struct{}, value string) {
	var latin strings.Builder
	flush := func() {
		if latin.Len() > 1 {
			dst[latin.String()] = struct{}{}
		}
		latin.Reset()
	}
	for _, r := range strings.ToLower(value) {
		switch {
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
			flush()
			dst[string(r)] = struct{}{}
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			latin.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
}

func sameDecisionAmbiguity(left, right decisionAmbiguity) bool {
	if len(left.Terms) == 0 || len(right.Terms) == 0 {
		return false
	}
	headerMatch := setIntersection(left.Headers, right.Headers) > 0
	optionSimilarity := setJaccard(left.Options, right.Options)
	termSimilarity := setJaccard(left.Terms, right.Terms)
	return (headerMatch && (optionSimilarity >= 0.5 || termSimilarity >= 0.45)) ||
		(optionSimilarity >= 0.75 && termSimilarity >= 0.35) || termSimilarity >= 0.7
}

func setIntersection(left, right map[string]struct{}) int {
	count := 0
	for value := range left {
		if _, ok := right[value]; ok {
			count++
		}
	}
	return count
}

func setJaccard(left, right map[string]struct{}) float64 {
	union := len(left) + len(right)
	if union == 0 {
		return 0
	}
	intersection := setIntersection(left, right)
	return float64(intersection) / float64(union-intersection)
}

type askArgs struct {
	DecisionID string `json:"decision_id"`
	Evidence   string `json:"new_evidence"`
	Questions  []struct {
		Header      string `json:"header"`
		Question    string `json:"question"`
		MultiSelect bool   `json:"multiSelect"`
		Options     []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"options"`
	} `json:"questions"`
}

func parseAskArgs(raw json.RawMessage) (askArgs, error) {
	var p askArgs
	if err := json.Unmarshal(raw, &p); err != nil {
		return askArgs{}, fmt.Errorf("invalid args: %w", err)
	}
	if len(p.Questions) == 0 {
		return askArgs{}, fmt.Errorf("at least one question is required")
	}
	if len(p.Questions) > 3 {
		return askArgs{}, fmt.Errorf("at most 3 questions may be asked in one clarification")
	}
	return p, nil
}
