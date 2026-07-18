package idempotency

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"reasonix/internal/remote/protocol"
)

// Fingerprint is the SHA-256 digest of the canonical mutation identity. The
// requestId is deliberately not part of the digest: it is the registry key.
// Method, target and every other typed parameter are part of the digest.
type Fingerprint [sha256.Size]byte

func (f Fingerprint) String() string {
	return "sha256:" + hex.EncodeToString(f[:])
}

// CanonicalJSON converts a JSON-representable typed value into deterministic
// JSON. Object keys are sorted recursively, including keys inside
// json.RawMessage values. Array order and the distinction between null, an
// empty array, and an empty object are preserved.
func CanonicalJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("idempotency: encode canonical JSON: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("idempotency: decode canonical JSON: %w", err)
	}
	var out bytes.Buffer
	if err := writeCanonicalJSON(&out, decoded); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// FingerprintFor builds the canonical identity for one decoded mutation DTO.
// typedParams must be the complete top-level params object and must contain a
// requestId equal to requestID. Only that top-level requestId is omitted from
// the digest; a nested field with the same name remains significant.
func FingerprintFor(method string, target Target, requestID protocol.RequestID, typedParams any) (Fingerprint, error) {
	if strings.TrimSpace(method) == "" || method != strings.TrimSpace(method) {
		return Fingerprint{}, errors.New("idempotency: method must be a non-empty canonical wire name")
	}
	if err := target.Validate(); err != nil {
		return Fingerprint{}, err
	}
	if strings.TrimSpace(string(requestID)) == "" {
		return Fingerprint{}, errors.New("idempotency: requestId is required")
	}

	paramsJSON, err := CanonicalJSON(typedParams)
	if err != nil {
		return Fingerprint{}, fmt.Errorf("idempotency: canonicalize params: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(paramsJSON))
	decoder.UseNumber()
	var params map[string]any
	if err := decoder.Decode(&params); err != nil {
		return Fingerprint{}, errors.New("idempotency: mutation params must be a JSON object")
	}
	wireRequestID, ok := params["requestId"]
	if !ok {
		return Fingerprint{}, errors.New("idempotency: mutation params are missing requestId")
	}
	wireRequestIDString, ok := wireRequestID.(string)
	if !ok || wireRequestIDString != string(requestID) {
		return Fingerprint{}, errors.New("idempotency: params requestId does not match registry requestId")
	}
	delete(params, "requestId")

	envelope := map[string]any{
		"method": method,
		"params": params,
		"target": target.canonicalValue(),
	}
	canonical, err := CanonicalJSON(envelope)
	if err != nil {
		return Fingerprint{}, fmt.Errorf("idempotency: canonicalize fingerprint envelope: %w", err)
	}
	return sha256.Sum256(canonical), nil
}

func writeCanonicalJSON(out *bytes.Buffer, value any) error {
	switch value := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if value {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case string:
		encoded, _ := json.Marshal(value)
		out.Write(encoded)
	case json.Number:
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("idempotency: invalid JSON number %q", value)
		}
		out.Write(encoded)
	case []any:
		out.WriteByte('[')
		for i, item := range value {
			if i != 0 {
				out.WriteByte(',')
			}
			if err := writeCanonicalJSON(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i != 0 {
				out.WriteByte(',')
			}
			encodedKey, _ := json.Marshal(key)
			out.Write(encodedKey)
			out.WriteByte(':')
			if err := writeCanonicalJSON(out, value[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("idempotency: unsupported canonical JSON value %T", value)
	}
	return nil
}
