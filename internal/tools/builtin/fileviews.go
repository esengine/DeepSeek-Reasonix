package builtin

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"reasonix/internal/contract/tool"
)

// ErrFileChangedSinceSeen marks a write_file refused because the file no longer
// holds what the agent last read or wrote there.
var ErrFileChangedSinceSeen = errors.New("file changed since the agent last saw it")

// FileViews is what one run's agent last saw of each file: the content a read
// returned it from, or what one of its own writes left there. write_file checks
// it so a whole-file overwrite cannot discard a change made since. A nil
// *FileViews records and checks nothing.
type FileViews struct {
	mu   sync.Mutex
	seen map[string][sha256.Size]byte
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

// sawFile records the file's current content, as the edit tools would read it.
func (v *FileViews) sawFile(ctx context.Context, overlay FileOverlay, path string) {
	if v == nil {
		return
	}
	if info, err := os.Stat(path); err == nil && info.Size() <= maxViewBytes {
		if src, err := readEditSource(ctx, overlay, path); err == nil {
			v.saw(path, src.content)
			return
		}
	}
	v.mu.Lock()
	delete(v.seen, filepath.Clean(path))
	v.mu.Unlock()
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
