package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

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
