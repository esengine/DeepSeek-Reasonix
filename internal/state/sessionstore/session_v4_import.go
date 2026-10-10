package sessionstore

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/state/sessionv4"
)

// ImportSource records that a session was brought over from a store this
// build does not write, and what it held then: a later import compares both
// to tell whether 1.x moved on and whether this copy has since been continued.
type ImportSource struct {
	Line    string `json:"line"`
	LogSize int64  `json:"log_size"`
	Digest  string `json:"digest"`
}

const importLineV4 = "1.x sessions-v4"

// v4RootFor is the sessions-v4 root 1.x keeps beside a project's sessions
// directory, or "" for any other directory.
func v4RootFor(sessionsDir string) string {
	if filepath.Base(sessionsDir) != "sessions" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionsDir), "sessions-v4")
}

// v4ImportPrefix marks a transcript imported from a 1.x v4 session. The bare
// id is not usable as the name: 1.x reads sessions-v4/<name> as the mirror of
// a sessions/<name>.jsonl transcript and takes the transcript as the authority.
const v4ImportPrefix = "v4-"

// v4ImportPath is where a 1.x v4 conversation lives once imported.
func v4ImportPath(sessionsDir, id string) string {
	return filepath.Join(sessionsDir, v4ImportPrefix+id+".jsonl")
}

func isV4SessionID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil && strings.ToLower(id) == id
}

// v4SessionFor is the 1.x v4 session a transcript path names, if any.
func v4SessionFor(path string) (sessionv4.Session, bool) {
	id, ok := strings.CutPrefix(strings.TrimSuffix(filepath.Base(path), ".jsonl"), v4ImportPrefix)
	root := v4RootFor(filepath.Dir(path))
	if !ok || root == "" || !isV4SessionID(id) {
		return sessionv4.Session{}, false
	}
	s, err := sessionv4.Open(filepath.Join(root, id))
	return s, err == nil
}

// PrepareSessionPath makes a path a session list offered openable: a 1.x v4
// conversation is imported there on first open, and brought up to date when
// 1.x has continued it and this copy has not.
func PrepareSessionPath(path string) error {
	s, ok := v4SessionFor(path)
	if !ok {
		return nil
	}
	if !sessionArtifactExists(path) {
		return importV4(path, s)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok || meta.ImportedFrom == nil || meta.ImportedFrom.LogSize == s.LogSize {
		return nil
	}
	current, err := loadSessionUnlocked(path)
	if err != nil {
		return nil
	}
	if digest, err := DigestSessionMessages(current.Snapshot()); err != nil || hex.EncodeToString(digest[:]) != meta.ImportedFrom.Digest {
		return nil
	}
	t, err := s.Transcript()
	if err != nil {
		return err
	}
	current.Rewrite(t.Messages, "import from 1.x")
	if err := current.SaveRewrite(path); err != nil {
		return fmt.Errorf("refresh 1.x session: %w", err)
	}
	return saveImportMeta(path, s, t)
}

func importV4(path string, s sessionv4.Session) error {
	t, err := s.Transcript()
	if err != nil {
		return fmt.Errorf("import 1.x session %s: %w", s.Manifest.SessionID, err)
	}
	sess := NewSession("")
	sess.Messages = t.Messages
	if err := sess.SaveIfAbsent(path); err != nil && !os.IsExist(err) {
		return fmt.Errorf("import 1.x session %s: %w", s.Manifest.SessionID, err)
	}
	return saveImportMeta(path, s, t)
}

func saveImportMeta(path string, s sessionv4.Session, t sessionv4.Transcript) error {
	digest, err := DigestSessionMessages(t.Messages)
	if err != nil {
		return err
	}
	preview, turns := SessionPreviewFromMessages(t.Messages)
	meta, _, _ := LoadBranchMeta(path)
	meta.ID = BranchID(path)
	meta.CreatedAt = s.Manifest.CreatedAt
	meta.UpdatedAt = s.UpdatedAt()
	meta.Preview, meta.Turns, meta.SchemaVersion = preview, turns, BranchMetaCountsVersion
	meta.Model = t.ModelRef
	if t.Title != "" {
		meta.CustomTitle = t.Title
	}
	meta.ImportedFrom = &ImportSource{Line: importLineV4, LogSize: s.LogSize, Digest: hex.EncodeToString(digest[:])}
	return SaveBranchMeta(path, meta)
}

// withV4Sessions adds the 1.x v4 conversations beside dir that have no import
// yet; an imported one is already in the list as its own transcript.
func withV4Sessions(dir string, out []SessionInfo) []SessionInfo {
	root := v4RootFor(dir)
	if root == "" {
		return out
	}
	added := false
	for _, s := range sessionv4.List(root) {
		id := s.Manifest.SessionID
		path := v4ImportPath(dir, id)
		if !isV4SessionID(id) || sessionArtifactExists(path) {
			continue
		}
		listing, ok := s.CachedListing()
		if !ok {
			t, err := s.Transcript()
			if err != nil {
				continue
			}
			listing.Title, listing.ModelRef = t.Title, t.ModelRef
			listing.Preview, listing.Turns = SessionPreviewFromMessages(t.Messages)
		}
		if listing.Turns == 0 {
			continue
		}
		out = append(out, SessionInfo{
			Path: path, CreatedAt: s.Manifest.CreatedAt, LastActivityAt: s.UpdatedAt(), ModTime: s.UpdatedAt(),
			Preview: listing.Preview, Turns: listing.Turns, CountsKnown: true, CustomTitle: listing.Title,
		})
		added = true
	}
	if added {
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].LastActivityAt.Equal(out[j].LastActivityAt) {
				return out[i].Path < out[j].Path
			}
			return out[i].LastActivityAt.After(out[j].LastActivityAt)
		})
	}
	return out
}

// ImportV4From copies the 1.x v4 conversations directly under store into dir
// as transcripts, for a store that does not sit beside dir. A non-empty
// route(sessionID) names the directory a conversation goes to instead of dir. A conversation already
// imported, or one with no turns, is skipped silently; one that cannot be read
// is returned as a SkippedSession (Path relative to store) and does not stop the rest.
func ImportV4From(store fs.FS, dir string, route func(sessionID string) string) (int, []SkippedSession, error) {
	entries, err := fs.ReadDir(store, ".")
	if err != nil {
		return 0, nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, nil, err
	}
	imported := 0
	var skipped []SkippedSession
	skip := func(name string, reason SkipReason) {
		skipped = append(skipped, SkippedSession{Name: name, Path: name, Reason: reason})
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		s, err := sessionv4.OpenIn(store, e.Name())
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			skip(e.Name(), classifySkip(err))
			continue
		}
		id := s.Manifest.SessionID
		target := dir
		if route != nil {
			if routed := route(id); routed != "" {
				target = routed
			}
		}
		path := v4ImportPath(target, id)
		if !isV4SessionID(id) || sessionArtifactExists(path) {
			continue
		}
		t, err := s.Transcript()
		if err != nil {
			skip(e.Name(), classifySkip(err))
			continue
		}
		if _, turns := SessionPreviewFromMessages(t.Messages); turns == 0 {
			continue
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			skip(e.Name(), classifyWriteSkip(err))
			continue
		}
		if err := importV4(path, s); err != nil {
			skip(e.Name(), classifyWriteSkip(err))
			continue
		}
		imported++
	}
	return imported, skipped, nil
}
