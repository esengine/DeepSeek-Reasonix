package extension

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// contentStore scripts the host's content store for host/content/read.
type contentStore struct {
	mu       sync.Mutex
	objects  map[string][]byte
	requests []ContentReadParams
	tamper   bool // flip one byte in the first served chunk
}

func newContentStore() *contentStore {
	return &contentStore{objects: make(map[string][]byte)}
}

func (s *contentStore) put(data string) (ref string, descriptor ExternalizedField) {
	sum := sha256.Sum256([]byte(data))
	ref = "content_test_" + hex.EncodeToString(sum[:4])
	s.mu.Lock()
	s.objects[ref] = []byte(data)
	s.mu.Unlock()
	return ref, ExternalizedField{
		JSONPointer: "/payload", ContentRef: ref,
		TotalBytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:]),
	}
}

// handler pages like the real host: at most ContentRefChunkBytes per answer,
// NextOffset null at the end, content_ref_expired for unknown refs.
func (s *contentStore) handler(params json.RawMessage) (any, *hostError) {
	var p ContentReadParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &hostError{Code: CodeInvalidParams, Message: "bad params"}
	}
	s.mu.Lock()
	s.requests = append(s.requests, p)
	data, ok := s.objects[p.ContentRef]
	tamper := s.tamper
	s.mu.Unlock()
	if !ok {
		return nil, &hostError{
			Code:    DomainErrorCode,
			Message: "The referenced content has expired.",
			Data:    ProtocolErrorData{Reason: ErrContentRefExpired, Retryable: true},
		}
	}
	if p.Offset < 0 || p.Offset > int64(len(data)) {
		return nil, &hostError{
			Code:    DomainErrorCode,
			Message: "The referenced content has expired.",
			Data:    ProtocolErrorData{Reason: ErrContentRefExpired, Retryable: true},
		}
	}
	end := p.Offset + ContentRefChunkBytes
	if end > int64(len(data)) {
		end = int64(len(data))
	}
	chunk := append([]byte(nil), data[p.Offset:end]...)
	if tamper && p.Offset == 0 && len(chunk) > 0 {
		chunk[0] ^= 0xFF
	}
	var next *int64
	if end < int64(len(data)) {
		value := end
		next = &value
	}
	sum := sha256.Sum256(data)
	return ContentReadResult{
		ContentRef: p.ContentRef, Offset: p.Offset,
		DataBase64: base64.StdEncoding.EncodeToString(chunk),
		NextOffset: next, TotalBytes: int64(len(data)),
		SHA256: hex.EncodeToString(sum[:]), Encoding: ContentUTF8,
	}, nil
}

func (s *contentStore) requestedOffsets() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []int64
	for _, r := range s.requests {
		out = append(out, r.Offset)
	}
	return out
}

// TestReadContentRefMultiChunk reads a payload spanning several chunks and
// verifies paging offsets and SHA-256.
func TestReadContentRefMultiChunk(t *testing.T) {
	store := newContentStore()
	big := strings.Repeat("abcdefghij", ContentRefChunkBytes/4) // exactly 2.5 chunks → 3 pages
	ref, _ := store.put(big)
	var data []byte
	var readErr error
	hook := map[string]InterceptorFunc{
		"tool.before": func(ctx context.Context, _ string, _ json.RawMessage) (*InterceptResult, error) {
			data, readErr = ReadContentRef(ctx, ref)
			return Continue(), nil
		},
	}
	host, _ := startFakeHost(t, basicHandler(), Options{Interceptors: hook})
	host.onRequest(MethodHostContentRead, store.handler)
	host.handshake(t)
	host.request(MethodExtensionIntercept, InterceptParams{
		Event: EventToolBefore, Seq: 1, Payload: json.RawMessage(`{}`),
	})
	if readErr != nil {
		t.Fatalf("ReadContentRef: %v", readErr)
	}
	if string(data) != big {
		t.Fatalf("reassembled %d bytes, want %d identical bytes", len(data), len(big))
	}
	offsets := store.requestedOffsets()
	if len(offsets) != 3 {
		t.Fatalf("read offsets = %v, want 3 pages", offsets)
	}
	for i, offset := range offsets {
		if offset != int64(i)*ContentRefChunkBytes {
			t.Fatalf("offset %d = %d, want %d", i, offset, int64(i)*ContentRefChunkBytes)
		}
	}
}

// TestReadContentRefTamper detects a SHA-256 mismatch.
func TestReadContentRefTamper(t *testing.T) {
	store := newContentStore()
	store.tamper = true
	ref, _ := store.put(strings.Repeat("x", ContentRefChunkBytes+10))
	var readErr error
	interceptors := map[string]InterceptorFunc{
		"tool.before": func(ctx context.Context, _ string, _ json.RawMessage) (*InterceptResult, error) {
			_, readErr = ReadContentRef(ctx, ref)
			return Continue(), nil
		},
	}
	host, _ := startFakeHost(t, basicHandler(), Options{Interceptors: interceptors})
	host.onRequest(MethodHostContentRead, store.handler)
	host.handshake(t)
	host.request(MethodExtensionIntercept, InterceptParams{
		Event: EventToolBefore, Seq: 1, Payload: json.RawMessage(`{}`),
	})
	var protocolErr *ProtocolError
	if !errors.As(readErr, &protocolErr) || !strings.Contains(protocolErr.Message, "SHA-256") {
		t.Fatalf("readErr = %v, want SHA-256 mismatch protocol error", readErr)
	}
}

// TestReadContentRefExpired maps the wire reason to a *ProtocolError.
func TestReadContentRefExpired(t *testing.T) {
	store := newContentStore()
	var readErr error
	interceptors := map[string]InterceptorFunc{
		"tool.before": func(ctx context.Context, _ string, _ json.RawMessage) (*InterceptResult, error) {
			_, readErr = ReadContentRef(ctx, "content_gone")
			return Continue(), nil
		},
	}
	host, _ := startFakeHost(t, basicHandler(), Options{Interceptors: interceptors})
	host.onRequest(MethodHostContentRead, store.handler)
	host.handshake(t)
	host.request(MethodExtensionIntercept, InterceptParams{
		Event: EventToolBefore, Seq: 1, Payload: json.RawMessage(`{}`),
	})
	var protocolErr *ProtocolError
	if !errors.As(readErr, &protocolErr) {
		t.Fatalf("readErr = %v, want *ProtocolError", readErr)
	}
	if protocolErr.Reason != ErrContentRefExpired {
		t.Fatalf("reason = %q, want content_ref_expired", protocolErr.Reason)
	}
}

// TestInterceptExternalizedPayload runs the full transparent rehydration:
// the host sends payload:null plus the externalized envelope, and the
// interceptor receives the reassembled bytes.
func TestInterceptExternalizedPayload(t *testing.T) {
	store := newContentStore()
	big := `{"text":"` + strings.Repeat("lorem ", ContentRefChunkBytes/3) + `"}`
	_, descriptor := store.put(big)
	var got json.RawMessage
	interceptors := map[string]InterceptorFunc{
		"input.receive": func(_ context.Context, _ string, payload json.RawMessage) (*InterceptResult, error) {
			got = payload
			return Continue(), nil
		},
	}
	host, _ := startFakeHost(t, basicHandler(), Options{Interceptors: interceptors})
	host.onRequest(MethodHostContentRead, store.handler)
	host.handshake(t)
	resp := host.request(MethodExtensionIntercept, map[string]any{
		"event": "input.receive", "seq": 1, "payload": nil, "timeoutMillis": 0,
		"externalized": []ExternalizedField{descriptor},
	})
	if resp.Err != nil {
		t.Fatalf("intercept failed: %+v", resp.Err)
	}
	if string(got) != big {
		t.Fatalf("payload = %d bytes, want rehydrated %d bytes", len(got), len(big))
	}
}

// TestInterceptExternalizedViolation rejects an inline payload alongside an
// envelope.
func TestInterceptExternalizedViolation(t *testing.T) {
	interceptors := map[string]InterceptorFunc{
		"*": func(context.Context, string, json.RawMessage) (*InterceptResult, error) { return Continue(), nil },
	}
	host, _ := startFakeHost(t, basicHandler(), Options{Interceptors: interceptors})
	host.handshake(t)
	resp := host.request(MethodExtensionIntercept, map[string]any{
		"event": "input.receive", "seq": 1, "payload": json.RawMessage(`{"x":1}`), "timeoutMillis": 0,
		"externalized": []ExternalizedField{{
			JSONPointer: "/payload", ContentRef: "content_fake",
			TotalBytes: 7, SHA256: strings.Repeat("0", 64),
		}},
	})
	if resp.Err == nil {
		t.Fatal("expected a protocol error for inline payload plus envelope")
	}
	data, _ := resp.Err.Data.(ProtocolErrorData)
	if data.Reason != ErrProtocolError {
		t.Fatalf("reason = %q, want protocol_error", data.Reason)
	}
}

// TestResolveExternalizedHelper covers the exported helper directly,
// including the pointer check.
func TestResolveExternalizedHelper(t *testing.T) {
	store := newContentStore()
	_, descriptor := store.put(`{"hello":"world"}`)
	var resolved json.RawMessage
	var resolveErr error
	interceptors := map[string]InterceptorFunc{
		"tool.before": func(ctx context.Context, _ string, _ json.RawMessage) (*InterceptResult, error) {
			resolved, resolveErr = ResolveExternalized(ctx, nil, []ExternalizedField{descriptor}, "/payload")
			return Continue(), nil
		},
	}
	host, _ := startFakeHost(t, basicHandler(), Options{Interceptors: interceptors})
	host.onRequest(MethodHostContentRead, store.handler)
	host.handshake(t)
	host.request(MethodExtensionIntercept, InterceptParams{
		Event: EventToolBefore, Seq: 1, Payload: json.RawMessage(`{}`),
	})
	if resolveErr != nil {
		t.Fatalf("ResolveExternalized: %v", resolveErr)
	}
	if string(resolved) != `{"hello":"world"}` {
		t.Fatalf("resolved = %s", resolved)
	}

	// A wrong pointer must fail without any content read.
	before := len(store.requestedOffsets())
	var wrongErr error
	interceptors2 := map[string]InterceptorFunc{
		"tool.before": func(ctx context.Context, _ string, _ json.RawMessage) (*InterceptResult, error) {
			_, wrongErr = ResolveExternalized(ctx, nil, []ExternalizedField{descriptor}, "/replacement")
			return Continue(), nil
		},
	}
	host2, _ := startFakeHost(t, basicHandler(), Options{Interceptors: interceptors2})
	host2.onRequest(MethodHostContentRead, store.handler)
	host2.handshake(t)
	host2.request(MethodExtensionIntercept, InterceptParams{
		Event: EventToolBefore, Seq: 1, Payload: json.RawMessage(`{}`),
	})
	var protocolErr *ProtocolError
	if !errors.As(wrongErr, &protocolErr) || protocolErr.Reason != ErrProtocolError {
		t.Fatalf("wrongErr = %v, want protocol_error", wrongErr)
	}
	if got := len(store.requestedOffsets()); got != before {
		t.Fatalf("content reads happened despite the pointer violation: %d → %d", before, got)
	}
}

// TestResolveExternalizedNoConnection requires an SDK callback context.
func TestResolveExternalizedNoConnection(t *testing.T) {
	if _, err := ResolveExternalized(context.Background(), nil, nil, "/payload"); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("err = %v, want ErrNoConnection", err)
	}
	if _, err := ReadContentRef(context.Background(), "content_x"); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("err = %v, want ErrNoConnection", err)
	}
}

// TestEventExternalizedPayload rehydrates event payloads too.
func TestEventExternalizedPayload(t *testing.T) {
	store := newContentStore()
	big := fmt.Sprintf(`{"blob":"%s"}`, strings.Repeat("z", ContentRefChunkBytes+100))
	_, descriptor := store.put(big)
	seen := make(chan json.RawMessage, 1)
	opts := Options{Observer: func(_ context.Context, _ string, payload json.RawMessage) { seen <- payload }}
	host, _ := startFakeHost(t, basicHandler(), opts)
	host.onRequest(MethodHostContentRead, store.handler)
	host.handshake(t)
	host.notify(MethodExtensionEvent, map[string]any{
		"event": "session.end", "payload": nil, "externalized": []ExternalizedField{descriptor},
	})
	select {
	case payload := <-seen:
		if string(payload) != big {
			t.Fatalf("payload = %d bytes, want %d", len(payload), len(big))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("observer not called for the externalized event")
	}
}
