package rpcwire

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
)

// StrictRequestFrame is one validated JSON-RPC 2.0 NDJSON request. Raw retains
// the exact bytes consumed from the reader, including line ending and harmless
// surrounding whitespace, so a bootstrap proxy can forward it unchanged.
type StrictRequestFrame struct {
	Raw    []byte
	ID     json.RawMessage
	Method string
	Params json.RawMessage
}

// ResponseIDForError returns an ID that is safe to place in a JSON-RPC error
// response. Invalid request IDs must never be reflected back because doing so
// would make the error response invalid too. Missing and invalid IDs therefore
// become null; valid string, integer, and null IDs retain their value.
func ResponseIDForError(id json.RawMessage) json.RawMessage {
	id = trimSpace(id)
	if !validRPCID(id) {
		return json.RawMessage("null")
	}
	return append(json.RawMessage(nil), id...)
}

// ReadStrictRequestFrame reads exactly one request while retaining br and any
// buffered remainder for subsequent proxying. It shares rpcwire's strict frame
// validator and inbound byte-limit semantics.
func ReadStrictRequestFrame(br *bufio.Reader, maxBytes int) (StrictRequestFrame, error) {
	var raw []byte
	for {
		chunk, err := br.ReadSlice('\n')
		raw = append(raw, chunk...)
		if maxBytes > 0 && len(raw) > maxBytes {
			return StrictRequestFrame{Raw: append([]byte(nil), raw...)}, &FrameTooLargeError{Direction: "inbound", Size: len(raw), Limit: maxBytes}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return StrictRequestFrame{}, err
		}
		if len(raw) == 0 {
			return StrictRequestFrame{}, io.EOF
		}
		break
	}

	payload := trimSpace(raw)
	var in inbound
	if err := json.Unmarshal(payload, &in); err != nil {
		return StrictRequestFrame{Raw: append([]byte(nil), raw...)}, err
	}
	frame := StrictRequestFrame{
		Raw: append([]byte(nil), raw...), ID: append(json.RawMessage(nil), in.ID...),
		Method: in.Method, Params: append(json.RawMessage(nil), in.Params...),
	}
	if err := validateStrictFrame(payload, in); err != nil {
		return frame, err
	}
	if in.Method == "" || len(in.ID) == 0 {
		return frame, errors.New("rpcwire: bootstrap frame must be a request")
	}
	return frame, nil
}
