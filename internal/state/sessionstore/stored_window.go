package sessionstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
	"sort"
)

type StoredMessageWindow struct {
	HistoryPageRead
	DecodedMessages int
	IndexBytesRead  int
}

func addDisplayPageMetadata(index *SessionDisplayIndex, messages []provider.Message) {
	history := newHistoryReadIndex(0)
	history.extend(messages, 0)
	applyDisplayPageMetadata(index, history)
}

func applyDisplayPageMetadata(index *SessionDisplayIndex, history *historyReadIndex) {
	work, _ := history.pageContext(0, len(index.Entries), len(index.Entries))
	index.PageMetadata = 1
	for i := range index.Entries {
		entry := &index.Entries[i]
		entry.WorkMs = work[i]
		entry.CallKnown = false
		if call, ok := history.results[i]; ok {
			entry.CallKnown = true
			entry.CallMessage = call.MessageIndex
			entry.CallOrdinal = call.Ordinal
		}
	}
}

// ReadStoredMessageWindow reads page payloads by offset after validating the derived index.
// Missing or stale read models are repaired once under the ordinary save locks.
func ReadStoredMessageWindow(path string, after, limit int) (StoredMessageWindow, error) {
	unlock := lockSessionSavePath(path)
	defer unlock()
	release, err := lockSessionFile(path)
	if err != nil {
		return StoredMessageWindow{}, err
	}
	defer release()
	page, err := readStoredWindow(path, after, limit)
	if !errors.Is(err, ErrSessionDisplayReadModelDamaged) {
		return page, err
	}
	if err := repairSessionDisplayReadModelUnlocked(path); err != nil {
		return StoredMessageWindow{}, err
	}
	return readStoredWindow(path, after, limit)
}

func readStoredWindow(path string, after, limit int) (StoredMessageWindow, error) {
	index, err := currentWindowIndex(path)
	if err != nil {
		return StoredMessageWindow{}, err
	}
	defer index.file.Close()
	f, err := os.Open(path)
	if err != nil {
		return StoredMessageWindow{}, err
	}
	defer f.Close()
	limit = min(max(limit, 1), 2000)
	if after < 0 || after > index.count {
		after = max(0, index.count-limit)
	}
	end := min(index.count, after+limit)
	page := StoredMessageWindow{HistoryPageRead: HistoryPageRead{Identity: fmt.Sprintf("%d:%s", index.revision, index.digest), Start: after, End: end, Total: index.count, Messages: []provider.Message{}, WorkMs: []int64{}, ToolContext: []HistoryToolContext{}}}
	decoded := map[int]provider.Message{}
	read := func(at int) (provider.Message, error) {
		if m, ok := decoded[at]; ok {
			return m, nil
		}
		entry, err := index.entry(at)
		if err != nil {
			return provider.Message{}, err
		}
		m, err := readDisplayEntry(f, entry)
		if err == nil {
			decoded[at] = m
			page.DecodedMessages++
		}
		return m, err
	}
	seen := map[[2]int]bool{}
	for i := after; i < end; i++ {
		m, err := read(i)
		if err != nil {
			return StoredMessageWindow{}, err
		}
		page.Messages = append(page.Messages, m)
		entry, err := index.entry(i)
		if err != nil {
			return StoredMessageWindow{}, err
		}
		page.WorkMs = append(page.WorkMs, entry.WorkMs)
		if !entry.CallKnown || entry.CallMessage >= after {
			continue
		}
		if entry.CallMessage < 0 || entry.CallMessage >= i {
			return StoredMessageWindow{}, ErrSessionDisplayReadModelDamaged
		}
		parent, err := read(entry.CallMessage)
		if err != nil {
			return StoredMessageWindow{}, err
		}
		if entry.CallOrdinal < 0 || entry.CallOrdinal >= len(parent.ToolCalls) || parent.ToolCalls[entry.CallOrdinal].ID != m.ToolCallID {
			return StoredMessageWindow{}, ErrSessionDisplayReadModelDamaged
		}
		key := [2]int{entry.CallMessage, entry.CallOrdinal}
		if !seen[key] {
			seen[key] = true
			page.ToolContext = append(page.ToolContext, HistoryToolContext{MessageIndex: entry.CallMessage, Ordinal: entry.CallOrdinal, Call: parent.ToolCalls[entry.CallOrdinal]})
		}
	}
	sort.Slice(page.ToolContext, func(i, j int) bool {
		a, b := page.ToolContext[i], page.ToolContext[j]
		if a.MessageIndex == b.MessageIndex {
			return a.Ordinal < b.Ordinal
		}
		return a.MessageIndex < b.MessageIndex
	})
	page.IndexBytesRead = index.readBytes
	return page, nil
}

func readDisplayEntry(f *os.File, entry DisplayIndexEntry) (provider.Message, error) {
	var message provider.Message
	if entry.Length <= 0 || entry.Length > sessionDisplayIndexMaxLineBytes {
		return message, ErrSessionDisplayReadModelDamaged
	}
	data := make([]byte, entry.Length)
	if _, err := f.ReadAt(data, entry.Offset); err != nil && err != io.EOF {
		return message, err
	}
	if data[len(data)-1] != '\n' {
		return message, ErrSessionDisplayReadModelDamaged
	}
	err := json.Unmarshal(data[:len(data)-1], &message)
	return message, err
}

func currentPageIndex(path string) (*SessionDisplayIndex, error) {
	indexPath := store.SessionDisplayIndex(path)
	index, err := LoadSessionDisplayIndex(indexPath)
	if err != nil {
		return nil, err
	}
	if index.PageMetadata != 1 {
		return nil, ErrSessionDisplayReadModelDamaged
	}
	revision, digest, err := sessionContentRevision(path)
	if err != nil {
		return nil, err
	}
	if digest != "" && (!index.RevisionKnown || index.Revision != revision || index.ContentDigest != digest) {
		return nil, ErrHistorySnapshotChanged
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	stamp, err := os.Stat(indexPath)
	if err != nil {
		return nil, err
	}
	if info.Size() != index.TranscriptSize || stamp.ModTime().Before(SessionContentModTime(path)) {
		return nil, ErrHistorySnapshotChanged
	}
	return index, nil
}
