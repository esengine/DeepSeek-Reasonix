package delta

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

// Entry is one file of a release tree as the builder reads it.
type Entry struct {
	Path string
	Data []byte
	Exec bool
}

// Build describes entries as an Index, handing each distinct chunk to store
// once so the caller can put it in the chunk store.
func Build(version, platform string, entries []Entry, store func(hash string, plain []byte) error) (Index, error) {
	sorted := append([]Entry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	x := Index{SchemaVersion: SchemaVersion, Version: version, Platform: platform}
	stored := map[string]bool{}
	for _, e := range sorted {
		sum := sha256.Sum256(e.Data)
		f := File{Path: e.Path, Size: int64(len(e.Data)), SHA256: hex.EncodeToString(sum[:]), Exec: e.Exec, Chunks: []Chunk{}}
		err := SplitReader(bytes.NewReader(e.Data), func(_ Span, b []byte) error {
			h := HashOf(b)
			f.Chunks = append(f.Chunks, Chunk{Hash: h, Size: len(b)})
			if stored[h] {
				return nil
			}
			stored[h] = true
			return store(h, b)
		})
		if err != nil {
			return Index{}, err
		}
		x.Files = append(x.Files, f)
	}
	return x, x.Validate()
}
