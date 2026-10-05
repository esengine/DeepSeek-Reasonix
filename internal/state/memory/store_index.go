package memory

import (
	"os"
	"path/filepath"
	"strings"

	fileencoding "reasonix/internal/base/fileutil/encoding"
)

// readIndexIn treats a missing index as empty, but does not hide other read errors.
func readIndexIn(dir string) ([]byte, error) {
	existing, err := fileencoding.ReadFileUTF8(filepath.Join(dir, indexFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	return existing, err
}

// indexLinesExceptIn returns the managed MEMORY.md lines keyed by filename stem,
// dropping the entry for name and reporting whether it was present.
func indexLinesExceptIn(dir, name string) (map[string]string, bool, error) {
	existing, err := readIndexIn(dir)
	if err != nil {
		return nil, false, err
	}
	keep := map[string]string{}
	contains := false
	for line := range strings.SplitSeq(string(existing), "\n") {
		if mt := indexLineRe.FindStringSubmatch(line); mt != nil {
			if mt[1] == name {
				contains = true
			} else {
				keep[mt[1]] = strings.TrimRight(line, "\r")
			}
		}
	}
	return keep, contains, nil
}

func removeMemoryFromDir(path, name string) error {
	dir := filepath.Dir(path)
	lines, _, err := indexLinesExceptIn(dir, name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return flushIndexIn(dir, lines)
}
