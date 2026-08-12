package taskmonitor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

type EventTail struct {
	Items      []TaskEvent
	NextOffset int64
	Reset      bool
}

// ReadEventTail reads only complete JSONL lines after a catalog byte
// checkpoint. It preserves FileStore's identifier and symlink defenses.
func (s *FileStore) ReadEventTail(ctx context.Context, projectDir, taskID string, offset int64) (EventTail, error) {
	out := EventTail{Items: []TaskEvent{}, NextOffset: offset}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	id, err := safeID(taskID)
	if err != nil {
		return out, err
	}
	root, err := s.taskRoot(projectDir)
	if err != nil {
		return out, err
	}
	path := filepath.Join(root, id, "events.jsonl")
	if err := rejectSymlinkChain(root, path); err != nil {
		return out, err
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return out, err
	}
	if offset < 0 || offset > info.Size() {
		offset, out.NextOffset, out.Reset = 0, 0, true
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return out, err
	}
	reader := bufio.NewReader(f)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			out.NextOffset += int64(len(line))
			line = line[:len(line)-1]
			var event TaskEvent
			if json.Unmarshal(line, &event) == nil {
				out.Items = append(out.Items, event)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return out, readErr
		}
	}
	return out, nil
}
