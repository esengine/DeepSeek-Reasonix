package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// ErrSessionDisplayReadModelDamaged means the authoritative event log has a
// torn tail. The normal session save path owns healing it; a derived display
// read model must never publish the replayable prefix as if it were complete.
var ErrSessionDisplayReadModelDamaged = errors.New("session display read model source is damaged")

// LoadSessionDisplayMessages returns the authoritative persisted transcript
// without applying resume-time normalization. Desktop history uses this only
// as the bounded-recovery path while the random-read model is missing or stale.
func LoadSessionDisplayMessages(path string) ([]provider.Message, PersistedState, bool, error) {
	unlock := lockSessionSavePath(path)
	defer unlock()
	return loadSessionDisplayMessagesUnlocked(path)
}

func loadSessionDisplayMessagesUnlocked(path string) ([]provider.Message, PersistedState, bool, error) {
	hasher := newSessionTranscriptHasher()
	msgs, _, damaged, err := loadSessionMessagesWithLimits(path, defaultSessionReplayLimits, hasher)
	if err != nil {
		return nil, PersistedState{}, false, err
	}
	digest, digestOK := hasher.sum()
	if !digestOK {
		digest, err = digestSessionMessages(msgs)
		if err != nil {
			return nil, PersistedState{}, false, err
		}
	}
	revision, ledgerDigest, err := sessionContentRevision(path)
	if err != nil {
		return nil, PersistedState{}, false, err
	}
	state := PersistedState{
		Digest:    digest,
		DigestHex: digestString(digest),
	}
	if ledgerDigest != "" && ledgerDigest == state.DigestHex {
		state.Revision = revision
		state.RevisionKnown = true
	}
	return msgs, state, !damaged, nil
}

// RepairSessionDisplayReadModel atomically refreshes the compatibility JSONL
// read model and publishes its display index from the authoritative event log.
// It shares both save locks with Session.save, so a background migration can
// never interleave between an event append and its revision/index publication.
func RepairSessionDisplayReadModel(path string) error {
	if path == "" {
		return fmt.Errorf("empty session path")
	}
	unlock := lockSessionSavePath(path)
	defer unlock()
	unlockFile, err := lockSessionFile(path)
	if err != nil {
		return fmt.Errorf("lock session file: %w", err)
	}
	defer unlockFile()

	msgs, state, repairable, err := loadSessionDisplayMessagesUnlocked(path)
	if err != nil {
		return err
	}
	if !repairable {
		return ErrSessionDisplayReadModelDamaged
	}
	if err := writeSessionMessages(path, msgs); err != nil {
		return fmt.Errorf("write session display read model: %w", err)
	}
	idx := BuildSessionDisplayIndex(msgs, state.Revision, state.RevisionKnown, state.Digest)
	if idx == nil {
		return fmt.Errorf("encode session display index")
	}
	if err := WriteSessionDisplayIndex(store.SessionDisplayIndex(path), idx); err != nil {
		return err
	}
	return nil
}

// appendSessionDisplayReadModel advances the JSONL random-read model in place
// when the previous display index proves it is exactly the authoritative
// prefix. false,nil asks the caller to leave the old model/index untouched and
// let background repair rebuild them; it never guesses from file size alone.
func appendSessionDisplayReadModel(path string, msgs []provider.Message, appendFrom int, baseRevision int64) (bool, error) {
	if appendFrom <= 0 || appendFrom > len(msgs) {
		return false, nil
	}
	indexPath := store.SessionDisplayIndex(path)
	idx, err := LoadSessionDisplayIndex(indexPath)
	if err != nil || idx.MessageCount != appendFrom || !idx.RevisionKnown || idx.Revision != baseRevision {
		return false, nil
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || idx.TranscriptSize != info.Size() {
		return false, nil
	}
	indexInfo, err := os.Stat(indexPath)
	if err != nil || indexInfo.IsDir() || indexInfo.ModTime().Before(info.ModTime()) {
		return false, nil
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return false, err
	}
	originalSize := info.Size()
	rollback := func(cause error) (bool, error) {
		closeErr := f.Close()
		truncateErr := os.Truncate(path, originalSize)
		return false, errors.Join(cause, closeErr, truncateErr)
	}
	enc := json.NewEncoder(f)
	for i := appendFrom; i < len(msgs); i++ {
		if err := enc.Encode(msgs[i]); err != nil {
			return rollback(fmt.Errorf("encode session display message %d: %w", i, err))
		}
	}
	if err := f.Sync(); err != nil {
		return rollback(fmt.Errorf("sync session display read model: %w", err))
	}
	if err := f.Close(); err != nil {
		if truncateErr := os.Truncate(path, originalSize); truncateErr != nil {
			return false, errors.Join(err, truncateErr)
		}
		return false, err
	}
	// Some filesystems expose coarse mtimes. Ensure the subsequently-published
	// index cannot appear older than this append generation.
	_ = os.Chtimes(path, time.Now(), time.Now())
	return true, nil
}
