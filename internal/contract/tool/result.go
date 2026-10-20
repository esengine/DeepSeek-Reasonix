package tool

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// ErrResultUnavailable means the call context carries no session result reader.
var ErrResultUnavailable = errors.New("tool result is unavailable in this session")

// ResultRecorder retains a lossless tool result outside the model transcript.
type ResultRecorder func(json.RawMessage) (string, error)

type resultRecorderKey struct{}
type resultReaderKey struct{}
type ResultReader func(string) (io.ReadCloser, error)

const ResultReferencePrefix = "tool-result:"

// ResultRetainLimit is the largest raw result kept on disk, and so the most a
// reader of a retained result ever has to load.
const ResultRetainLimit = 16 << 20

func IsResultReference(ref string) bool { return strings.HasPrefix(ref, ResultReferencePrefix) }

func WithResultReader(ctx context.Context, read ResultReader) context.Context {
	return context.WithValue(ctx, resultReaderKey{}, read)
}

func OpenResult(ctx context.Context, ref string) (io.ReadCloser, error) {
	read, _ := ctx.Value(resultReaderKey{}).(ResultReader)
	if read == nil {
		return nil, ErrResultUnavailable
	}
	return read(ref)
}

func WithResultRecorder(ctx context.Context, record ResultRecorder) context.Context {
	return context.WithValue(ctx, resultRecorderKey{}, record)
}

// CanRecordResult reports whether this call has somewhere to retain a result.
func CanRecordResult(ctx context.Context) bool {
	record, _ := ctx.Value(resultRecorderKey{}).(ResultRecorder)
	return record != nil
}

func RecordResult(ctx context.Context, raw json.RawMessage) (string, error) {
	record, _ := ctx.Value(resultRecorderKey{}).(ResultRecorder)
	if record == nil {
		return "", nil
	}
	return record(append(json.RawMessage(nil), raw...))
}
