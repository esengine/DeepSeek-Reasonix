package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/fileutil"
	fileencoding "reasonix/internal/fileutil/encoding"
)

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
	note = oneLine(note)
	if note == "" {
		return nil
	}
	if err := ensureDestinationNotSymlink(path); err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := ensureDestinationNotSymlink(path); err != nil {
		return err
	}

	existing, _ := fileencoding.ReadFileUTF8(path) // missing → new file
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
	return writeDocBytes(path, []byte(out))
}

// writeDocFile overwrites path with body, creating the parent directory and
// ensuring a single trailing newline. Used by Set.WriteDoc for the panel's
// in-place editor (path validation happens in the caller).
func writeDocFile(path, body string) error {
	out := strings.TrimRight(body, "\n") + "\n"
	return writeDocBytes(path, []byte(out))
}

func writeDocBytes(path string, body []byte) error {
	if err := ensureDestinationNotSymlink(path); err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := ensureDestinationNotSymlink(path); err != nil {
		return err
	}

	return fileutil.AtomicWriteFile(path, body, 0o644)
}

// ensureDestinationNotSymlink rejects an existing final symlink. Ancestor
// symlinks may be legitimate platform/workspace aliases (for example macOS
// /var), while the sibling-temp atomic replace never follows a final link.
func ensureDestinationNotSymlink(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to write %q through a symlink", path)
	}
	return nil
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
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
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
