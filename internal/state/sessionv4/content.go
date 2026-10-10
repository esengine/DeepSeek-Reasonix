package sessionv4

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
)

// contentRef names one immutable object in the content pool by the SHA-256 of
// its bytes.
type contentRef struct {
	Digest    string `json:"digest"`
	Bytes     int64  `json:"bytes"`
	MediaType string `json:"mediaType,omitempty"`
}

// contentPool is the directory 1.x keeps large payloads and images in.
type contentPool struct {
	store fs.FS
	dir   string
}

// poolFor resolves the pool a session names, accepting only the two spellings
// 1.x itself resolves, so a manifest cannot point a read outside the store.
func poolFor(store fs.FS, sessionName, named string) contentPool {
	dir := ".content-v1"
	if named == ".content-v1" {
		dir = path.Join(sessionName, ".content-v1")
	}
	return contentPool{store: store, dir: dir}
}

// read returns an object's bytes after checking its size and digest.
func (p contentPool) read(ref contentRef) ([]byte, error) {
	if ref.Bytes > maxObjectBytes {
		return nil, fmt.Errorf("%w: content %s holds %d bytes", ErrTooLarge, ref.Digest, ref.Bytes)
	}
	if len(ref.Digest) != sha256.Size*2 || ref.Bytes < 0 {
		return nil, fmt.Errorf("%w: invalid content reference %q", ErrDamaged, ref.Digest)
	}
	if _, err := hex.DecodeString(ref.Digest); err != nil {
		return nil, fmt.Errorf("%w: invalid content reference %q", ErrDamaged, ref.Digest)
	}
	f, err := p.store.Open(path.Join(p.dir, "objects", ref.Digest[:2], ref.Digest[2:4], ref.Digest))
	if err != nil {
		return nil, fmt.Errorf("%w: content %s: %w", ErrDamaged, ref.Digest, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, ref.Bytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: content %s: %w", ErrDamaged, ref.Digest, err)
	}
	if int64(len(data)) != ref.Bytes {
		return nil, fmt.Errorf("%w: content %s holds %d bytes, want %d", ErrDamaged, ref.Digest, len(data), ref.Bytes)
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != ref.Digest {
		return nil, fmt.Errorf("%w: content %s does not match its digest", ErrDamaged, ref.Digest)
	}
	return data, nil
}

const maxObjectBytes = 256 << 20

// payload is an event's JSON body, inline or from the pool.
func (p contentPool) payload(ev event) (json.RawMessage, error) {
	if ev.PayloadRef == nil {
		return json.RawMessage(ev.Payload), nil
	}
	return p.read(*ev.PayloadRef)
}
