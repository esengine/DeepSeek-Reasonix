package pluginpkg

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"reasonix/internal/ext/extensioncontract"
)

// FrameRef is one frame document a package declares: an HTML file inside the
// package that the host serves into an isolated frame.
type FrameRef struct {
	ID    string `json:"id"`
	Entry string `json:"entry"`
	Title string `json:"title"`
}

// MaxFrameDocumentBytes caps one frame document.
const MaxFrameDocumentBytes = 2 << 20

const maxFrameTitle = 200

var (
	ErrFrameNotDeclared = errors.New("frame is not declared by the package")
	ErrFrameMissing     = errors.New("frame document is missing")
	ErrFrameOutsideRoot = errors.New("frame document is outside the package")
	ErrFrameTooLarge    = errors.New("frame document is too large")
	ErrFrameNotUTF8     = errors.New("frame document is not UTF-8 text")
)

// parseFrameList decodes contributes.frames. Entries are checked lexically
// here; validateFrameFiles checks them on disk.
func parseFrameList(raw json.RawMessage, at string) ([]FrameRef, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var frames []FrameRef
	if err := strictDecode(raw, &frames, at); err != nil {
		return nil, err
	}
	if len(frames) > extensioncontract.MaxFramesPerPackage {
		return nil, fmt.Errorf("%s: at most %d frames", at, extensioncontract.MaxFramesPerPackage)
	}
	seen := map[string]bool{}
	for i, f := range frames {
		where := fmt.Sprintf("%s[%d]", at, i)
		if !extensioncontract.ValidFrameID(f.ID) {
			return nil, fmt.Errorf("%s: id %q is not a lowercase slug of at most 32 characters", where, f.ID)
		}
		if seen[f.ID] {
			return nil, fmt.Errorf("%s: id %q is declared twice", where, f.ID)
		}
		seen[f.ID] = true
		if strings.TrimSpace(f.Title) == "" || len(f.Title) > maxFrameTitle {
			return nil, fmt.Errorf("%s: title must be 1 to %d characters", where, maxFrameTitle)
		}
		if !strings.HasSuffix(strings.ToLower(f.Entry), ".html") {
			return nil, fmt.Errorf("%s: entry %q must be an .html file", where, f.Entry)
		}
		if err := validateRelativePath(f.Entry); err != nil {
			return nil, fmt.Errorf("%s: %w", where, err)
		}
	}
	return frames, nil
}

// addFrames parses contributes.frames onto the manifest and checks each entry.
func addFrames(root string, m *Manifest, raw json.RawMessage) error {
	frames, err := parseFrameList(raw, "contributes.frames")
	if err != nil {
		return err
	}
	m.Frames = frames
	return validateFrameFiles(root, m)
}

// validateFrameFiles requires every frame entry to be a regular file whose
// resolved path stays inside the package, and a runtime that may publish UI.
func validateFrameFiles(root string, m *Manifest) error {
	if len(m.Frames) == 0 {
		return nil
	}
	if m.Runtime == nil || !slices.Contains(m.Runtime.Capabilities, "ui") {
		return fmt.Errorf("contributes.frames needs a runtime with the ui capability")
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		resolvedRoot = filepath.Clean(root)
	}
	for _, f := range m.Frames {
		abs := filepath.Join(root, filepath.FromSlash(f.Entry))
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return fmt.Errorf("frame %q entry %q cannot be resolved: %w", f.ID, f.Entry, err)
		}
		if !pathWithinRoot(resolvedRoot, resolved) {
			return fmt.Errorf("frame %q entry %q escapes the plugin root through a symlink", f.ID, f.Entry)
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return fmt.Errorf("frame %q entry %q is not readable: %w", f.ID, f.Entry, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("frame %q entry %q is not a regular file", f.ID, f.Entry)
		}
	}
	return nil
}

// Frame returns the declaration of one frame id.
func (p Package) Frame(id string) (FrameRef, bool) {
	for _, f := range p.Manifest.Frames {
		if f.ID == id {
			return f, true
		}
	}
	return FrameRef{}, false
}

// FrameDocument reads one declared frame document through a root opened on
// the package, so a file swapped for a link after install is refused rather
// than followed out of the package.
func (p Package) FrameDocument(id string) ([]byte, error) {
	f, ok := p.Frame(id)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrFrameNotDeclared, id)
	}
	root, err := os.OpenRoot(p.Root)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFrameMissing, err)
	}
	defer root.Close()
	rel := filepath.FromSlash(f.Entry)
	info, err := root.Lstat(rel)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("%w: %q", ErrFrameMissing, f.Entry)
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrFrameOutsideRoot, err)
	case info.Mode()&fs.ModeSymlink != 0, !info.Mode().IsRegular():
		return nil, fmt.Errorf("%w: %q is not a regular file", ErrFrameOutsideRoot, f.Entry)
	case info.Size() > MaxFrameDocumentBytes:
		return nil, fmt.Errorf("%w: %d bytes", ErrFrameTooLarge, info.Size())
	}
	file, err := root.Open(rel)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFrameOutsideRoot, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxFrameDocumentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFrameMissing, err)
	}
	if len(data) > MaxFrameDocumentBytes {
		return nil, fmt.Errorf("%w: more than %d bytes", ErrFrameTooLarge, MaxFrameDocumentBytes)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%w: %q", ErrFrameNotUTF8, f.Entry)
	}
	return data, nil
}
