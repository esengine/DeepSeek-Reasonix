package memory

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// quickAddMu serializes AppendDoc writes so concurrent "#" quick-adds don't
// interleave their read-modify-write and clobber each other's note.
var quickAddMu sync.Mutex

// quickAddHeading marks the section quick-added notes accumulate under, so
// repeated "#" additions group together instead of scattering through a
// hand-written file.
const quickAddHeading = "## Notes"

// AppendDoc appends a one-line note as a bullet under a "## Notes" section in
// the doc-memory file at path, creating the file (and section) when absent. The
// note is normalised to a single line so it can't corrupt the section. This is
// the write side of the "#" quick-add: a plain file edit the user can later
// reorganise by hand.
func AppendDoc(path, note string) error {
	quickAddMu.Lock()
	defer quickAddMu.Unlock()

	note = oneLine(note)
	if note == "" {
		return nil
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	existing, _ := os.ReadFile(path) // missing → new file
	body := string(existing)
	bullet := "- " + note

	var out string
	switch {
	case strings.TrimSpace(body) == "":
		out = "# Project memory\n\n" + quickAddHeading + "\n\n" + bullet + "\n"
	case strings.Contains(body, quickAddHeading):
		// Insert the bullet at the end of the existing Notes section (before the
		// next heading, or at EOF), keeping additions chronological.
		out = insertUnderHeading(body, quickAddHeading, bullet)
	default:
		out = strings.TrimRight(body, "\n") + "\n\n" + quickAddHeading + "\n\n" + bullet + "\n"
	}
	return os.WriteFile(path, []byte(out), 0o644)
}

// writeDocFile overwrites path with body, creating the parent directory and
// ensuring a single trailing newline. Used by Set.WriteDoc for the panel's
// in-place editor (path validation happens in the caller).
func writeDocFile(path, body string) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	out := strings.TrimRight(body, "\n") + "\n"
	return os.WriteFile(path, []byte(out), 0o644)
}

// isATXHeading reports whether a line is a CommonMark ATX heading: 1–6 "#"
// followed by a space or end-of-line. A line like "#tag" (no space) is NOT a
// heading — it's a tag — so the section scanner won't stop at it and bullets
// appended under "## Notes" stay inside that section even when a "#tag" line
// appears later.
func isATXHeading(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "#") {
		return false
	}
	n := 0
	for _, r := range t {
		if r != '#' {
			break
		}
		n++
	}
	if n > 6 {
		return false
	}
	rest := t[n:]
	return rest == "" || strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\t")
}

// insertUnderHeading appends bullet to the end of the section started by heading
// — just before the next "## "/"# " heading, or at end of file if none follows.
func insertUnderHeading(body, heading, bullet string) string {
	lines := strings.Split(body, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == heading {
			start = i
			break
		}
	}
	if start < 0 { // shouldn't happen (caller checked Contains), but stay safe
		return strings.TrimRight(body, "\n") + "\n\n" + bullet + "\n"
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if isATXHeading(lines[i]) {
			end = i
			break
		}
	}
	// Trim trailing blank lines within the section, then place the bullet.
	insert := end
	for insert > start+1 && strings.TrimSpace(lines[insert-1]) == "" {
		insert--
	}
	out := append([]string{}, lines[:insert]...)
	out = append(out, bullet)
	out = append(out, lines[insert:]...)
	return strings.Join(out, "\n")
}
