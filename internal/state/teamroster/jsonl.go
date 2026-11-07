package teamroster

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// readJSONL decodes every complete line of path into fn. A last line with no
// newline is a write a crash cut short and is skipped; any other line that does
// not decode is corruption.
func readJSONL(path string, fn func([]byte) error) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if i := bytes.LastIndexByte(data, '\n'); i < len(data)-1 {
		data = data[:i+1]
	}
	for n, line := range bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if err := fn(line); err != nil {
			return fmt.Errorf("%w: %s line %d: %w", ErrCorrupt, path, n+1, err)
		}
	}
	return nil
}

// appendJSONL writes v as one line at the end of path, first cutting away a
// line a crash left without its newline so the new one never joins it.
func appendJSONL(path string, v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := cutTornTail(f); err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

func cutTornTail(f *os.File) error {
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return err
	}
	last := []byte{0}
	if _, err := f.ReadAt(last, info.Size()-1); err != nil || last[0] == '\n' {
		return err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	return f.Truncate(int64(bytes.LastIndexByte(data, '\n') + 1))
}
