package browseripc

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestFrameRoundTrip proves both sides of the wire agree on the length-prefixed
// framing: the Go host and the Electron companion share this exact byte format.
func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	req := Request{
		ProtocolVersion: ProtocolVersion,
		RequestID:       "r-1",
		OwnerID:         "chat-42",
		Method:          "tab.open",
		Params:          json.RawMessage(`{"ownerId":"chat-42","url":"https://example.com","disposition":"foreground"}`),
	}
	if err := WriteRequest(&buf, req); err != nil {
		t.Fatalf("WriteRequest: %v", err)
	}
	payload, err := ReadFrame(&buf, FrameMaxBytes)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	var got Request
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Method != req.Method || got.RequestID != req.RequestID || got.OwnerID != req.OwnerID {
		t.Errorf("round trip mismatch: %+v", got)
	}
	if err := ValidateRequest(got); err != nil {
		t.Errorf("round-tripped request invalid: %v", err)
	}
}

// TestFrameBigEndianHeader pins the exact wire bytes so the companion's
// implementation cannot drift into little-endian or text framing.
func TestFrameBigEndianHeader(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, []byte(`{}`)); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	want := []byte{0x00, 0x00, 0x00, 0x02, '{', '}'}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("wire bytes = %x, want %x", buf.Bytes(), want)
	}
}

func TestReadFrameRejectsOversize(t *testing.T) {
	var buf bytes.Buffer
	header := []byte{0x40, 0x00, 0x00, 0x01} // 1 GiB + 1 announced
	buf.Write(header)
	payload := make([]byte, 1<<20)
	buf.Write(payload)
	_, err := ReadFrame(&buf, FrameMaxBytes)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("err = %v, want ErrFrameTooLarge", err)
	}
}

func TestReadFrameRejectsZero(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0, 0, 0, 0})
	if _, err := ReadFrame(&buf, FrameMaxBytes); !errors.Is(err, ErrZeroFrame) {
		t.Fatalf("err = %v, want ErrZeroFrame", err)
	}
}

func TestWriteFrameRejectsOversizePayload(t *testing.T) {
	if err := WriteFrame(io.Discard, make([]byte, FrameMaxBytes+1)); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("err = %v, want ErrFrameTooLarge", err)
	}
}

func TestReadFrameTruncated(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0, 0, 0, 5, 'a', 'b'})
	if _, err := ReadFrame(&buf, FrameMaxBytes); err == nil || errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("err = %v, want truncation error", err)
	}
}

// TestValidateRequestRejects pins the fail-closed envelope rules.
func TestValidateRequestRejects(t *testing.T) {
	cases := []struct {
		name string
		req  Request
		want string
	}{
		{"wrong protocol version", Request{ProtocolVersion: 2, RequestID: "r", Method: "tab.list", Params: raw(`{"ownerId":"o"}`)}, "protocol version"},
		{"unknown method", Request{ProtocolVersion: 1, RequestID: "r", Method: "tab.explode", Params: raw(`{}`)}, "unknown method"},
		{"oversized request id", Request{ProtocolVersion: 1, RequestID: strings.Repeat("x", MaxRequestIDBytes+1), Method: "tab.list", Params: raw(`{"ownerId":"o"}`)}, "requestId exceeds"},
		{"unknown field", Request{ProtocolVersion: 1, RequestID: "r", Method: "tab.list", Params: raw(`{"ownerId":"o","sneaky":true}`)}, "unknown field"},
		{"missing required owner", Request{ProtocolVersion: 1, RequestID: "r", Method: "tab.list", Params: raw(`{}`)}, "ownerId is required"},
		{"non-http url", Request{ProtocolVersion: 1, RequestID: "r", OwnerID: "o", Method: "tab.open", Params: raw(`{"ownerId":"o","url":"file:///etc/passwd","disposition":"foreground"}`)}, "http(s)"},
		{"invalid disposition", Request{ProtocolVersion: 1, RequestID: "r", OwnerID: "o", Method: "tab.open", Params: raw(`{"ownerId":"o","url":"https://a.com","disposition":"sideways"}`)}, "invalid disposition"},
		{"invalid action", Request{ProtocolVersion: 1, RequestID: "r", OwnerID: "o", Method: "tab.act", Params: raw(`{"ownerId":"o","tabId":"t","action":"eval"}`)}, "invalid action"},
		{"click without ref", Request{ProtocolVersion: 1, RequestID: "r", OwnerID: "o", Method: "tab.act", Params: raw(`{"ownerId":"o","tabId":"t","action":"click"}`)}, "ref is required"},
		{"empty scopes", Request{ProtocolVersion: 1, RequestID: "r", Method: "data.clear", Params: raw(`{"scopes":[]}`)}, "scopes is required"},
		{"unknown scope", Request{ProtocolVersion: 1, RequestID: "r", Method: "data.clear", Params: raw(`{"scopes":["cookies","everything"]}`)}, "invalid scope"},
		{"bad waitUntil", Request{ProtocolVersion: 1, RequestID: "r", OwnerID: "o", Method: "tab.wait", Params: raw(`{"ownerId":"o","tabId":"t","waitUntil":"forever"}`)}, "invalid waitUntil"},
		{"missing tab", Request{ProtocolVersion: 1, RequestID: "r", OwnerID: "o", Method: "tab.activate", Params: raw(`{"ownerId":"o"}`)}, "tabId is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRequest(tc.req)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestValidateResponse(t *testing.T) {
	if err := ValidateResponse(Response{ProtocolVersion: 1, RequestID: "r", Result: raw(`{}`)}); err != nil {
		t.Errorf("valid response rejected: %v", err)
	}
	if err := ValidateResponse(Response{ProtocolVersion: 1, RequestID: "r", Error: &RPCError{Code: CodeTabBusy, Message: "busy"}}); err != nil {
		t.Errorf("valid error response rejected: %v", err)
	}
	for _, tc := range []struct {
		name string
		resp Response
		want string
	}{
		{"both result and error", Response{ProtocolVersion: 1, RequestID: "r", Result: raw(`{}`), Error: &RPCError{Code: CodeInternal}}, "exactly one"},
		{"neither", Response{ProtocolVersion: 1, RequestID: "r"}, "exactly one"},
		{"unknown code", Response{ProtocolVersion: 1, RequestID: "r", Error: &RPCError{Code: "mystery"}}, "unknown error code"},
		{"wrong version", Response{ProtocolVersion: 9, RequestID: "r", Result: raw(`{}`)}, "protocol version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateResponse(tc.resp); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestValidateEvent(t *testing.T) {
	if err := ValidateEvent(Event{ProtocolVersion: 1, Event: EventBody{Name: "tab.changed", OwnerID: "o", Data: raw(`{}`)}}); err != nil {
		t.Errorf("valid event rejected: %v", err)
	}
	if err := ValidateEvent(Event{ProtocolVersion: 1, Event: EventBody{Name: "tab.mystery", Data: raw(`{}`)}}); err == nil {
		t.Error("unknown event accepted")
	}
	if err := ValidateEvent(Event{ProtocolVersion: 1, Event: EventBody{Name: "tab.changed"}}); err == nil {
		t.Error("data-less event accepted")
	}
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }
