package sidecar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/extension/protocol"
)

// readAll pages a content ref through ReadHandler the way an extension would
// and returns the reassembled bytes.
func readAll(t *testing.T, store *Store, ref string) []byte {
	t.Helper()
	var out []byte
	var offset int64
	for {
		params, _ := json.Marshal(protocol.ContentReadParams{ContentRef: ref, Offset: offset})
		raw, err := store.ReadHandler(context.Background(), params)
		if err != nil {
			t.Fatalf("ReadHandler at offset %d: %v", offset, err)
		}
		result := raw.(protocol.ContentReadResult)
		chunk, err := base64.StdEncoding.DecodeString(result.DataBase64)
		if err != nil {
			t.Fatalf("chunk base64: %v", err)
		}
		out = append(out, chunk...)
		if result.NextOffset == nil {
			return out
		}
		offset = *result.NextOffset
	}
}

func TestMaybeExternalizePassthroughBelowThreshold(t *testing.T) {
	store := NewStore()
	field, err := MaybeExternalize(store, "/payload", bytes.Repeat([]byte("a"), protocol.ExternalizeFieldBytes))
	if err != nil {
		t.Fatalf("MaybeExternalize: %v", err)
	}
	if field != nil {
		t.Fatalf("%d bytes was externalized, want inline passthrough", protocol.ExternalizeFieldBytes)
	}
}

func TestExternalizeAndChunkedReadRoundTrip(t *testing.T) {
	store := NewStore()
	// 700 KiB crosses the threshold and needs three 256 KiB pages.
	payload := make([]byte, 700<<10)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	field, err := MaybeExternalize(store, "/payload", payload)
	if err != nil {
		t.Fatalf("MaybeExternalize: %v", err)
	}
	if field == nil {
		t.Fatal("700 KiB payload passed through inline")
	}
	sum := sha256.Sum256(payload)
	if field.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("descriptor SHA-256 mismatch")
	}
	if field.TotalBytes != int64(len(payload)) || field.JSONPointer != "/payload" {
		t.Fatalf("descriptor = %+v", field)
	}
	if got := readAll(t, store, field.ContentRef); !bytes.Equal(got, payload) {
		t.Fatalf("round trip mismatch: got %d bytes", len(got))
	}
}

func TestReadHandlerChunkBoundaries(t *testing.T) {
	store := NewStore()
	payload := bytes.Repeat([]byte("x"), protocol.ContentRefChunkBytes+1)
	ref, _, _, err := store.Put(payload)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	read := func(offset int64) protocol.ContentReadResult {
		params, _ := json.Marshal(protocol.ContentReadParams{ContentRef: ref, Offset: offset})
		raw, err := store.ReadHandler(context.Background(), params)
		if err != nil {
			t.Fatalf("ReadHandler at %d: %v", offset, err)
		}
		return raw.(protocol.ContentReadResult)
	}
	first := read(0)
	if first.NextOffset == nil || *first.NextOffset != int64(protocol.ContentRefChunkBytes) {
		t.Fatalf("first chunk nextOffset = %v", first.NextOffset)
	}
	last := read(*first.NextOffset)
	if last.NextOffset != nil {
		t.Fatalf("final chunk carried nextOffset %v", *last.NextOffset)
	}
	if last.TotalBytes != int64(len(payload)) {
		t.Fatalf("totalBytes = %d", last.TotalBytes)
	}
}

func TestReadHandlerRejectsBadOffsetsAndExpiredRefs(t *testing.T) {
	store := NewStore()
	ref, _, _, err := store.Put([]byte("hello world"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	readErr := func(params protocol.ContentReadParams) error {
		raw, _ := json.Marshal(params)
		_, err := store.ReadHandler(context.Background(), raw)
		if err == nil {
			t.Fatalf("ReadHandler(%+v) succeeded", params)
		}
		return err
	}
	// Out-of-range offset: the ref exists but the position does not.
	if reason := protocolReason(t, readErr(protocol.ContentReadParams{ContentRef: ref, Offset: 1 << 20})); reason != protocol.ErrContentRefExpired {
		t.Fatalf("out-of-range offset reason = %q, want %q", reason, protocol.ErrContentRefExpired)
	}
	// Unknown ref.
	if reason := protocolReason(t, readErr(protocol.ContentReadParams{ContentRef: "content_gone", Offset: 0})); reason != protocol.ErrContentRefExpired {
		t.Fatalf("expired ref reason = %q, want %q", reason, protocol.ErrContentRefExpired)
	}
	// Negative offsets fail strict decode as invalid_params.
	if reason := protocolReason(t, readErr(protocol.ContentReadParams{ContentRef: ref, Offset: -1})); reason != protocol.ErrInvalidParams {
		t.Fatalf("negative offset reason = %q, want %q", reason, protocol.ErrInvalidParams)
	}
}

func TestStoreEnforcesObjectCap(t *testing.T) {
	store := NewStore()
	if _, _, _, err := store.Put(make([]byte, protocol.ContentRefObjectBytes+1)); err == nil {
		t.Fatal("Put accepted an object beyond ContentRefObjectBytes")
	} else if reason := protocolReason(t, err); reason != protocol.ErrFrameTooLarge {
		t.Fatalf("reason = %q, want %q", reason, protocol.ErrFrameTooLarge)
	}
	if _, _, _, err := store.Put(make([]byte, protocol.ContentRefObjectBytes)); err != nil {
		t.Fatalf("Put at exactly the cap failed: %v", err)
	}
}

func TestStoreEvictsOldestBeyondCapacity(t *testing.T) {
	store := NewStore()
	refs := make([]string, 0, storeMaxEntries+1)
	for i := range storeMaxEntries + 1 {
		ref, _, _, err := store.Put(fmt.Appendf(nil, "object-%03d", i))
		if err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
		refs = append(refs, ref)
	}
	params, _ := json.Marshal(protocol.ContentReadParams{ContentRef: refs[0], Offset: 0})
	if _, err := store.ReadHandler(context.Background(), params); err == nil {
		t.Fatal("oldest ref was not evicted beyond the 64-entry cap")
	}
	last, _ := json.Marshal(protocol.ContentReadParams{ContentRef: refs[len(refs)-1], Offset: 0})
	if _, err := store.ReadHandler(context.Background(), last); err != nil {
		t.Fatalf("newest ref was evicted: %v", err)
	}
	if !strings.HasPrefix(refs[0], "content_") {
		t.Fatalf("ref %q lacks the content_ prefix", refs[0])
	}
}
