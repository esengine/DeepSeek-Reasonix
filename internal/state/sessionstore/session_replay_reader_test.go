package sessionstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/state/store"
)

func TestSessionRecordReaderFramesPrettyAndAdjacentRecords(t *testing.T) {
	first := "{\n\"value\": \"quoted \\\" } [ text\",\n\"nested\": [1, {\"a\": true}]\n}"
	second := `{"schema_version":1}`
	input := " \n" + first + "\t" + second
	r := newSessionRecordReader(strings.NewReader(input), "fixture", int64(len(input)))
	for i, want := range []string{first, second} {
		raw, start, err := r.next()
		if err != nil || string(raw) != want || input[start:r.offset] != want {
			t.Fatalf("record %d: %q at %d, err=%v", i, raw, start, err)
		}
	}
	if _, _, err := r.next(); !errors.Is(err, io.EOF) {
		t.Fatalf("end = %v, want EOF", err)
	}
}

func TestSessionRecordReaderRefusesGrowthPastEncodedBudget(t *testing.T) {
	input := `{"one":1}{"two":2}`
	r := newSessionRecordReader(strings.NewReader(input), "fixture", 9)
	if _, _, err := r.next(); err != nil {
		t.Fatal(err)
	}
	_, _, err := r.next()
	var limitErr *SessionReplayLimitError
	if !errors.As(err, &limitErr) || limitErr.Resource != "encoded_bytes" || limitErr.Value != 10 {
		t.Fatalf("growth error = %v", err)
	}
}

func TestSessionRecordReaderBoundsOneLargeRecord(t *testing.T) {
	input := io.MultiReader(strings.NewReader(`{"content":"`), io.LimitReader(replayPayloadReader{}, 64<<20), strings.NewReader(`"}`))
	r := newSessionRecordReader(input, "fixture", 512<<20)
	_, _, err := r.next()
	var limitErr *SessionReplayLimitError
	if !errors.As(err, &limitErr) || limitErr.Resource != "event_record_bytes" || limitErr.Value != (64<<20)+1 {
		t.Fatalf("record bound error = %v", err)
	}
}

type replayPayloadReader struct{}

func (replayPayloadReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestNativeReplayBoundsAggregateMessagePayload(t *testing.T) {
	path := writeDAGSession(t)
	f, err := os.Create(store.SessionEventLog(path))
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	body := strings.Repeat("x", 1<<20)
	for i := range 65 {
		if err := enc.Encode(map[string]any{"schema_version": 1, "type": "append", "message_index": i, "messages": []any{map[string]any{"role": "user", "content": body}}}); err != nil {
			f.Close()
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	msgs, fromEvents, damaged, err := loadSessionMessages(path)
	var limitErr *SessionReplayLimitError
	if !errors.As(err, &limitErr) || limitErr.Resource != "message_payload_bytes" || msgs != nil || !fromEvents || damaged {
		t.Fatalf("native load = %d messages, events=%v, damaged=%v, err=%v", len(msgs), fromEvents, damaged, err)
	}
}

func TestNullEventKeepsUnsupportedSchemaRefusal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []string
	}{
		{"native", []string{`{"schema_version":1,"type":"replace","messages":[{"role":"user","content":"prefix"}]}`, "null"}},
		{"dag", append(baseDAG(t), "null")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeDAGSession(t, tc.lines...)
			s, err := LoadSession(path)
			if err == nil || s != nil {
				t.Fatalf("unsupported null record returned a recoverable transcript: session=%v err=%v", s != nil, err)
			}
		})
	}
}

// referenceRecordNext is the byte-at-a-time framing the bulk scan must agree
// with on every record, offset and refusal.
func referenceRecordNext(r *sessionRecordReader) ([]byte, int64, error) {
	r.buffer = r.buffer[:0]
	for {
		b, err := r.readByte()
		if err != nil {
			return nil, r.offset, err
		}
		if b == ' ' || b == '\t' || b == '\r' || b == '\n' {
			continue
		}
		if b == 'n' {
			start := r.offset - 1
			r.buffer = append(r.buffer, b)
			for range 3 {
				b, err := r.readByte()
				if err != nil {
					if errors.Is(err, io.EOF) {
						err = io.ErrUnexpectedEOF
					}
					return nil, start, err
				}
				r.buffer = append(r.buffer, b)
			}
			return r.buffer, start, nil
		}
		if b != '{' {
			return nil, r.offset - 1, fmt.Errorf("session event record must be an object")
		}
		r.buffer = append(r.buffer, b)
		break
	}
	start := r.offset - 1
	depth := 1
	quoted, escaped := false, false
	for depth != 0 {
		b, err := r.readByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return nil, start, err
		}
		if int64(len(r.buffer)) >= sessionReplayMaxPayloadBytes {
			return nil, start, sessionReplayLimitError(r.path, "event_record_bytes", int64(len(r.buffer))+1, sessionReplayMaxPayloadBytes)
		}
		r.buffer = append(r.buffer, b)
		if quoted {
			if escaped {
				escaped = false
			} else if b == '\\' {
				escaped = true
			} else if b == '"' {
				quoted = false
			}
			continue
		}
		switch b {
		case '"':
			quoted = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		}
	}
	return r.buffer, start, nil
}

type tinyReader struct {
	data []byte
	step int
}

func (t *tinyReader) Read(p []byte) (int, error) {
	if len(t.data) == 0 {
		return 0, io.EOF
	}
	n := min(len(p), t.step, len(t.data))
	copy(p, t.data[:n])
	t.data = t.data[n:]
	return n, nil
}

func TestBulkRecordFramingAgreesWithByteAtATime(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 11))
	atoms := []string{`{}`, `{"a":1}`, `{"s":"x{y}[z]"}`, `{"e":"q\"uote\\"}`, `{"n":{"m":[1,{"k":"v"}]}}`,
		"\n", "  ", "\r\n\t", `null`, `{"u":"\u00e9 é 中文"}`, `{"long":"` + strings.Repeat("abc\\\"d", 400) + `"}`,
		`{"unterminated":"`, `{"deep":[[[[{"x":[]}]]]]}`, `x`, `{`, `"`}
	for iter := range 400 {
		var doc strings.Builder
		for range 1 + rng.IntN(8) {
			doc.WriteString(atoms[rng.IntN(len(atoms))])
		}
		data := []byte(doc.String())
		maxBytes := int64(1 + rng.IntN(len(data)+40))
		step := 1 + rng.IntN(64)
		got := newSessionRecordReader(&tinyReader{data: slices.Clone(data), step: step}, "p", maxBytes)
		want := newSessionRecordReader(&tinyReader{data: slices.Clone(data), step: step}, "p", maxBytes)
		for call := range 12 {
			gb, go2, gerr := got.next()
			wb, wo, werr := referenceRecordNext(want)
			if string(gb) != string(wb) || go2 != wo || fmt.Sprint(gerr) != fmt.Sprint(werr) || got.offset != want.offset {
				t.Fatalf("iter %d call %d (max %d step %d) on %q:\n bulk: %q off=%d err=%v read=%d\n ref:  %q off=%d err=%v read=%d",
					iter, call, maxBytes, step, data, gb, go2, gerr, got.offset, wb, wo, werr, want.offset)
			}
			if gerr != nil {
				break
			}
		}
	}
}

func BenchmarkRecordFraming(b *testing.B) {
	rec := `{"type":"append","messages":[{"role":"assistant","content":"` + strings.Repeat("a plain english sentence, with \\\"quotes\\\" and {braces}. ", 120) + `"}]}` + "\n"
	data := []byte(strings.Repeat(rec, 2000))
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		r := newSessionRecordReader(bytes.NewReader(data), "p", int64(len(data))+1)
		for {
			if _, _, err := r.next(); err != nil {
				break
			}
		}
	}
}
