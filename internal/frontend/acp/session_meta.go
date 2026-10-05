package acp

// The ACP sidecar that remembers a session between connections: where it lives,
// what it records, and the title a listing shows for it.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"
	"time"

	"reasonix/internal/base/fileutil"
	fileencoding "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/contract/provider"
)

func (s *acpSession) saveMetaIfPresent() {
	s.mu.Lock()
	path := s.transcript
	meta := s.metaLocked()
	s.mu.Unlock()
	if path != "" && sessionFileExists(path) {
		_ = saveACPMeta(path, meta)
	}
}

func (s *service) loadMeta(id string) (acpSessionMeta, bool) {
	dir := s.sessionDir()
	if dir == "" {
		return acpSessionMeta{}, false
	}
	meta, ok, err := loadACPMeta(resolveTranscriptPath(dir, id))
	if err != nil {
		return acpSessionMeta{}, false
	}
	return meta, ok
}

func (s *acpSession) meta() acpSessionMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.metaLocked()
}

func (s *acpSession) metaLocked() acpSessionMeta {
	return acpSessionMeta{
		SessionID:         s.id,
		Cwd:               s.cwd,
		Model:             s.model,
		EffortOverride:    cloneStringPtr(s.effortOverride),
		RuntimeProfile:    s.runtimeProfile,
		ToolApprovalMode:  normalizeACPToolApprovalMode(s.toolApprovalMode),
		CollaborationMode: normalizeACPCollaborationMode(s.modeID),
		Title:             s.title,
		CreatedAt:         s.createdAt,
		UpdatedAt:         s.updatedAt,
		Status:            s.status.persisted(),
	}
}

func metadataForLoadedSession(path, id, cwd string, history []provider.Message) acpSessionMeta {
	now := time.Now().UTC()
	meta, ok, err := loadACPMeta(path)
	if err != nil || !ok {
		meta = acpSessionMeta{
			SessionID: id,
			Cwd:       cwd,
			Title:     titleFromHistory(history),
			CreatedAt: now,
			UpdatedAt: now,
		}
		if info, statErr := os.Stat(path); statErr == nil {
			meta.CreatedAt = info.ModTime().UTC()
			meta.UpdatedAt = info.ModTime().UTC()
		}
	}
	if meta.SessionID == "" {
		meta.SessionID = id
	}
	if cwd != "" {
		meta.Cwd = cwd
	}
	if meta.Title == "" {
		meta.Title = titleFromHistory(history)
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	if meta.UpdatedAt.IsZero() {
		meta.UpdatedAt = meta.CreatedAt
	}
	return meta
}

func loadACPMeta(sessionPath string) (acpSessionMeta, bool, error) {
	path := acpMetaPath(sessionPath)
	if path == "" {
		return acpSessionMeta{}, false, nil
	}
	b, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		if os.IsNotExist(err) {
			return acpSessionMeta{}, false, nil
		}
		return acpSessionMeta{}, false, err
	}
	var meta acpSessionMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return acpSessionMeta{}, false, fmt.Errorf("decode ACP session metadata %s: %w", path, err)
	}
	return meta, true, nil
}

func saveACPMeta(sessionPath string, meta acpSessionMeta) error {
	path := acpMetaPath(sessionPath)
	if path == "" {
		return nil
	}
	now := time.Now().UTC()
	if meta.SessionID == "" {
		meta.SessionID = sessionIDFromTranscript(sessionPath)
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	if meta.UpdatedAt.IsZero() {
		meta.UpdatedAt = meta.CreatedAt
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".acp-session.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return fileutil.ReplaceFile(tmpPath, path)
}

func listACPMetas(dir string) ([]acpSessionMeta, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := []acpSessionMeta{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".acp.json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".acp.json")
		sessionPath := transcriptPath(dir, id)
		if sessionstore.IsCleanupPending(sessionPath) {
			continue
		}
		if !sessionFileExists(sessionPath) {
			continue
		}
		meta, ok, err := loadACPMeta(sessionPath)
		if err != nil || !ok {
			continue
		}
		if meta.SessionID == "" {
			meta.SessionID = id
		}
		if meta.Cwd == "" {
			continue
		}
		out = append(out, meta)
	}
	return out, nil
}

func acpMetaPath(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return strings.TrimSuffix(sessionPath, filepath.Ext(sessionPath)) + ".acp.json"
}

// listMetaBeats reports whether a should represent its session id in
// session/list over b. A meta without an ActiveTranscript redirect is the
// session's live transcript and always beats a redirect sidecar; between two
// of the same kind the later UpdatedAt wins.
func listMetaBeats(a, b acpSessionMeta) bool {
	aRedirect := strings.TrimSpace(a.ActiveTranscript) != ""
	bRedirect := strings.TrimSpace(b.ActiveTranscript) != ""
	if aRedirect != bRedirect {
		return !aRedirect
	}
	return a.UpdatedAt.After(b.UpdatedAt)
}

func titleFromHistory(history []provider.Message) string {
	for _, m := range history {
		if m.Role == provider.RoleUser {
			if title := previewTitle(m.Content); title != "" {
				return title
			}
		}
	}
	return ""
}

func previewTitle(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len([]rune(text)) <= 80 {
		return text
	}
	runes := []rune(text)
	return string(runes[:77]) + "..."
}

func parseSessionUpdatedAt(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
