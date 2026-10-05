package chartspec

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// Parse decodes and validates a spec. The size cap is checked before any
// decoding, unknown fields are refused, and nothing is returned on error.
func Parse(raw []byte) (*Spec, error) {
	if len(raw) > MaxSpecBytes {
		return nil, exceeded(CapBytes, "")
	}
	if err := rejectDuplicateKeys(raw); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var s Spec
	if err := dec.Decode(&s); err != nil {
		return nil, decodeError(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fail(CodeSchemaInvalid, "")
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

func decodeError(err error) error {
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		return fail(CodeSchemaInvalid, pointerOf(te.Field))
	}
	return fail(CodeSchemaInvalid, "")
}

func pointerOf(field string) string {
	if field == "" {
		return ""
	}
	out := make([]byte, 0, len(field)+1)
	out = append(out, '/')
	for _, ch := range []byte(field) {
		if ch == '.' {
			out = append(out, '/')
			continue
		}
		out = append(out, ch)
	}
	return string(out)
}

// Canonical is the byte form a spec is stored and hashed in: struct field
// order, no whitespace, so equal specs give equal bytes.
func (s *Spec) Canonical() ([]byte, error) { return json.Marshal(s) }

// rejectDuplicateKeys refuses an object that repeats a key, case-folded because
// the decoder matches field names that way and keeps the last one, so a spec could show one value to a reader of the text and
// another to the renderer. Iterative, so nesting depth costs no stack.
func rejectDuplicateKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var keys []map[string]struct{}
	var expectKey []bool
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{':
				keys = append(keys, map[string]struct{}{})
				expectKey = append(expectKey, true)
			case '[':
				keys = append(keys, nil)
				expectKey = append(expectKey, false)
			default:
				keys = keys[:len(keys)-1]
				expectKey = expectKey[:len(expectKey)-1]
				if n := len(expectKey); n > 0 && keys[n-1] != nil {
					expectKey[n-1] = true
				}
			}
			continue
		}
		n := len(keys)
		if n == 0 || keys[n-1] == nil {
			continue
		}
		if expectKey[n-1] {
			k := strings.ToLower(tok.(string))
			if _, dup := keys[n-1][k]; dup {
				return fail(CodeSchemaInvalid, "")
			}
			keys[n-1][k] = struct{}{}
			expectKey[n-1] = false
		} else {
			expectKey[n-1] = true
		}
	}
}
