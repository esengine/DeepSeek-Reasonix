package sidecar

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"reasonix/internal/extension/protocol"
)

// storeMaxEntries bounds the per-client content store; the oldest object is
// evicted past the cap so a chatty sidecar cannot grow host memory without
// limit. Mirrored from the Remote workbench content store.
const storeMaxEntries = 64

// ExternalizedField is the host-side mirror of the schema's externalized
// field metadata: when an externalizable payload exceeds
// protocol.ExternalizeFieldBytes it travels as a content ref, and this
// descriptor tells the extension where to page the real bytes back from.
type ExternalizedField = protocol.ExternalizedField

type contentObject struct {
	data   []byte
	sha256 string
	order  uint64
}

// Store holds externalized content for exactly one sidecar connection. It is
// session-scoped in memory only: refs expire with the connection, which is
// why a read of an unknown ref answers content_ref_expired.
type Store struct {
	mu        sync.Mutex
	contents  map[string]contentObject
	nextOrder uint64
}

// NewStore returns an empty content store.
func NewStore() *Store {
	return &Store{contents: make(map[string]contentObject)}
}

// Put stores data and returns its ref, SHA-256 hex digest, and byte count.
// Objects larger than protocol.ContentRefObjectBytes are rejected with the
// frozen frame_too_large error; past storeMaxEntries the oldest object is
// evicted.
func (s *Store) Put(data []byte) (ref string, digest string, totalBytes int64, err error) {
	if len(data) > protocol.ContentRefObjectBytes {
		return "", "", 0, protocol.MustProtocolError(protocol.ErrFrameTooLarge)
	}
	sum := sha256.Sum256(data)
	digest = hex.EncodeToString(sum[:])
	ref = "content_" + randomHex(12)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextOrder++
	s.contents[ref] = contentObject{data: append([]byte(nil), data...), sha256: digest, order: s.nextOrder}
	if len(s.contents) > storeMaxEntries {
		var oldestRef string
		var oldestOrder uint64
		for candidate, object := range s.contents {
			if candidate == ref {
				continue
			}
			if oldestRef == "" || object.order < oldestOrder {
				oldestRef, oldestOrder = candidate, object.order
			}
		}
		delete(s.contents, oldestRef)
	}
	return ref, digest, int64(len(data)), nil
}

// Read pages one chunk of at most protocol.ContentRefChunkBytes from ref at
// offset, returning the chunk, the next offset (nil at end of object), the
// total byte count, and the object's SHA-256. Unknown refs and out-of-range
// offsets answer the frozen content_ref_expired error. This is the in-process
// form of ReadHandler: the host uses it to resolve content refs a peer hands
// back (e.g. an externalized intercept replacement) with the exact
// host/content/read chunking rules.
func (s *Store) Read(ref string, offset int64) (chunk []byte, next *int64, totalBytes int64, digest string, err error) {
	s.mu.Lock()
	object, ok := s.contents[ref]
	s.mu.Unlock()
	if !ok {
		return nil, nil, 0, "", protocol.MustProtocolError(protocol.ErrContentRefExpired)
	}
	if offset < 0 || offset > int64(len(object.data)) {
		return nil, nil, 0, "", protocol.MustProtocolError(protocol.ErrContentRefExpired)
	}
	end := min(offset+protocol.ContentRefChunkBytes, int64(len(object.data)))
	if end < int64(len(object.data)) {
		value := end
		next = &value
	}
	return object.data[offset:end], next, int64(len(object.data)), object.sha256, nil
}

// ReadHandler answers host/content/read: one page of at most
// protocol.ContentRefChunkBytes starting at the exact requested offset. The
// final chunk omits NextOffset. Unknown refs and out-of-range offsets answer
// the frozen content_ref_expired error, mirroring the Remote workbench.
func (s *Store) ReadHandler(_ context.Context, raw json.RawMessage) (any, error) {
	decoded, err := protocol.DecodeExtensionRequestParams(protocol.MethodHostContentRead, raw)
	if err != nil {
		return nil, protocol.MustProtocolError(protocol.ErrInvalidParams).RPCError()
	}
	p := decoded.(protocol.ContentReadParams)
	chunk, next, totalBytes, digest, err := s.Read(p.ContentRef, p.Offset)
	if err != nil {
		var protocolErr *protocol.ProtocolError
		if errors.As(err, &protocolErr) {
			return nil, protocolErr.RPCError()
		}
		return nil, err
	}
	return protocol.ContentReadResult{
		ContentRef: p.ContentRef, Offset: p.Offset,
		DataBase64: base64.StdEncoding.EncodeToString(chunk), NextOffset: next,
		TotalBytes: totalBytes, SHA256: digest, Encoding: protocol.ContentUTF8,
	}, nil
}

// MaybeExternalize stores value and returns its descriptor when it exceeds
// the frozen externalization threshold; smaller values pass through as a nil
// descriptor, meaning the caller keeps them inline. field is the RFC 6901
// JSON pointer of the externalizable field inside its owner document.
func MaybeExternalize(store *Store, field string, value []byte) (*ExternalizedField, error) {
	if !protocol.RequiresExternalization(len(value)) {
		return nil, nil
	}
	if store == nil {
		return nil, fmt.Errorf("sidecar: externalizing %s requires a content store", field)
	}
	ref, digest, totalBytes, err := store.Put(value)
	if err != nil {
		return nil, err
	}
	return &ExternalizedField{
		JSONPointer: field, ContentRef: ref, TotalBytes: totalBytes, SHA256: digest,
	}, nil
}

// randomHex returns n random bytes hex-encoded (2n chars) for content refs.
func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("sidecar: crypto/rand unavailable: %v", err))
	}
	return hex.EncodeToString(buf)
}
