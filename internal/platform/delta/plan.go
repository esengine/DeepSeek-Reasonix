package delta

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Source is where an install already holds a chunk: a file and an offset.
type Source struct {
	Path string // relative to the install root, slash-separated
	Off  int64
}

// Plan is what an Index needs from the network given what an install holds.
type Plan struct {
	Local        map[string]Source // chunk hash → where the install has it
	Missing      []Chunk           // each chunk the install lacks, once
	MissingBytes int64             // plain bytes of Missing
	ReuseBytes   int64             // plain bytes of the new tree found locally
}

// PlanFrom reads the install at root with the same chunker the release used
// and works out which of x's chunks it already has. A file the Index lists
// unchanged is taken by its hash without being cut again; every other file is
// cut, so a chunk that moved between files or names is found too.
func PlanFrom(x Index, root string) (Plan, error) {
	p := Plan{Local: map[string]Source{}}
	wanted := map[string]File{}
	for _, f := range x.Files {
		wanted[f.Path] = f
	}
	err := filepath.WalkDir(root, func(abs string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if f, ok := wanted[rel]; ok && sameFile(abs, f) {
			var off int64
			for _, c := range f.Chunks {
				if _, have := p.Local[c.Hash]; !have {
					p.Local[c.Hash] = Source{Path: rel, Off: off}
				}
				off += int64(c.Size)
			}
			return nil
		}
		return indexFile(abs, rel, p.Local)
	})
	if err != nil {
		return Plan{}, err
	}
	queued := map[string]bool{}
	for _, f := range x.Files {
		for _, c := range f.Chunks {
			if _, have := p.Local[c.Hash]; have {
				p.ReuseBytes += int64(c.Size)
				continue
			}
			if !queued[c.Hash] {
				queued[c.Hash] = true
				p.Missing = append(p.Missing, c)
				p.MissingBytes += int64(c.Size)
			}
		}
	}
	return p, nil
}

// sameFile reports whether the local file at abs is byte-for-byte f.
func sameFile(abs string, f File) bool {
	st, err := os.Stat(abs)
	if err != nil || st.Size() != f.Size {
		return false
	}
	fh, err := os.Open(abs)
	if err != nil {
		return false
	}
	defer fh.Close()
	h := sha256.New()
	if _, err := io.Copy(h, fh); err != nil {
		return false
	}
	return hex.EncodeToString(h.Sum(nil)) == f.SHA256
}

func indexFile(abs, rel string, into map[string]Source) error {
	fh, err := os.Open(abs)
	if err != nil {
		return nil // an unreadable local file only means its chunks are fetched
	}
	defer fh.Close()
	return SplitReader(fh, func(s Span, b []byte) error {
		h := HashOf(b)
		if _, have := into[h]; !have {
			into[h] = Source{Path: rel, Off: s.Off}
		}
		return nil
	})
}
