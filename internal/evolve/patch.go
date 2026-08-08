package evolve

import (
	"strings"

	"reasonix/internal/diff"
)

const (
	sectionConventions = "## Conventions"
	sectionNotes       = "## Notes"
)

// PatchStandingDoc inserts a short bullet into the standing instruction document.
// Prefers ## Conventions, then ## Notes, otherwise appends a ## Notes section.
// Returns the new full body and a unified diff Change for preview/tests.
func PatchStandingDoc(path, oldBody, bullet string) (newBody string, change diff.Change) {
	bullet = strings.TrimSpace(bullet)
	if bullet != "" && !strings.HasPrefix(bullet, "-") {
		bullet = "- " + bullet
	}
	newBody = insertStandingBullet(oldBody, bullet)
	kind := diff.Modify
	if strings.TrimSpace(oldBody) == "" {
		kind = diff.Create
	}
	change = diff.Build(path, oldBody, newBody, kind)
	return newBody, change
}

func insertStandingBullet(body, bullet string) string {
	if strings.TrimSpace(bullet) == "" {
		return body
	}
	body = strings.ReplaceAll(body, "\r\n", "\n")
	if strings.TrimSpace(body) == "" {
		return "# Project memory\n\n" + sectionNotes + "\n\n" + bullet + "\n"
	}
	// Prefer Conventions when present.
	if hasHeading(body, sectionConventions) {
		return ensureTrailingNewline(insertUnderHeading(body, sectionConventions, bullet))
	}
	if hasHeading(body, sectionNotes) {
		return ensureTrailingNewline(insertUnderHeading(body, sectionNotes, bullet))
	}
	// Create Notes section at end.
	return ensureTrailingNewline(strings.TrimRight(body, "\n") + "\n\n" + sectionNotes + "\n\n" + bullet + "\n")
}

func hasHeading(body, heading string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == heading {
			return true
		}
	}
	return false
}

func insertUnderHeading(body, heading, bullet string) string {
	lines := strings.Split(body, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == heading {
			start = i
			break
		}
	}
	if start < 0 {
		return strings.TrimRight(body, "\n") + "\n\n" + bullet + "\n"
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
			end = i
			break
		}
	}
	// Skip if an identical bullet already exists in the section.
	for i := start + 1; i < end; i++ {
		if strings.TrimSpace(lines[i]) == strings.TrimSpace(bullet) {
			return body
		}
	}
	insert := end
	for insert > start+1 && strings.TrimSpace(lines[insert-1]) == "" {
		insert--
	}
	out := append([]string{}, lines[:insert]...)
	out = append(out, bullet)
	out = append(out, lines[insert:]...)
	return strings.Join(out, "\n")
}

func ensureTrailingNewline(s string) string {
	if s == "" {
		return "\n"
	}
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
