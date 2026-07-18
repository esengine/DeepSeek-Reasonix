package runtimeservice

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strconv"

	"reasonix/internal/runtimeapi"
)

const cursorVersion = 1

type cursorPayload struct {
	Version  int    `json:"v"`
	Method   string `json:"m"`
	Root     string `json:"r"`
	Session  string `json:"s"`
	Filter   string `json:"f"`
	Revision string `json:"q"`
	Offset   int    `json:"o"`
}

func (s *FileGitService) encodeCursor(payload cursorPayload) runtimeapi.Cursor {
	payload.Version = cursorVersion
	payload.Root = s.rootID
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, s.cursorKey[:])
	_, _ = mac.Write(raw)
	signature := mac.Sum(nil)
	encoded := make([]byte, 0, len(raw)+len(signature))
	encoded = append(encoded, raw...)
	encoded = append(encoded, signature...)
	return runtimeapi.Cursor(base64.RawURLEncoding.EncodeToString(encoded))
}

func (s *FileGitService) decodeCursor(cursor runtimeapi.Cursor) (cursorPayload, error) {
	var payload cursorPayload
	raw, err := base64.RawURLEncoding.DecodeString(string(cursor))
	if err != nil || len(raw) <= sha256.Size || base64.RawURLEncoding.EncodeToString(raw) != string(cursor) {
		return payload, ErrInvalidCursor
	}
	message, signature := raw[:len(raw)-sha256.Size], raw[len(raw)-sha256.Size:]
	mac := hmac.New(sha256.New, s.cursorKey[:])
	_, _ = mac.Write(message)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return payload, ErrInvalidCursor
	}
	if err := json.Unmarshal(message, &payload); err != nil || payload.Version != cursorVersion || payload.Root != s.rootID || payload.Offset < 0 {
		return cursorPayload{}, ErrInvalidCursor
	}
	return payload, nil
}

func (s *FileGitService) pageOffset(cursor runtimeapi.Cursor, method, session, filter, revision string, length int) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	payload, err := s.decodeCursor(cursor)
	if err != nil {
		return 0, err
	}
	if payload.Method != method || payload.Session != session || payload.Filter != filter {
		return 0, ErrInvalidCursor
	}
	if payload.Revision != revision || payload.Offset > length {
		return 0, ErrStaleCursor
	}
	return payload.Offset, nil
}

func snapshotRevision[T any](values []T, extra ...string) string {
	h := sha256.New()
	for _, value := range extra {
		_, _ = h.Write([]byte(strconv.Itoa(len(value))))
		_, _ = h.Write([]byte{':'})
		_, _ = h.Write([]byte(value))
	}
	for _, value := range values {
		raw, _ := json.Marshal(value)
		_, _ = h.Write([]byte(strconv.Itoa(len(raw))))
		_, _ = h.Write([]byte{':'})
		_, _ = h.Write(raw)
	}
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func normalizedPageLimit(limit int) (int, error) {
	if err := runtimeapi.ValidatePageLimit(limit); err != nil {
		return 0, err
	}
	if limit == 0 {
		return runtimeapi.PageDefaultItems, nil
	}
	return limit, nil
}

func normalizedSearchLimit(limit int) (int, error) {
	if err := runtimeapi.ValidateSearchLimit(limit); err != nil {
		return 0, err
	}
	if limit == 0 {
		return runtimeapi.SearchDefaultItems, nil
	}
	return limit, nil
}
