// Subject keys: the knowledge-conflict model. A fact may declare which
// question it answers (project.package_manager, user.response_style); one
// scope holds at most one active value per subject, so a new answer becomes
// an update of the existing fact instead of a silent contradiction.
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NormalizeSubjectKey canonicalizes a subject key: lowercase, dot-separated
// segments of [a-z0-9_-], empty segments collapsed. Returns "" (no subject)
// for keys with no usable content so punctuation typos never mint a distinct
// identity.
func NormalizeSubjectKey(s string) string {
	var segments []string
	for seg := range strings.SplitSeq(strings.ToLower(strings.TrimSpace(s)), ".") {
		var b strings.Builder
		for _, r := range seg {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
				b.WriteRune(r)
			case r == ' ', r == '_':
				b.WriteRune('_')
			}
		}
		if cleaned := strings.Trim(b.String(), "_-"); cleaned != "" {
			segments = append(segments, cleaned)
		}
	}
	return strings.Join(segments, ".")
}

// findActiveSubject returns the active fact holding a subject key within one
// scope's directory, if any.
func (s Store) findActiveSubject(scope FactScope, key string) (Memory, bool) {
	dir := s.DirFor(scope)
	if dir == "" || key == "" {
		return Memory{}, false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Memory{}, false
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == indexFile || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		m, ok := loadMemory(filepath.Join(dir, e.Name()))
		if !ok || NormalizeSubjectKey(m.SubjectKey) != key {
			continue
		}
		if m.Scope == "" {
			m.Scope = s.scopeForDir(dir)
		}
		return m, true
	}
	return Memory{}, false
}

// validateSubjectKey enforces one active value per (scope, subject): a save
// claiming an already-held subject is rejected with directions to update the
// holder, so "npm -> pnpm" becomes a revision of one fact, not a second
// contradicting fact. The error is written for the model to act on.
func (s Store) validateSubjectKey(m Memory) error {
	key := NormalizeSubjectKey(m.SubjectKey)
	if key == "" {
		return nil
	}
	holder, ok := s.findActiveSubject(NormalizeFactScope(string(m.Scope)), key)
	if !ok || holder.ID == m.ID {
		return nil
	}
	return fmt.Errorf(
		"subject %q is already tracked by memory id=%s revision=%d name=%s (current: %s); "+
			"if this is the new value of the same fact, update that id instead of creating a second one",
		key, holder.ID, holder.Revision, holder.Name, oneLine(firstNonEmpty(holder.Description, holder.Body)))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
