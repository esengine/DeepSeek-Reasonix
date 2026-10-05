package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// ExtractReleaseUnit reads a line's release files out of a verified .tar.gz. It
// requires every member the line declares and rejects duplicates: a partial
// extraction would publish a version directory missing a file, which the
// pointer swap would then make current.
func ExtractReleaseUnit(targz []byte, line Line) (map[string][]byte, error) {
	members := line.ArchiveNames()
	if len(members) == 0 {
		return nil, errors.New("update: release unit has no members; the host did not declare its line")
	}
	want := make(map[string]struct{}, len(members))
	for _, name := range members {
		want[name] = struct{}{}
	}
	found := make(map[string][]byte, len(want))
	gz, err := gzip.NewReader(bytes.NewReader(targz))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		name := path.Base(strings.TrimSpace(h.Name))
		if _, ok := want[name]; !ok {
			continue
		}
		if h.Typeflag != tar.TypeReg || h.Size < 0 {
			return nil, fmt.Errorf("update: release member %q is not a regular file", name)
		}
		if _, duplicate := found[name]; duplicate {
			return nil, fmt.Errorf("update: release member %q appears more than once", name)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		found[name] = body
	}
	for _, name := range members {
		if _, ok := found[name]; !ok {
			return nil, fmt.Errorf("update: release member %q not found in archive", name)
		}
	}
	return found, nil
}
