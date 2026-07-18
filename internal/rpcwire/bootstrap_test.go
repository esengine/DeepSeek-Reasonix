package rpcwire

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"testing"
)

func TestReadStrictRequestFramePreservesExactFrameAndBufferedRemainder(t *testing.T) {
	first := "  {\"jsonrpc\":\"2.0\",\"id\":\"init-1\",\"method\":\"remote/initialize\",\"params\":{\"x\":1}} \r\n"
	second := "{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"next\",\"params\":{}}\n"
	reader := bufio.NewReaderSize(bytes.NewBufferString(first+second), 16)
	frame, err := ReadStrictRequestFrame(reader, 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	if string(frame.Raw) != first || string(frame.ID) != `"init-1"` || frame.Method != "remote/initialize" || string(frame.Params) != `{"x":1}` {
		t.Fatalf("frame = %+v raw=%q", frame, frame.Raw)
	}
	remainder, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(remainder) != second {
		t.Fatalf("buffered remainder = %q, want %q", remainder, second)
	}
}

func TestReadStrictRequestFrameRejectsNonRequestsAndLimit(t *testing.T) {
	for _, input := range []string{
		"{\"jsonrpc\":\"2.0\",\"method\":\"note\",\"params\":{}}\n",
		"{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n",
		"{\"jsonrpc\":\"1.0\",\"id\":1,\"method\":\"bad\"}\n",
	} {
		if _, err := ReadStrictRequestFrame(bufio.NewReader(bytes.NewBufferString(input)), 8<<20); err == nil {
			t.Fatalf("invalid bootstrap frame accepted: %s", input)
		}
	}
	_, err := ReadStrictRequestFrame(bufio.NewReader(bytes.NewBufferString("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"x\"}\n")), 8)
	var tooLarge *FrameTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("frame limit error = %v", err)
	}
}

func TestResponseIDForErrorRejectsUnsafeIDs(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{id: `"request"`, want: `"request"`},
		{id: `-17`, want: `-17`},
		{id: `null`, want: `null`},
		{id: ``, want: `null`},
		{id: `true`, want: `null`},
		{id: `{}`, want: `null`},
		{id: `[]`, want: `null`},
		{id: `1.5`, want: `null`},
	}
	for _, test := range tests {
		if got := string(ResponseIDForError(json.RawMessage(test.id))); got != test.want {
			t.Errorf("ResponseIDForError(%q) = %s, want %s", test.id, got, test.want)
		}
	}
}
