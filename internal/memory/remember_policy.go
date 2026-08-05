package memory

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"

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
	wantTitle := normalizedMemoryPhrase(in.Title)
	wantDescription := normalizedMemoryPhrase(in.Description)
	for _, existing := range store.ListAll() {
		if slug(existing.Name) == name {
			return true
		}
		if wantTitle != "" && normalizedMemoryPhrase(existing.Title) == wantTitle {
			return true
		}
		if wantDescription != "" && normalizedMemoryPhrase(existing.Description) == wantDescription {
			return true
		}
	}
	return false
}

func normalizedMemoryPhrase(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, value)
}
