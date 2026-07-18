package runtimeservice

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/sessiondisplay"
)

const promptHistoryCursorVersion = 1

// PromptHistorySessionSource is the adapter-private binding between an opaque
// RuntimeAPI Session and its durable Host transcript. Paths are inputs only;
// PromptHistoryService never returns or embeds them in cursors.
type PromptHistorySessionSource struct {
	Session     runtimeapi.SessionRef
	SessionDir  string `json:"-"`
	SessionPath string `json:"-"`
}

// PromptHistoryOptions exposes cursor-key injection only for deterministic
// tests. Production callers leave CursorKey empty and receive a random key.
type PromptHistoryOptions struct {
	CursorKey []byte
}

// PromptHistoryService owns target-neutral prompt-history scanning and
// pagination. It imports no daemon, SSH, Wails, or Remote protocol package and
// is therefore shared by Local and Remote adapters.
type PromptHistoryService struct {
	cursorKey [sha256.Size]byte
}

type promptHistoryCursor struct {
	Version   int    `json:"v"`
	Method    string `json:"m"`
	Workspace string `json:"w"`
	Revision  string `json:"r"`
	Offset    int    `json:"o"`
}

type promptHistorySession struct {
	session  runtimeapi.SessionRef
	activity int64
	entries  []runtimeapi.PromptHistoryEntry
}

type promptHistoryRecord struct {
	Kind           string          `json:"kind"`
	Type           string          `json:"type"`
	Role           string          `json:"role"`
	Text           string          `json:"text"`
	Content        string          `json:"content"`
	Time           json.RawMessage `json:"time"`
	Timestamp      json.RawMessage `json:"timestamp"`
	CreatedAt      json.RawMessage `json:"createdAt"`
	CreatedAtSnake json.RawMessage `json:"created_at"`
	UpdatedAt      json.RawMessage `json:"updatedAt"`
	UpdatedAtSnake json.RawMessage `json:"updated_at"`
}

func NewPromptHistoryService(options PromptHistoryOptions) (*PromptHistoryService, error) {
	service := &PromptHistoryService{}
	if len(options.CursorKey) == 0 {
		if _, err := rand.Read(service.cursorKey[:]); err != nil {
			return nil, errors.New("runtime service: initialize prompt-history cursor key")
		}
		return service, nil
	}
	// Hashing accepts convenient test/application keys of any non-zero length
	// while ensuring HMAC always receives a fixed-size key.
	service.cursorKey = sha256.Sum256(options.CursorKey)
	return service, nil
}

// History returns newest prompts first, grouping Sessions by their durable
// activity order and preserving newest-first order inside each Session.
func (s *PromptHistoryService) History(
	ctx context.Context,
	input runtimeapi.PromptHistoryInput,
	sources []PromptHistorySessionSource,
) (runtimeapi.PromptHistoryPage, error) {
	result := runtimeapi.PromptHistoryPage{Entries: []runtimeapi.PromptHistoryEntry{}}
	if s == nil || strings.TrimSpace(string(input.WorkspaceID)) == "" {
		return result, ErrInvalidSession
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	limit, err := normalizedPageLimit(input.Limit)
	if err != nil {
		return result, ErrQueryFailed
	}

	sessions := make([]promptHistorySession, 0, len(sources))
	seenTargets := make(map[runtimeapi.SessionRef]struct{}, len(sources))
	seenPaths := make(map[string]struct{}, len(sources))
	displays := make(map[string]sessiondisplay.Map)
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !source.Session.Valid() || source.Session.WorkspaceID != input.WorkspaceID {
			return result, ErrInvalidSession
		}
		if _, duplicate := seenTargets[source.Session]; duplicate {
			return result, ErrInvalidSession
		}
		seenTargets[source.Session] = struct{}{}
		dir, path, info, err := validatePromptHistorySource(source)
		if err != nil {
			return result, err
		}
		if _, duplicate := seenPaths[path]; duplicate {
			return result, ErrInvalidSession
		}
		seenPaths[path] = struct{}{}
		mapping, ok := displays[dir]
		if !ok {
			mapping = sessiondisplay.Load(dir)
			displays[dir] = mapping
		}
		resolve := sessiondisplay.ResolverFromMap(mapping, path)
		entries, err := scanPromptHistorySession(ctx, source.Session, path, info, resolve)
		if err != nil {
			return result, err
		}
		sessions = append(sessions, promptHistorySession{
			session: source.Session, activity: promptHistoryActivity(path, info), entries: entries,
		})
	}

	sort.SliceStable(sessions, func(i, j int) bool {
		if sessions[i].activity != sessions[j].activity {
			return sessions[i].activity > sessions[j].activity
		}
		return sessionBinding(sessions[i].session) < sessionBinding(sessions[j].session)
	})
	all := make([]runtimeapi.PromptHistoryEntry, 0)
	revisionParts := make([]string, 0, len(sessions)+2)
	revisionParts = append(revisionParts, "composer/history", string(input.WorkspaceID))
	for _, session := range sessions {
		revisionParts = append(revisionParts, sessionBinding(session.session))
		all = append(all, session.entries...)
	}
	revision := snapshotRevision(all, revisionParts...)
	offset, err := s.promptHistoryOffset(input.Cursor, input.WorkspaceID, revision, len(all))
	if err != nil {
		return result, err
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	result.Entries = append(result.Entries, all[offset:end]...)
	result.HasMore = end < len(all)
	if result.HasMore {
		result.Next = s.encodePromptHistoryCursor(promptHistoryCursor{
			Version: promptHistoryCursorVersion, Method: "composer/history",
			Workspace: string(input.WorkspaceID), Revision: revision, Offset: end,
		})
	}
	return result, nil
}

func validatePromptHistorySource(source PromptHistorySessionSource) (string, string, os.FileInfo, error) {
	dir, err := filepath.Abs(strings.TrimSpace(source.SessionDir))
	if err != nil || strings.TrimSpace(source.SessionDir) == "" {
		return "", "", nil, ErrQueryFailed
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return "", "", nil, sanitizePromptHistoryError(err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil || !dirInfo.IsDir() {
		return "", "", nil, ErrQueryFailed
	}
	path, err := filepath.Abs(strings.TrimSpace(source.SessionPath))
	if err != nil || strings.TrimSpace(source.SessionPath) == "" {
		return "", "", nil, ErrQueryFailed
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", "", nil, sanitizePromptHistoryError(err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", "", nil, ErrQueryFailed
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", nil, sanitizePromptHistoryError(err)
	}
	within, err := filepath.Rel(dir, filepath.Clean(path))
	if err != nil || within == "." || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) || filepath.IsAbs(within) {
		return "", "", nil, ErrQueryFailed
	}
	return filepath.Clean(dir), filepath.Clean(path), info, nil
}

func scanPromptHistorySession(
	ctx context.Context,
	session runtimeapi.SessionRef,
	path string,
	info os.FileInfo,
	resolve func(string) string,
) ([]runtimeapi.PromptHistoryEntry, error) {
	fallback := promptHistoryFallbackMillis(path, info)
	entries := make([]runtimeapi.PromptHistoryEntry, 0)
	emit := func(text string, at int64) {
		text = strings.TrimSpace(resolve(strings.TrimSpace(text)))
		if text == "" || !utf8.ValidString(text) || control.IsSyntheticUserMessage(text) {
			return
		}
		if at < 0 {
			at = 0
		}
		entries = append(entries, runtimeapi.PromptHistoryEntry{
			Text: text, AtMillis: at, Session: session, Turn: len(entries),
		})
	}

	if agent.HasNativeSessionEventLog(path) {
		users, err := agent.LoadSessionUserMessages(path)
		if err != nil {
			return nil, sanitizePromptHistoryError(err)
		}
		for _, user := range users {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			at := fallback
			if !user.At.IsZero() {
				at = user.At.UnixMilli()
			}
			emit(user.Text, at)
		}
	} else {
		file, err := os.Open(path)
		if err != nil {
			return nil, sanitizePromptHistoryError(err)
		}
		decoder := json.NewDecoder(file)
		for {
			if err := ctx.Err(); err != nil {
				_ = file.Close()
				return nil, err
			}
			var record promptHistoryRecord
			if err := decoder.Decode(&record); err != nil {
				_ = file.Close()
				if errors.Is(err, io.EOF) {
					break
				}
				// A crash-torn legacy tail keeps its clean prefix usable, matching
				// the tolerant current event-log replay contract.
				break
			}
			kind := strings.TrimSpace(record.Kind)
			if kind == "" {
				kind = strings.TrimSpace(record.Type)
			}
			text := ""
			if kind == "user.message" {
				text = record.Text
			} else if strings.TrimSpace(record.Role) == "user" {
				text = record.Content
			}
			if strings.TrimSpace(text) == "" {
				continue
			}
			at := fallback
			if parsed, ok := promptHistoryRecordMillis(record); ok {
				at = parsed
			}
			emit(text, at)
		}
		_ = file.Close()
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].AtMillis != entries[j].AtMillis {
			return entries[i].AtMillis > entries[j].AtMillis
		}
		return entries[i].Turn > entries[j].Turn
	})
	return entries, nil
}

func promptHistoryActivity(path string, info os.FileInfo) int64 {
	if meta, ok, err := agent.LoadBranchMeta(path); err == nil && ok && !meta.UpdatedAt.IsZero() {
		return max(meta.UpdatedAt.UnixMilli(), 0)
	}
	if changed := agent.SessionContentModTime(path); !changed.IsZero() {
		return max(changed.UnixMilli(), 0)
	}
	if info != nil {
		return max(info.ModTime().UnixMilli(), 0)
	}
	return 0
}

func promptHistoryFallbackMillis(path string, info os.FileInfo) int64 {
	if meta, ok, err := agent.LoadBranchMeta(path); err == nil && ok && !meta.UpdatedAt.IsZero() {
		return max(meta.UpdatedAt.UnixMilli(), 0)
	}
	if info != nil {
		return max(info.ModTime().UnixMilli(), 0)
	}
	return 0
}

func promptHistoryRecordMillis(record promptHistoryRecord) (int64, bool) {
	for _, raw := range []json.RawMessage{
		record.Time, record.Timestamp, record.CreatedAt, record.CreatedAtSnake,
		record.UpdatedAt, record.UpdatedAtSnake,
	} {
		if value, ok := parsePromptHistoryMillis(raw); ok {
			return value, true
		}
	}
	return 0, false
}

func parsePromptHistoryMillis(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, false
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		text = strings.TrimSpace(text)
		if value, err := strconv.ParseInt(text, 10, 64); err == nil {
			return normalizePromptHistoryMillis(float64(value))
		}
		if value, err := strconv.ParseFloat(text, 64); err == nil {
			return normalizePromptHistoryMillis(value)
		}
		if value, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return max(value.UnixMilli(), 0), true
		}
		return 0, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err != nil {
		return 0, false
	}
	value, err := strconv.ParseFloat(number.String(), 64)
	if err != nil {
		return 0, false
	}
	return normalizePromptHistoryMillis(value)
}

func normalizePromptHistoryMillis(value float64) (int64, bool) {
	if value <= 0 {
		return 0, false
	}
	switch {
	case value >= 1_000_000_000_000_000_000:
		return int64(value / 1_000_000), true
	case value >= 1_000_000_000_000_000:
		return int64(value / 1_000), true
	case value >= 100_000_000_000:
		return int64(value), true
	case value >= 1_000_000_000:
		return int64(value * 1_000), true
	default:
		return 0, false
	}
}

func (s *PromptHistoryService) encodePromptHistoryCursor(payload promptHistoryCursor) runtimeapi.Cursor {
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, s.cursorKey[:])
	_, _ = mac.Write(raw)
	encoded := append(append(make([]byte, 0, len(raw)+sha256.Size), raw...), mac.Sum(nil)...)
	return runtimeapi.Cursor(base64.RawURLEncoding.EncodeToString(encoded))
}

func (s *PromptHistoryService) decodePromptHistoryCursor(cursor runtimeapi.Cursor) (promptHistoryCursor, error) {
	var payload promptHistoryCursor
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
	if err := json.Unmarshal(message, &payload); err != nil || payload.Version != promptHistoryCursorVersion ||
		payload.Method != "composer/history" || strings.TrimSpace(payload.Workspace) == "" || payload.Offset < 0 {
		return promptHistoryCursor{}, ErrInvalidCursor
	}
	return payload, nil
}

func (s *PromptHistoryService) promptHistoryOffset(cursor runtimeapi.Cursor, workspace runtimeapi.WorkspaceID, revision string, length int) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	payload, err := s.decodePromptHistoryCursor(cursor)
	if err != nil {
		return 0, err
	}
	if payload.Workspace != string(workspace) {
		return 0, ErrInvalidCursor
	}
	if payload.Revision != revision || payload.Offset > length {
		return 0, ErrStaleCursor
	}
	return payload.Offset, nil
}

func sanitizePromptHistoryError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return ErrQueryFailed
}
