package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	restoreStagingPrefix = ".restoring-"
	restoreStampLayout   = "20060102-150405.000"
)

// restoreStagingName is the hidden name an in-flight restore moves its archive
// entry to. It has no .md suffix, so no reader, index, list, or migration ever
// treats it as a fact or an archive entry.
func restoreStagingName(id string, when time.Time) string {
	return restoreStagingPrefix + id + "-" + when.UTC().Format(restoreStampLayout)
}

// parseRestoreStagingName recovers the id and archive time a staging name
// carries. The stamp has a fixed width, so the id's own dashes are unambiguous.
func parseRestoreStagingName(name string) (string, time.Time, bool) {
	if !strings.HasPrefix(name, restoreStagingPrefix) || strings.HasSuffix(name, ".md") {
		return "", time.Time{}, false
	}
	rest := strings.TrimPrefix(name, restoreStagingPrefix)
	cut := len(rest) - len(restoreStampLayout) - 1
	if cut <= 0 || rest[cut] != '-' {
		return "", time.Time{}, false
	}
	when, err := time.ParseInLocation(restoreStampLayout, rest[cut+1:], time.UTC)
	if err != nil {
		return "", time.Time{}, false
	}
	return rest[:cut], when, true
}

// sweepRestoreStaging reconciles hidden copies an interrupted restore left in
// .archive. Every restore runs it first, so a retry decides the fate of a dead
// attempt before it stages anything of its own.
func (s Store) sweepRestoreStaging() error {
	for _, base := range s.dirs() {
		if strings.TrimSpace(base) == "" {
			continue
		}
		entries, err := readMemoryDirectory(filepath.Join(base, ".archive"))
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			id, when, ok := parseRestoreStagingName(entry.Name())
			if !ok {
				continue
			}
			if err := s.settleRestoreStaging(base, entry.Name(), id, when); err != nil {
				return err
			}
		}
	}
	return nil
}

// settleRestoreStaging resolves one hidden copy by whether its id is active:
// an active id means the dead restore had published, so only the redundant
// copy goes; an absent id means it never published, so the entry goes back.
func (s Store) settleRestoreStaging(base, staging, id string, when time.Time) error {
	_, _, active, err := s.findActive(id)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer root.Close()
	held := filepath.Join(".archive", staging)
	if active {
		if err := root.Remove(held); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	archived, ok, readErr := loadMemory(filepath.Join(base, ".archive", staging))
	// A staging copy that cannot be read is the same state as one that cannot
	// be parsed: the restore cannot move forward, and nothing is published.
	if readErr != nil || !ok {
		return fmt.Errorf("interrupted restore left an unreadable archive copy at %q", staging)
	}
	dest, err := archivePath(root, slug(archived.Name), when)
	if err != nil {
		return err
	}
	return renameMemoryFile(root, held, dest)
}

// stageArchivedEntry moves the archive entry to its hidden staging name before
// the active file is published. A failed rename returns the error with the
// archive entry untouched and nothing published.
func stageArchivedEntry(base, archivePath, id string) (string, error) {
	if id == "" || id != filepath.Base(id) || strings.ContainsAny(id, `/\`) {
		return "", fmt.Errorf("archived memory has no id that can be staged")
	}
	when := archiveTimeFromName(filepath.Base(archivePath))
	if when.IsZero() {
		when = time.Now().UTC()
	}
	staging := restoreStagingName(id, when)
	root, err := os.OpenRoot(base)
	if err != nil {
		return "", err
	}
	defer root.Close()
	held := filepath.Join(".archive", staging)
	if err := renameMemoryFile(root, filepath.Join(".archive", filepath.Base(archivePath)), held); err != nil {
		return "", err
	}
	return filepath.Join(base, held), nil
}

// putBackStagedArchive returns a staged entry to the exact archive name it was
// taken from, so a restore that fails before the commit changes nothing.
func putBackStagedArchive(base, stagingPath, archivePath string) error {
	root, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer root.Close()
	return renameMemoryFile(root,
		filepath.Join(".archive", filepath.Base(stagingPath)),
		filepath.Join(".archive", filepath.Base(archivePath)))
}

// publishRestored publishes the active fact, reindexes it, then drops the
// staging copy. Any failure withdraws the active fact and puts the archive
// entry back, so a refused restore leaves the store as it found it.
func publishRestored(base, archivePath, path, stagingPath string, m Memory) error {
	if err := writeMemoryCreate(path, []byte(render(m, m.Name)), 0o644); err != nil {
		if back := putBackStagedArchive(base, stagingPath, archivePath); back != nil {
			return fmt.Errorf("%w (and the archived copy could not be put back: %w)", err, back)
		}
		if os.IsExist(err) {
			return fmt.Errorf("memory name %q is already active", m.Name)
		}
		return err
	}
	undo := func(cause error) error {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("%w (and the restored %q could not be withdrawn: %w)", cause, m.Name, err)
		}
		if err := putBackStagedArchive(base, stagingPath, archivePath); err != nil {
			return fmt.Errorf("%w (and the archived copy could not be put back: %w)", cause, err)
		}
		lines, _, err := indexLinesExceptIn(base, m.Name)
		if err != nil {
			return fmt.Errorf("%w (and the index could not be cleaned: %w)", cause, err)
		}
		if err := flushIndexIn(base, lines); err != nil {
			return fmt.Errorf("%w (and the index could not be cleaned: %w)", cause, err)
		}
		return cause
	}
	if err := reindexIn(base, m.Name, m); err != nil {
		return undo(err)
	}
	if err := fault("remove", stagingPath); err != nil {
		return undo(err)
	}
	if err := os.Remove(stagingPath); err != nil && !os.IsNotExist(err) {
		return undo(err)
	}
	return nil
}
