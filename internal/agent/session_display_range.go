package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// LoadSessionPreviewFromDisplayIndex reads only the first authored user-message
// range from a current display index. It never acquires the session save lock or
// scans the full transcript, so runtime tree snapshots cannot wait behind a
// long-running save. A missing or stale index fails closed and lets the caller
// use another preview source.
func LoadSessionPreviewFromDisplayIndex(path string) (string, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false, fmt.Errorf("empty session path")
	}
	index, err := LoadSessionDisplayIndex(store.SessionDisplayIndex(path))
	if err != nil {
		return "", false, err
	}
	messageIndex := -1
	for _, entry := range index.Entries {
		if entry.StartsTurn {
			messageIndex = entry.Index
			break
		}
	}
	if messageIndex < 0 {
		return "", false, nil
	}
	messages, checkedIndex, err := LoadSessionDisplayMessageRange(path, messageIndex, messageIndex+1)
	if err != nil {
		return "", false, err
	}
	if checkedIndex == nil || messageIndex >= len(checkedIndex.Entries) || !checkedIndex.Entries[messageIndex].StartsTurn {
		return "", false, fmt.Errorf("session display index changed while reading preview")
	}
	if err := validateSessionPreviewDisplayIndex(path, checkedIndex); err != nil {
		return "", false, err
	}
	preview, turns := SessionPreviewFromMessages(messages)
	if turns == 0 || strings.TrimSpace(preview) == "" {
		return "", false, nil
	}
	return preview, true, nil
}

func validateSessionPreviewDisplayIndex(path string, index *SessionDisplayIndex) error {
	transcriptInfo, err := os.Stat(path)
	if err != nil {
		return err
	}
	indexInfo, err := os.Stat(store.SessionDisplayIndex(path))
	if err != nil {
		return err
	}
	// The index is published after the content it describes. Equality is
	// ambiguous on coarse filesystems, so this latency-sensitive fallback
	// declines to run a full digest scan to resolve the tie.
	if !indexInfo.ModTime().After(SessionContentModTime(path)) {
		return fmt.Errorf("session display index is not newer than session content")
	}
	identity, identityKnown, err := SessionContentIdentity(path)
	if err != nil {
		return err
	}
	if identityKnown {
		if !ValidateSessionDisplayIndex(index, identity.Revision, identity.RevisionKnown, identity.Digest, transcriptInfo.Size()) {
			return fmt.Errorf("session display index does not match content identity")
		}
	} else if index.RevisionKnown {
		return fmt.Errorf("session display index has no matching content identity")
	}
	return nil
}

// LoadSessionDisplayMessageRange decodes a bounded range through the display
// index. Callers must separately compare the index revision and digest with
// the authoritative content ledger before trusting it.
func LoadSessionDisplayMessageRange(path string, start, end int) ([]provider.Message, *SessionDisplayIndex, error) {
	index, err := LoadSessionDisplayIndex(store.SessionDisplayIndex(path))
	if err != nil {
		return nil, nil, err
	}
	if start < 0 || end < start || end > index.MessageCount {
		return nil, index, fmt.Errorf("invalid display message range [%d,%d)", start, end)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, index, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, index, err
	}
	if info.Size() != index.TranscriptSize {
		return nil, index, fmt.Errorf("display index transcript size changed")
	}
	out := make([]provider.Message, 0, end-start)
	for _, entry := range index.Entries[start:end] {
		line := make([]byte, entry.Length)
		if _, err := f.ReadAt(line, entry.Offset); err != nil && err != io.EOF {
			return nil, index, err
		}
		if len(line) == 0 || line[len(line)-1] != '\n' {
			return nil, index, fmt.Errorf("display message %d is not newline terminated", entry.Index)
		}
		var message provider.Message
		if err := json.Unmarshal(line[:len(line)-1], &message); err != nil {
			return nil, index, fmt.Errorf("decode display message %d: %w", entry.Index, err)
		}
		out = append(out, message)
	}
	return out, index, nil
}
