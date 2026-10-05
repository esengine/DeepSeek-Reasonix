package delta

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
)

// SchemaVersion names the chunking parameters and this layout together.
const SchemaVersion = 1

// ErrInvalidIndex is an Index that does not describe a tree safely: a path
// that climbs out of the install, a hash that is not one, or chunks that do not
// add up to their file.
var ErrInvalidIndex = errors.New("delta: invalid index")

// Index describes one release's install tree for one platform.
type Index struct {
	SchemaVersion int    `json:"schemaVersion"`
	Version       string `json:"version"`
	Platform      string `json:"platform"`
	Files         []File `json:"files"`
}

// File is one file of the tree. Path is slash-separated and relative.
type File struct {
	Path   string  `json:"path"`
	Size   int64   `json:"size"`
	SHA256 string  `json:"sha256"`
	Exec   bool    `json:"exec,omitempty"`
	Chunks []Chunk `json:"chunks"`
}

// Chunk names a piece of a file by the SHA-256 of its plain bytes.
type Chunk struct {
	Hash string `json:"h"`
	Size int    `json:"n"`
}

// Encode is the canonical form the release signs.
func (x Index) Encode() ([]byte, error) {
	if err := x.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(x); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Decode reads an Index and refuses one that is not safe to apply.
func Decode(data []byte) (Index, error) {
	var x Index
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&x); err != nil {
		return Index{}, fmt.Errorf("%w: %w", ErrInvalidIndex, err)
	}
	return x, x.Validate()
}

// Validate checks everything an apply relies on without trusting the writer.
func (x Index) Validate() error {
	if x.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: schema %d, this build reads %d", ErrInvalidIndex, x.SchemaVersion, SchemaVersion)
	}
	if strings.TrimSpace(x.Version) == "" || strings.TrimSpace(x.Platform) == "" {
		return fmt.Errorf("%w: version and platform are required", ErrInvalidIndex)
	}
	seen := make(map[string]bool, len(x.Files))
	for _, f := range x.Files {
		if !safePath(f.Path) {
			return fmt.Errorf("%w: unsafe path %q", ErrInvalidIndex, f.Path)
		}
		key := strings.ToLower(f.Path)
		if seen[key] {
			return fmt.Errorf("%w: %q listed twice", ErrInvalidIndex, f.Path)
		}
		seen[key] = true
		if !isHash(f.SHA256) {
			return fmt.Errorf("%w: %q has no SHA-256", ErrInvalidIndex, f.Path)
		}
		var sum int64
		for _, c := range f.Chunks {
			if !isHash(c.Hash) || c.Size <= 0 || c.Size > maxChunk {
				return fmt.Errorf("%w: %q has a malformed chunk", ErrInvalidIndex, f.Path)
			}
			sum += int64(c.Size)
		}
		if sum != f.Size {
			return fmt.Errorf("%w: %q chunks add up to %d, not %d", ErrInvalidIndex, f.Path, sum, f.Size)
		}
	}
	return nil
}

// safePath is a relative, slash-separated path that stays inside the tree on
// every platform the tree is written to.
func safePath(p string) bool {
	if p == "" || strings.ContainsAny(p, "\\:\x00") || strings.HasPrefix(p, "/") {
		return false
	}
	if path.Clean(p) != p {
		return false
	}
	for part := range strings.SplitSeq(p, "/") {
		if part == ".." || part == "." || part == "" {
			return false
		}
	}
	return true
}

func isHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}
