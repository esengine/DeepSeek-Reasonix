package memory

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// FactReadError identifies one active fact file that exists but could not be
// read. It is the fact-level counterpart of DirectoryReadError: the file is
// present, so no caller may report it as absent, and its bytes are unknown, so
// a write issued over it must stop instead of minting a new fact in its place.
type FactReadError struct {
	Path string
	Err  error
}

func (e *FactReadError) Error() string {
	return fmt.Sprintf("read memory fact %q: %v", e.Path, e.Err)
}

func (e *FactReadError) Unwrap() error { return e.Err }

// Name is the fact's filename stem. For an unreadable file that stem is the
// only identity left: the frontmatter that normally carries the name cannot be
// parsed. It is therefore what a by-name lookup can still match, and what a
// reader sees in place of the title.
func (e *FactReadError) Name() string {
	return slug(strings.TrimSuffix(filepath.Base(e.Path), ".md"))
}

// ListReport is one read of the active store: the facts that were readable and
// the fact files that are present but could not be read. Surfaces that can name
// an unreadable file must read this instead of ListAll, which returns the facts
// alone and cannot say why one is missing.
type ListReport struct {
	Facts      []Memory
	Unreadable []*FactReadError
}

// ListActive reads the active store once and separates the facts that were
// readable from the fact files that are present but could not be read. A
// missing directory is still empty; a directory I/O error is still reported for
// the whole list; an unreadable file is reported beside the facts that loaded.
func (s Store) ListActive() (ListReport, error) {
	if s.Dir == "" && s.GlobalDir == "" {
		return ListReport{}, nil
	}
	var report ListReport
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		entries, err := readMemoryDirectory(dir)
		if err != nil {
			return ListReport{}, err
		}
		for _, entry := range entries {
			if entry.IsDir() || entry.Name() == indexFile || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			memory, ok, readErr := loadMemory(filepath.Join(dir, entry.Name()))
			if readErr != nil {
				report.Unreadable = append(report.Unreadable, readErr)
				continue
			}
			if !ok {
				continue
			}
			if memory.Scope == "" {
				memory.Scope = s.scopeForDir(dir)
			}
			report.Facts = append(report.Facts, memory)
		}
	}
	slices.SortFunc(report.Facts, func(a, b Memory) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Scope, b.Scope), cmp.Compare(a.ID, b.ID))
	})
	// ReadDir sorts within one directory; both scopes merge here.
	slices.SortFunc(report.Unreadable, func(a, b *FactReadError) int {
		return cmp.Compare(a.Path, b.Path)
	})
	return report, nil
}
