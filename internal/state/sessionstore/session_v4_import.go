package sessionstore

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionv4"
	"reasonix/internal/state/store"
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

// v4DesktopIDPrefixes are the markers 1.x Desktop puts before the hex digits of
// a conversation it created itself; the longer one is tried first.
var v4DesktopIDPrefixes = []string{"desktop-manual-", "desktop-"}

// isV4SessionID admits the lowercase hex ids 1.x generates (32 digits in
// sessions-v4, 24 in Desktop's by-id store), with or without a Desktop prefix,
// which are safe as a file name.
func isV4SessionID(id string) bool {
	for _, prefix := range v4DesktopIDPrefixes {
		if rest, ok := strings.CutPrefix(id, prefix); ok {
			id = rest
			break
		}
	}
	if len(id) < 16 || len(id) > 64 {
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
	if v4RemovalPath(path) != "" && !IsVisibleSession(path) {
		return &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}
	s, ok := v4SessionFor(path)
	if !ok {
		return nil
	}
	if !sessionArtifactExists(path) {
		return importV4(path, s)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil {
		return nil
	}
	if !ok || meta.ImportedFrom == nil {
		// The transcript landed but the sidecar that records what the
		// conversation means did not: finish it before the copy is used.
		_, err := completeV4Import(path, s, BranchMeta{})
		return err
	}
	if meta.ImportedFrom.LogSize == s.LogSize {
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
	// This path refreshes a copy that already exists, so its meta already carries
	// whatever the first import decided — including the archived mark. Passing an
	// empty start is what keeps that decision rather than resetting it.
	return saveImportMeta(path, s, t, BranchMeta{}, t.Messages)
}

func importV4(path string, s sessionv4.Session) error {
	return importV4As(path, s, BranchMeta{})
}

// importV4As copies one conversation and starts its meta from the caller's, which
// carries what the transcript cannot say — that 1.x had filed it away, for one.
func importV4As(path string, s sessionv4.Session, start BranchMeta) error {
	t, err := s.Transcript()
	if err != nil {
		return fmt.Errorf("import 1.x session %s: %w", s.Manifest.SessionID, err)
	}
	sess := NewSession("")
	sess.Messages = t.Messages
	if err := sess.SaveIfAbsent(path); err != nil && !os.IsExist(err) {
		return fmt.Errorf("import 1.x session %s: %w", s.Manifest.SessionID, err)
	}
	return saveImportMeta(path, s, t, start, t.Messages)
}

// importedTitle is the name 1.x showed for the conversation: the title its log
// recorded, else the one its listing cache kept. The list of unimported 1.x
// conversations reads both, so an import that read only the log named the copy
// after its file.
func importedTitle(s sessionv4.Session, t sessionv4.Transcript) string {
	if t.Title != "" {
		return t.Title
	}
	if listing, ok := s.CachedListing(); ok {
		return listing.Title
	}
	return ""
}

// healImportedTitle gives an earlier import the title it should have had. Only
// an empty title is filled; one the person set stays.
func healImportedTitle(path string, s sessionv4.Session) {
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok || meta.ImportedFrom == nil || meta.CustomTitle != "" {
		return
	}
	listing, cached := s.CachedListing()
	if !cached || listing.Title == "" {
		return
	}
	meta.CustomTitle = listing.Title
	_ = SaveBranchMeta(path, meta)
}

func saveImportMeta(path string, s sessionv4.Session, t sessionv4.Transcript, start BranchMeta, content []provider.Message) error {
	digest, err := DigestSessionMessages(t.Messages)
	if err != nil {
		return err
	}
	// content is what the copy holds, which is the source's transcript on a
	// first import and may be a superset of it on a completed interrupted one.
	preview, turns := SessionPreviewFromMessages(content)
	meta, _, _ := LoadBranchMeta(path)
	meta.ID = BranchID(path)
	meta.CreatedAt = s.Manifest.CreatedAt
	meta.UpdatedAt = s.UpdatedAt()
	meta.Preview, meta.Turns, meta.SchemaVersion = preview, turns, BranchMetaCountsVersion
	meta.Model = t.ModelRef
	if title := importedTitle(s, t); title != "" {
		meta.CustomTitle = title
	}
	// The caller's starting meta carries what the transcript cannot say: 1.x kept
	// its archive list outside the session store, so a conversation filed away
	// there has to arrive already filed away.
	meta.Archived = meta.Archived || start.Archived
	meta.Pinned = meta.Pinned || start.Pinned
	meta.ImportedFrom = &ImportSource{Line: importLineV4, LogSize: s.LogSize, Digest: hex.EncodeToString(digest[:])}
	return SaveBranchMeta(path, meta)
}

// completeV4Import finishes a copy an interrupted run left behind: the transcript
// landed, but the sidecar that carries what it cannot say — archive mark, title,
// pin, and source — did not. A copy with a sidecar is done and is left alone;
// one without is completed here. It reports whether it wrote the sidecar.
func completeV4Import(path string, s sessionv4.Session, start BranchMeta) (bool, error) {
	meta, ok, err := LoadBranchMeta(path)
	if err != nil {
		// A sidecar that exists but cannot be read is not one to rewrite on an
		// import pass; leave it for the recovery path.
		return false, nil
	}
	if ok && meta.ImportedFrom != nil {
		healImportedTitle(path, s)
		return false, nil
	}
	t, err := s.Transcript()
	if err != nil {
		return false, fmt.Errorf("import 1.x session %s: %w", s.Manifest.SessionID, err)
	}
	if _, turns := SessionPreviewFromMessages(t.Messages); turns == 0 {
		// Nothing to bring over; writing counts derived from an empty source
		// would make a copy that does have turns look empty.
		return false, nil
	}
	content, err := absorbInterruptedCopy(path, t)
	if err != nil {
		return false, fmt.Errorf("import 1.x session %s: %w", s.Manifest.SessionID, err)
	}
	if err := saveImportMeta(path, s, t, start, content); err != nil {
		return false, fmt.Errorf("import 1.x session %s: %w", s.Manifest.SessionID, err)
	}
	return true, nil
}

// absorbInterruptedCopy brings an interrupted import's copy up to the source
// while it is still only a prefix of it. A copy the person continued in 2.x is
// left as it is: its extra turns are not in 1.x, and replaying over it would
// lose them. It returns the messages the copy holds afterwards, so the sidecar
// describes the conversation that exists rather than the one in the source.
func absorbInterruptedCopy(path string, t sessionv4.Transcript) ([]provider.Message, error) {
	// sessionArtifactExists is the store's reader for this question. It answers
	// bool, so missing and unreadable look alike; a copy that is there but
	// cannot be read still fails when it is loaded below.
	if !sessionArtifactExists(path) {
		sess := NewSession("")
		sess.Messages = t.Messages
		if err := sess.SaveIfAbsent(path); err != nil && !os.IsExist(err) {
			return nil, err
		}
		return t.Messages, nil
	}
	current, err := loadSessionUnlocked(path)
	if err != nil {
		return nil, err
	}
	copied := current.Snapshot()
	if len(copied) >= len(t.Messages) || !isMessagePrefixOf(copied, t.Messages) {
		return copied, nil
	}
	current.Rewrite(t.Messages, "complete an interrupted import")
	if err := current.SaveRewrite(path); err != nil {
		return nil, err
	}
	return t.Messages, nil
}

// isMessagePrefixOf reports whether copied is exactly the first len(copied)
// messages of full, using the same digest the import records for its source.
func isMessagePrefixOf(copied, full []provider.Message) bool {
	if len(copied) > len(full) {
		return false
	}
	have, err := DigestSessionMessages(copied)
	if err != nil {
		return false
	}
	want, err := DigestSessionMessages(full[:len(copied)])
	return err == nil && have == want
}

// v4CopyBegun reports whether an import has already put something of its own at
// path: the transcript or any sidecar that is not a rebuildable index.
func v4CopyBegun(path string) bool {
	return sessionArtifactExistsExcept(path, store.SessionEventIndex(path), store.SessionDisplayIndex(path))
}

// removeOrphanIndexes drops the indexes of a transcript that is not there, so a
// fresh copy is not mistaken for one already begun and the stale index is not
// read as the new transcript's.
func removeOrphanIndexes(path string) {
	for _, index := range []string{store.SessionEventIndex(path), store.SessionDisplayIndex(path)} {
		if err := os.Remove(index); err != nil && !os.IsNotExist(err) {
			slog.Warn("session: stale index not removed before import", "path", index, "err", err)
		}
	}
}

// withV4Sessions adds the 1.x v4 conversations beside dir that have no import
// yet; an imported one is already in the list as its own transcript.
func withV4Sessions(dir string, out []SessionInfo) []SessionInfo {
	root := v4RootFor(dir)
	if root == "" {
		return out
	}
	r := resolverFor(dir)
	defer r.flush(context.Background(), dir, false)
	for _, source := range sessionv4.List(root) {
		id := source.Manifest.SessionID
		if !isV4SessionID(id) {
			continue
		}
		path := v4ImportPath(dir, id)
		if !IsVisibleSession(path) || v4CopyBegun(path) {
			continue
		}
		listing, err := r.resolve(context.Background(), dir, source, readLegacyTranscript)
		if err != nil || listing.Turns == 0 {
			continue
		}
		out = append(out, legacySessionInfo(dir, source, listing, true))
	}
	sortLegacyRows(out)
	return out
}

// V4Origin is what a source conversation says about where it was made: its id
// and the working folder its header records, "" when it records none.
type V4Origin struct {
	ID  string
	Cwd string
}

// V4Placement is a route's answer. Dir is the project sessions directory the
// conversation belongs in. An empty Dir with Known set means its origin is known
// but no folder answers for it any more (the project was deleted), so it falls
// back to dir. Known unset means it names no project at all.
type V4Placement struct {
	Dir   string
	Known bool
}

// readV4Cwd is the working folder a 1.x Desktop conversation's header records.
func readV4Cwd(store fs.FS, name string) string {
	raw, err := fs.ReadFile(store, path.Join(name, "header.json"))
	if err != nil {
		return ""
	}
	var header struct {
		Cwd string `json:"cwd"`
	}
	if json.Unmarshal(raw, &header) != nil {
		return ""
	}
	return strings.TrimSpace(header.Cwd)
}

// ImportV4From copies the 1.x v4 conversations under store into dir. With a route
// each goes where it is placed, and one naming no project is skipped as SkipNoProject;
// without one everything goes to dir. A finished copy is left alone, an
// interrupted one is completed, an empty one is skipped silently, an unreadable
// one becomes a SkippedSession, and importedAs supplies each copy's meta.
func ImportV4From(store fs.FS, dir string, route func(V4Origin) V4Placement, importedAs func(sessionID string) BranchMeta) (int, []SkippedSession, error) {
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
		if !isV4SessionID(id) {
			skip(e.Name(), SkipUnreadableFormat)
			continue
		}
		target := dir
		if route != nil {
			placed := route(V4Origin{ID: id, Cwd: readV4Cwd(store, e.Name())})
			switch {
			case placed.Dir != "":
				target = placed.Dir
			case !placed.Known:
				skip(e.Name(), SkipNoProject)
				continue
			}
		}
		path := v4ImportPath(target, id)
		if !v4CopyBegun(path) {
			removeOrphanIndexes(path)
		}
		if sessionArtifactExists(path) {
			start := BranchMeta{}
			if importedAs != nil {
				start = importedAs(id)
			}
			completed, err := completeV4Import(path, s, start)
			if err != nil {
				skip(e.Name(), classifyWriteSkip(err))
				continue
			}
			if completed {
				imported++
			}
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
		start := BranchMeta{}
		if importedAs != nil {
			start = importedAs(id)
		}
		if err := importV4As(path, s, start); err != nil {
			skip(e.Name(), classifyWriteSkip(err))
			continue
		}
		imported++
	}
	return imported, skipped, nil
}
