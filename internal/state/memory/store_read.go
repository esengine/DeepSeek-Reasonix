package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

func (s Store) Read(ref string) (Memory, bool, error) {
	memory, _, ok, err := s.findActive(ref)
	return memory, ok, err
}

func (s Store) findSaveTarget(id, name string) (Memory, string, bool, error) {
	if id == "" {
		return s.findActive(name)
	}
	active, path, found, err := s.findActive(id)
	if err != nil {
		return active, path, found, err
	}
	// A save by id can be the retry after a rename was killed between retiring
	// the old file and publishing the new one: the fact's only copies are then
	// hidden artifacts, and reporting not-found would strand them.
	if !found {
		if active, path, found, err = s.recoverInterruptedRename(id); err != nil {
			return Memory{}, "", false, err
		}
	}
	if !found {
		err = fmt.Errorf("memory id %q not found", id)
	}
	return active, path, found, err
}

func (s Store) findActive(ref string) (Memory, string, bool, error) {
	parsed := parseMemoryReference(ref)
	if parsed.raw == "" {
		return Memory{}, "", false, nil
	}
	if parsed.qualified {
		return s.findActiveInDir(s.DirFor(parsed.scope), parsed.raw)
	}
	for _, v := range slices.Backward(s.dirs()) {
		dir := v
		memory, path, ok, err := s.findActiveInDir(dir, parsed.raw)
		if err != nil {
			return Memory{}, "", false, err
		}
		if ok {
			return memory, path, true, nil
		}
	}
	return Memory{}, "", false, nil
}

func (s Store) findActiveInDir(dir, ref string) (Memory, string, bool, error) {
	if strings.TrimSpace(dir) == "" {
		return Memory{}, "", false, nil
	}
	entries, err := readMemoryDirectory(dir)
	if err != nil {
		return Memory{}, "", false, err
	}
	parsed := parseMemoryReference(ref)
	wantName := parsed.name
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == indexFile || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		memory, ok, readErr := loadMemory(path)
		if readErr != nil {
			// The stem is an unreadable file's only remaining identity: report
			// it for the name asked for, and never let it block another name.
			if slug(strings.TrimSuffix(entry.Name(), ".md")) == wantName {
				return Memory{}, path, false, readErr
			}
			continue
		}
		if !ok {
			continue
		}
		if memory.Scope == "" {
			memory.Scope = s.scopeForDir(dir)
		}
		if memory.ID == parsed.raw || slug(memory.Name) == wantName {
			memory.Name = slug(memory.Name)
			return memory, path, true, nil
		}
	}
	return Memory{}, "", false, nil
}

type memoryReference struct {
	raw       string
	name      string
	scope     FactScope
	qualified bool
}

// parseMemoryReference understands provider-visible references without ever
// treating them as filesystem paths. Memory facts are flat files, so only one
// fixed scope component plus one filename is accepted as a qualified form.
func parseMemoryReference(ref string) memoryReference {
	raw := strings.TrimSpace(ref)
	parsed := memoryReference{raw: raw, name: slug(strings.TrimSuffix(raw, ".md"))}
	for _, candidate := range []FactScope{FactScopeProject, FactScopeGlobal} {
		prefix := string(candidate) + "/"
		if !strings.HasPrefix(raw, prefix) {
			continue
		}
		name := strings.TrimPrefix(raw, prefix)
		if name == "" || strings.ContainsAny(name, `/\\`) {
			return parsed
		}
		parsed.name = slug(strings.TrimSuffix(name, ".md"))
		parsed.scope = candidate
		parsed.qualified = true
		return parsed
	}
	return parsed
}

func (s Store) Revisions(ref string) ([]Memory, error) {
	active, _, ok, err := s.findActive(ref)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	seen := map[int]bool{}
	var revisions []Memory
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		revisionDir := filepath.Join(dir, ".revisions", active.ID)
		entries, err := readMemoryDirectory(revisionDir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			// A revision that cannot be read makes this history incomplete, and
			// an incomplete history is reported rather than shown as if the
			// missing revision never existed.
			memory, ok, readErr := loadMemory(filepath.Join(revisionDir, entry.Name()))
			if readErr != nil {
				return nil, readErr
			}
			if !ok || memory.ID != active.ID || seen[memory.Revision] {
				continue
			}
			seen[memory.Revision] = true
			revisions = append(revisions, memory)
		}
	}
	sort.Slice(revisions, func(i, j int) bool { return revisions[i].Revision > revisions[j].Revision })
	return revisions, nil
}

func (s Store) findArchivedByPath(want string) (Memory, string, bool, error) {
	for _, base := range s.dirs() {
		if strings.TrimSpace(base) == "" {
			continue
		}
		dir := filepath.Join(base, ".archive")
		info, err := os.Lstat(dir)
		if os.IsNotExist(err) {
			// Windows reports "not found" beneath a regular file as well.
			if parent, statErr := os.Stat(base); statErr == nil && !parent.IsDir() {
				return Memory{}, "", false, &DirectoryReadError{Path: base, Err: err}
			}
			continue
		}
		if err != nil {
			return Memory{}, "", false, &DirectoryReadError{Path: dir, Err: err}
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		entries, err := readMemoryDirectory(dir)
		if err != nil {
			return Memory{}, "", false, err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			path, err := safeJoin(dir, entry.Name())
			if err != nil || cleanMemoryPath(path) != want {
				continue
			}
			info, err := os.Lstat(path)
			if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return Memory{}, "", false, nil
			}
			archived, ok, readErr := loadMemory(path)
			if readErr != nil {
				// This is the very file the caller named, so recovery stops
				// instead of reporting the archive entry as missing.
				return Memory{}, "", false, readErr
			}
			if !ok {
				return Memory{}, "", false, nil
			}
			if archived.Scope == "" {
				archived.Scope = s.scopeForDir(base)
			}
			archived.Name = slug(archived.Name)
			return archived, base, true, nil
		}
	}
	return Memory{}, "", false, nil
}

func (s Store) maxKnownRevision(id string) (int, error) {
	maxRevision := 0
	for _, base := range s.dirs() {
		if strings.TrimSpace(base) == "" {
			continue
		}
		for _, dir := range []string{filepath.Join(base, ".archive"), filepath.Join(base, ".revisions", id)} {
			entries, err := readMemoryDirectory(dir)
			if err != nil {
				return 0, err
			}
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
					continue
				}
				path := filepath.Join(dir, entry.Name())
				info, err := os.Lstat(path)
				if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
					continue
				}
				// History that cannot be read makes the maximum unknown, and a
				// revision computed from incomplete history is exactly what the
				// recovery contract forbids; stop before snapshotting anything.
				candidate, ok, readErr := loadMemory(path)
				if readErr != nil {
					return 0, readErr
				}
				if ok && candidate.ID == id && candidate.Revision > maxRevision {
					maxRevision = candidate.Revision
				}
			}
		}
	}
	return maxRevision, nil
}
