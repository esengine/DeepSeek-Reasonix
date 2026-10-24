package builtin

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"reasonix/internal/contract/tool"
)

// ErrFileChangedSinceSeen marks a write_file refused because the file no longer
// holds what the agent last read or wrote there.
var ErrFileChangedSinceSeen = errors.New("file changed since the agent last saw it")

// ErrFileNotFullySeen marks a strict overwrite of a file no whole read certified.
var ErrFileNotFullySeen = fmt.Errorf("%w: not read in full", ErrFileChangedSinceSeen)

// FileViews is what one run's agent last saw of each file: the content a read
// returned it from, or what one of its own writes left there. write_file checks
// it so a whole-file overwrite cannot discard a change made since. A nil
// *FileViews records and checks nothing.
type FileViews struct {
	mu     sync.Mutex
	seen   map[string][sha256.Size]byte
	strict bool
}

// NewFileViews returns an empty record.
func NewFileViews() *FileViews {
	return &FileViews{seen: map[string][sha256.Size]byte{}}
}

func (v *FileViews) saw(path, content string) {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.seen[filepath.Clean(path)] = sha256.Sum256([]byte(content))
}

// maxViewBytes bounds the files a read records: past it the view is dropped
// rather than paid for with a second whole-file read.
const maxViewBytes = 8 << 20

func (v *FileViews) captureRead(path string, emit func(io.Reader, int, int) (string, error)) func(io.Reader, int, int) (string, error) {
	if v == nil {
		return emit
	}
	return func(src io.Reader, offset, limit int) (string, error) {
		data, err := io.ReadAll(io.LimitReader(src, maxViewBytes+1))
		if err != nil {
			return "", err
		}
		out, err := emit(io.MultiReader(bytes.NewReader(data), src), offset, limit)
		if err == nil && len(data) <= maxViewBytes {
			lines := bytes.Count(data, []byte{'\n'})
			if len(data) > 0 && data[len(data)-1] != '\n' {
				lines++
			}
			if offset == 0 && (limit <= 0 || lines <= limit) {
				v.saw(path, string(data))
			}
		} else {
			v.mu.Lock()
			delete(v.seen, filepath.Clean(path))
			v.mu.Unlock()
		}
		return out, err
	}
}

// checkOverwrite refuses replacing current when the agent saw different
// content there. A file it has never seen is not refused: nothing it holds
// can be stale.
func (v *FileViews) checkOverwrite(path, current string) error {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	want, ok := v.seen[filepath.Clean(path)]
	v.mu.Unlock()
	if !ok && v.strict {
		lines := strings.Count(current, "\n")
		if current != "" && !strings.HasSuffix(current, "\n") {
			lines++
		}
		return fmt.Errorf("%w: write_file replaces all %d lines of %s; read it whole first (read_file with offset 0 and limit >= %d), or change it with edit_file", ErrFileNotFullySeen, lines, path, lines)
	}
	if !ok || want == sha256.Sum256([]byte(current)) {
		return nil
	}
	return fmt.Errorf("%w: %s was modified after you last read or wrote it, by the user or another process; read it again and apply your change to what it holds now", ErrFileChangedSinceSeen, path)
}

// BindFileViews returns t sharing views when t reads or writes whole files;
// other tools are returned unchanged.
func BindFileViews(t tool.Tool, views *FileViews) tool.Tool {
	switch x := t.(type) {
	case readFile:
		x.views = views
		return x
	case writeFile:
		x.views = views
		return x
	case editFile:
		x.views = views
		return x
	case multiEdit:
		x.views = views
		return x
	case deleteRange:
		x.views = views
		return x
	case deleteSymbol:
		x.views = views
		return x
	}
	return t
}
