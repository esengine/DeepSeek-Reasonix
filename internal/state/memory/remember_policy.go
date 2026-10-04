package memory

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"

	"reasonix/internal/base/secrets"
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

// AssessRememberSkip reports whether the user's switch may let this write through
// without the dialog. It keeps every bound the low-risk path keeps — a bounded
// body, no secrets, an explicitly classified project or reference fact — and
// widens exactly one thing: an update of an existing project fact. A global fact,
// a preference and an unresolvable target all keep asking.
func AssessRememberSkip(store Store, args json.RawMessage) RememberAssessment {
	in, err := parseRememberRequest(args)
	if err != nil {
		return RememberAssessment{Reason: "invalid remember request"}
	}
	ref := parseMemoryReference(rememberRequestName(in))
	assessment := RememberAssessment{Name: ref.name, Type: NormalizeType(in.Type)}
	scope, ok := rememberTargetScope(store, in, ref)
	if !ok {
		assessment.Reason = "the memory this call targets cannot be resolved"
		return assessment
	}
	assessment.Scope = scope
	if assessment.Scope != FactScopeProject {
		assessment.Reason = "global memory requires confirmation"
		return assessment
	}
	if assessment.Name == "" {
		// An id-only call still has a name to report: the stored fact's own.
		id := strings.TrimSpace(in.ID)
		for _, stored := range store.ListAll() {
			if id != "" && stored.ID == id {
				assessment.Name = stored.Name
				break
			}
		}
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
	// Setting or changing activation is what pins a body into every later
	// session's prefix, which is a different promise than a bounded fact.
	if strings.TrimSpace(in.Activation) != "" {
		assessment.Reason = "pinned memory requires confirmation"
		return assessment
	}
	// Rewriting a fact that is already pinned is the same promise again: its
	// body rides every later session's prefix unless someone says otherwise.
	if stored, ok := rememberStoredFact(store, in, ref); ok && ResolveActivation(stored) == ActivationPinned {
		assessment.Reason = "a pinned memory requires confirmation"
		return assessment
	}
	// Rewriting one memory is this switch's business; minting a near
	// duplicate of another is not, so the same shape under a different
	// name still asks.
	if rememberRequestOverlapsAnother(store, in, assessment.Name) {
		assessment.Reason = "another memory already covers this fact"
		return assessment
	}
	assessment.AutoAllow = true
	assessment.Reason = "a bounded, non-sensitive project fact"
	return assessment
}

// rememberTargetScope resolves the scope a remember call would write and says
// whether it resolved at all. A fact the store already has speaks first: a call
// that restates a different scope than the stored one is a conflict rather than
// a move, and a record that states no scope stays unresolved.
func rememberTargetScope(store Store, in rememberRequest, ref memoryReference) (FactScope, bool) {
	if id := strings.TrimSpace(in.ID); id != "" {
		for _, stored := range store.ListAll() {
			if stored.ID != id {
				continue
			}
			return rememberStoredScope(in, ref, stored)
		}
		return FactScopeProject, false
	}
	if name := ref.name; name != "" {
		for _, stored := range store.ListAll() {
			if slug(stored.Name) != name {
				continue
			}
			return rememberStoredScope(in, ref, stored)
		}
	}
	if ref.qualified {
		return ref.scope, true
	}
	if scope := strings.TrimSpace(in.Scope); scope != "" {
		return NormalizeFactScope(scope), true
	}
	return FactScopeProject, true
}

// rememberStoredScope answers with the scope the fact already has, unless the
// call restates a different one — a project scope on a global fact is exactly
// the write that would move or delete it.
func rememberStoredScope(in rememberRequest, ref memoryReference, stored Memory) (FactScope, bool) {
	scope, ok := storedScope(stored)
	if !ok {
		return FactScopeProject, false
	}
	if ref.qualified && ref.scope != scope {
		return FactScopeProject, false
	}
	if asked := strings.TrimSpace(in.Scope); asked != "" && NormalizeFactScope(asked) != scope {
		return FactScopeProject, false
	}
	return scope, true
}

// storedScope is the scope a stored fact carries, and says whether the record
// stated one: an unstated scope stays unresolved, so the call keeps asking.
func storedScope(stored Memory) (FactScope, bool) {
	raw := strings.TrimSpace(string(stored.Scope))
	if raw == "" {
		return FactScopeProject, false
	}
	return NormalizeFactScope(raw), true
}

// rememberRequestOverlapsAnother reports whether a fact other than this call's own
// target already covers what it says: rewriting a memory is the switch's business,
// minting a near-duplicate of another is not.
func rememberRequestOverlapsAnother(store Store, in rememberRequest, name string) bool {
	wantTitle := normalizedMemoryPhrase(in.Title)
	wantDescription := normalizedMemoryPhrase(in.Description)
	for _, existing := range store.ListAll() {
		if slug(existing.Name) == name {
			continue
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

// rememberStoredFact returns the fact this call would rewrite, when the store has
// it: by stable id, else by the name the call addresses.
func rememberStoredFact(store Store, in rememberRequest, ref memoryReference) (Memory, bool) {
	if id := strings.TrimSpace(in.ID); id != "" {
		for _, stored := range store.ListAll() {
			if stored.ID == id {
				return stored, true
			}
		}
		return Memory{}, false
	}
	if name := ref.name; name != "" {
		for _, stored := range store.ListAll() {
			if slug(stored.Name) == name {
				return stored, true
			}
		}
	}
	return Memory{}, false
}
