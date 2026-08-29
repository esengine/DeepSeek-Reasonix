package memory

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"reasonix/internal/retrieval"
	"reasonix/internal/secrets"
)

const maxAutoRememberBodyRunes = 6000

var rememberEmailPattern = regexp.MustCompile(`(?i)\b[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+\b`)

// RememberAssessment explains whether an interactive host may safely allow a
// remember call without a confirmation dialog.
type RememberAssessment struct {
	AutoAllow bool
	Reason    string
	Name      string
	Type      Type
	Scope     FactScope
}

// AssessRememberWrite permits only bounded, non-sensitive project/reference
// creates. Global facts, preferences, feedback, updates, and potential
// duplicates remain explicit user decisions.
func AssessRememberWrite(store Store, args json.RawMessage) RememberAssessment {
	in, err := parseRememberRequest(args)
	if err != nil {
		return RememberAssessment{Reason: "invalid remember request"}
	}
	ref := parseMemoryReference(rememberRequestName(in))
	assessment := RememberAssessment{
		Name:  ref.name,
		Type:  NormalizeType(in.Type),
		Scope: NormalizeFactScope(in.Scope),
	}
	if ref.qualified {
		if strings.TrimSpace(in.Scope) != "" && assessment.Scope != ref.scope {
			assessment.Reason = "memory reference scope conflicts with explicit scope"
			return assessment
		}
		assessment.Scope = ref.scope
	}
	if strings.TrimSpace(in.Description) == "" || strings.TrimSpace(in.Body) == "" {
		assessment.Reason = "description and body are required"
		return assessment
	}
	if store.Dir == "" {
		assessment.Reason = "project memory store is unavailable"
		return assessment
	}
	typ := strings.ToLower(strings.TrimSpace(in.Type))
	if typ != string(TypeProject) && typ != string(TypeReference) {
		assessment.Reason = "only explicitly classified project/reference facts are low-risk"
		return assessment
	}
	if assessment.Scope != FactScopeProject {
		assessment.Reason = "global memory requires confirmation"
		return assessment
	}
	if strings.TrimSpace(in.ID) != "" || in.ExpectedRevision > 0 {
		assessment.Reason = "memory updates require confirmation"
		return assessment
	}
	if assessment.Name == "" {
		assessment.Reason = "memory name cannot be derived"
		return assessment
	}
	if len([]rune(in.Body)) > maxAutoRememberBodyRunes {
		assessment.Reason = "memory body exceeds the automatic-write budget"
		return assessment
	}
	if rememberRequestSensitive(in) {
		assessment.Reason = "memory may contain sensitive information"
		return assessment
	}
	if rememberRequestOverlaps(store, in, assessment.Name) {
		assessment.Reason = "an existing memory may already cover this fact"
		return assessment
	}
	assessment.AutoAllow = true
	assessment.Reason = "new low-risk project fact"
	return assessment
}

func rememberRequestSensitive(in rememberRequest) bool {
	text := strings.Join([]string{in.Name, in.Title, in.Description, in.Body}, "\n")
	if secrets.Redact(text) != text || rememberEmailPattern.MatchString(text) {
		return true
	}
	upper := strings.ToUpper(text)
	return strings.Contains(upper, "BEGIN PRIVATE KEY") || strings.Contains(upper, "BEGIN OPENSSH PRIVATE KEY")
}

func rememberRequestOverlaps(store Store, in rememberRequest, name string) bool {
	for _, existing := range store.ListAll() {
		if slug(existing.Name) == name {
			return true
		}
		if in.Title != "" && phraseOverlaps(existing.Title, in.Title) {
			return true
		}
		if in.Description != "" && phraseOverlaps(existing.Description, in.Description) {
			return true
		}
	}
	return false
}

// phraseOverlaps reports whether two fact titles/descriptions are
// near-duplicates: normalized equality or containment wins; otherwise distinct
// token overlap above a conservative threshold counts as covering, so a single
// shared generic bigram never suppresses a distinct fact and pure synonymy
// without lexical overlap is left to the user.
func phraseOverlaps(a, b string) bool {
	na, nb := normalizedMemoryPhrase(a), normalizedMemoryPhrase(b)
	if na == "" || nb == "" {
		return false
	}
	if na == nb {
		return true
	}
	// Containment: a short normalized phrase hiding inside a longer one is
	// the same claim with extra words. Guard the minimum rune count so a
	// generic short word ("ok", "x", 2-char CJK) cannot absorb an unrelated
	// longer description — byte length would let 2-char CJK words through.
	const minContainedRunes = 4
	la, lb := utf8.RuneCountInString(na), utf8.RuneCountInString(nb)
	if la >= minContainedRunes && lb >= minContainedRunes &&
		(strings.Contains(na, nb) || strings.Contains(nb, na)) {
		return true
	}
	// Length gate: a phrase far longer than the other cannot reach the token
	// overlap threshold below (containment above already handles the
	// short-in-long case). Without it, every existing memory pays
	// tokenization on every auto-write.
	if la > 6*lb || lb > 6*la {
		return false
	}
	ta, tb := retrieval.Tokens(a), retrieval.Tokens(b)
	if len(ta) < 2 || len(tb) < 2 {
		return false
	}
	// Overlap counts each shared token once on both sides; counting a
	// repeated token against a deduped set would let multiplicity inflate
	// the ratio ("包管理" vs "管理管理").
	sa := make(map[string]struct{}, len(ta))
	for _, tok := range ta {
		sa[tok] = struct{}{}
	}
	sb := make(map[string]struct{}, len(tb))
	for _, tok := range tb {
		sb[tok] = struct{}{}
	}
	common := 0
	for tok := range sa {
		if _, ok := sb[tok]; ok {
			common++
		}
	}
	smaller := len(sa)
	if len(sb) < smaller {
		smaller = len(sb)
	}
	// Two-thirds of distinct tokens must overlap. Tighter than half, so a
	// shared CJK domain prefix ("数据库迁移" vs "数据库备份", 4 bigrams
	// sharing 2) does not suppress a distinct fact — the differing suffix
	// carries the meaning. Coarse-grained English words still dedupe at the
	// same ratio ("uses database migrations" vs "uses database backups").
	return common >= 2 && common*3 >= smaller*2
}

func normalizedMemoryPhrase(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, value)
}
