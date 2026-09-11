package sidecar

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"

	"reasonix/internal/extension/protocol"
)

// Content-ref externalization for the intercept and event calls. The wire
// contract: a field tagged externalizable whose serialized value exceeds
// protocol.ExternalizeFieldBytes travels as null in its owner document, and
// the owner's externalized envelope carries one descriptor per moved field
// naming the content ref to page back through host/content/read. Only the
// host creates refs (the protocol registers host/content/read in the
// Extension → Host direction alone), so an extension answering with a content
// ref always names an object in this connection's host-side store.

// externalizablePointer resolves the schema-registered externalizable JSON
// pointer for one field of typ, failing loudly on schema drift instead of
// silently externalizing a field the schema no longer marks.
func externalizablePointer(typ reflect.Type, want string) (string, error) {
	for _, pointer := range protocol.ExternalizablePointers(typ) {
		if pointer == want {
			return pointer, nil
		}
	}
	return "", fmt.Errorf("sidecar: %s has no externalizable %s field (schema drift)", typ, want)
}

// externalizePayload moves an over-threshold payload into the connection's
// content store and returns the null placeholder plus the wire envelope.
// Below the protocol.ExternalizeFieldBytes threshold the payload passes
// through inline with a nil envelope, keeping small calls byte-identical.
func externalizePayload(store *Store, pointer string, payload json.RawMessage) (json.RawMessage, []ExternalizedField, error) {
	field, err := MaybeExternalize(store, pointer, payload)
	if err != nil {
		return nil, nil, err
	}
	if field == nil {
		return payload, nil, nil
	}
	return nil, []ExternalizedField{*field}, nil
}

// externalizeInterceptParams applies the outbound content-ref rule to one
// intercept call's payload.
func (c *Client) externalizeInterceptParams(params *protocol.InterceptParams) error {
	pointer, err := externalizablePointer(reflect.TypeOf(*params), "/payload")
	if err != nil {
		return err
	}
	payload, externalized, err := externalizePayload(c.store, pointer, params.Payload)
	if err != nil {
		return err
	}
	params.Payload = payload
	params.Externalized = externalized
	return nil
}

// externalizeEventParams applies the outbound content-ref rule to one event
// notification's payload.
func (c *Client) externalizeEventParams(params *protocol.EventParams) error {
	pointer, err := externalizablePointer(reflect.TypeFor[protocol.EventParams](), "/payload")
	if err != nil {
		return err
	}
	payload, externalized, err := externalizePayload(c.store, pointer, params.Payload)
	if err != nil {
		return err
	}
	params.Payload = payload
	params.Externalized = externalized
	return nil
}

// resolveExternalizedReplacement rehydrates an intercept result whose
// replacement traveled as a content-ref envelope instead of inline JSON. The
// ref is paged out of this connection's store with the exact
// host/content/read chunking rules (Store.Read), the reassembled bytes are
// verified against the descriptor's byte count and SHA-256, and only then
// substituted for the strict decode the dispatcher performs. An inline
// replacement alongside an envelope, a pointer outside the schema's
// externalizable set, or unverifiable content is a protocol error: the
// dispatcher must never decode bytes the peer did not prove.
func (c *Client) resolveExternalizedReplacement(result *protocol.InterceptResult) error {
	if len(result.Externalized) == 0 {
		return nil
	}
	if inline := bytes.TrimSpace(result.Replacement); len(inline) > 0 && !bytes.Equal(inline, []byte("null")) {
		return &protocol.ProtocolError{Reason: protocol.ErrProtocolError, Message: "intercept result carries both an inline replacement and an externalized envelope"}
	}
	pointer, err := externalizablePointer(reflect.TypeOf(*result), "/replacement")
	if err != nil {
		return err
	}
	if len(result.Externalized) != 1 || result.Externalized[0].JSONPointer != pointer {
		return &protocol.ProtocolError{Reason: protocol.ErrProtocolError, Message: fmt.Sprintf(
			"intercept result externalized envelope must hold exactly the %s descriptor", pointer)}
	}
	descriptor := result.Externalized[0]
	if descriptor.TotalBytes > protocol.ContentRefObjectBytes {
		return &protocol.ProtocolError{Reason: protocol.ErrFrameTooLarge, Message: fmt.Sprintf(
			"externalized replacement is %d bytes, above the %d byte object cap", descriptor.TotalBytes, protocol.ContentRefObjectBytes)}
	}
	reassembled, err := c.store.readAll(descriptor.ContentRef)
	if err != nil {
		return err
	}
	if int64(len(reassembled)) != descriptor.TotalBytes {
		return &protocol.ProtocolError{Reason: protocol.ErrProtocolError, Message: fmt.Sprintf(
			"externalized replacement reassembled to %d bytes, want %d", len(reassembled), descriptor.TotalBytes)}
	}
	sum := sha256.Sum256(reassembled)
	if hex.EncodeToString(sum[:]) != descriptor.SHA256 {
		return &protocol.ProtocolError{Reason: protocol.ErrProtocolError, Message: "externalized replacement SHA-256 mismatch"}
	}
	result.Replacement = reassembled
	return nil
}

// readAll pages a whole content ref out of the store in
// protocol.ContentRefChunkBytes chunks, mirroring how an extension pages
// host/content/read.
func (s *Store) readAll(ref string) ([]byte, error) {
	var out []byte
	var offset int64
	for {
		chunk, next, _, _, err := s.Read(ref, offset)
		if err != nil {
			return nil, err
		}
		out = append(out, chunk...)
		if next == nil {
			return out, nil
		}
		offset = *next
	}
}
