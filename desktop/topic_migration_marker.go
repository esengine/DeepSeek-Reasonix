package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/store"
)

// topicMigrationMarker records a completed legacy→topic migration for the
// directory's current session signature. New CLI sessions and same-size
// rewrites change the signature and force re-migration without relying only on
// coarse directory mtimes.
// v2 also re-evaluates recovery-named sessions that v1 skipped by filename.
const topicMigrationMarker = ".topics-migrated-v2"
const topicIndexRepairMarker = ".topic-indexes-repaired-v2"

const (
	migrationFingerprintWindow    = int64(2 << 10)
	migrationFullFingerprintLimit = int64(256 << 10)
)

func invalidateTopicDirMarkers(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	var errs []error
	for _, marker := range []string{topicMigrationMarker, topicIndexRepairMarker} {
		if err := os.Remove(filepath.Join(dir, marker)); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func topicDirMarkerDone(dir, marker string) bool {
	dir = strings.TrimSpace(dir)
	marker = strings.TrimSpace(marker)
	if dir == "" || marker == "" {
		return false
	}
	data, err := os.ReadFile(filepath.Join(dir, marker))
	if err != nil {
		return false
	}
	sig, err := sessionDirMigrationSignature(dir)
	if err != nil {
		// Transient directory read failure: treat as not done so the next
		// reconcile retries rather than permanently skipping migration.
		return false
	}
	// Accept both signature content and legacy empty markers that still match
	// only when the directory has no session files (empty sig of empty dir).
	got := strings.TrimSpace(string(data))
	if got == "" {
		// Legacy empty marker: valid only when the dir currently has no
		// migratable session/meta files. Any new transcript must re-run.
		return sig == emptySessionDirSignature
	}
	return got == sig
}

const emptySessionDirSignature = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func sessionDirMigrationSignature(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !migrationSignatureArtifact(name) {
			continue
		}
		record, err := migrationArtifactSignature(filepath.Join(dir, name), name)
		if err != nil {
			return "", err
		}
		lines = append(lines, record)
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:]), nil
}

func migrationSignatureArtifact(name string) bool {
	return store.IsSessionTranscriptName(name) ||
		strings.HasSuffix(name, ".events.jsonl") ||
		strings.HasSuffix(name, ".jsonl.meta")
}

// migrationArtifactSignature hashes bounded transcript windows so a large
// history is never fully read. Metadata gets a full digest under the size cap;
// mtime, size, prefix, and tail cover ordinary and restored-time rewrites.
func migrationArtifactSignature(path, name string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	before, err := f.Stat()
	if err != nil {
		return "", err
	}
	if before.IsDir() {
		return "", fmt.Errorf("migration signature artifact %q is a directory", path)
	}
	digest, err := migrationArtifactContentDigest(f, before.Size(), strings.HasSuffix(name, ".jsonl.meta"))
	if err != nil {
		return "", err
	}
	after, err := f.Stat()
	if err != nil {
		return "", err
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return "", fmt.Errorf("migration signature artifact changed while reading: %q", path)
	}
	return fmt.Sprintf("%q\t%d\t%d\t%s", name, before.Size(), before.ModTime().UnixNano(), digest), nil
}

func migrationArtifactContentDigest(f *os.File, size int64, preferFull bool) (string, error) {
	h := sha256.New()
	if size <= migrationFingerprintWindow*2 || (preferFull && size <= migrationFullFingerprintLimit) {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return "", err
		}
		if _, err := io.CopyN(h, f, size); err != nil {
			return "", err
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	for _, sample := range []struct {
		offset int64
		length int64
	}{
		{offset: 0, length: migrationFingerprintWindow},
		{offset: size - migrationFingerprintWindow, length: migrationFingerprintWindow},
	} {
		if _, err := fmt.Fprintf(h, "@%d:%d\n", sample.offset, sample.length); err != nil {
			return "", err
		}
		if _, err := f.Seek(sample.offset, io.SeekStart); err != nil {
			return "", err
		}
		if _, err := io.CopyN(h, f, sample.length); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func topicMigrationDone(dir string) bool {
	return topicDirMarkerDone(dir, topicMigrationMarker)
}

func topicIndexRepairDone(dir string) bool {
	return topicDirMarkerDone(dir, topicIndexRepairMarker)
}

func markTopicDirMarkerDone(dir, marker string) {
	dir = strings.TrimSpace(dir)
	marker = strings.TrimSpace(marker)
	if dir == "" || marker == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	sig, err := sessionDirMigrationSignature(dir)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, marker), []byte(sig+"\n"), 0o644)
}

func markTopicMigrationDone(dir string) {
	markTopicDirMarkerDone(dir, topicMigrationMarker)
}

func markTopicIndexRepairDone(dir string) {
	markTopicDirMarkerDone(dir, topicIndexRepairMarker)
}
