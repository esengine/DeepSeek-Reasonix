package sessionstore

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

// SessionVersion is a conversation as it stood before a rewind replaced part of
// it. It is a whole session file and opens like one; it only stays out of the
// session lists, where it would read as a second conversation.
type SessionVersion struct {
	Path    string
	Turns   int
	Preview string
	SavedAt time.Time
}

// SaveSupersededVersion keeps msgs, the conversation a rewind is about to cut,
// as a version of the session at parentPath, and returns where it went.
func SaveSupersededVersion(parentPath string, msgs []provider.Message) (string, error) {
	sess := NewSession("")
	sess.Messages = slices.Clone(msgs)
	path := NewSessionPath(filepath.Dir(parentPath), "version")
	if err := sess.SaveIfAbsent(path); err != nil {
		return "", err
	}
	preview, turns := SessionPreviewFromMessages(msgs)
	meta := BranchMeta{ParentID: BranchID(parentPath), Superseded: true, Preview: preview, Turns: turns,
		SchemaVersion: BranchMetaCountsVersion, CreatedAt: time.Now().UTC()}
	if parent, ok, err := LoadBranchMeta(parentPath); err == nil && ok {
		meta.Scope, meta.WorkspaceRoot, meta.Model = parent.Scope, parent.WorkspaceRoot, parent.Model
		meta.AgentPreset, meta.ToolApprovalMode = parent.AgentPreset, parent.ToolApprovalMode
	}
	if err := SaveBranchMeta(path, meta); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

// ListSessionVersions groups the versions under dir by the conversation they
// were cut from, newest first. A missing directory is not an error.
func ListSessionVersions(dir string) (map[string][]SessionVersion, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := map[string][]SessionVersion{}
	for _, e := range entries {
		if e.IsDir() || !store.IsSessionTranscriptName(e.Name()) {
			continue
		}
		full := filepath.Join(dir, e.Name())
		meta, ok, err := LoadBranchMeta(full)
		if err != nil || !ok || !meta.Superseded || meta.ParentID == "" || !IsVisibleSession(full) {
			continue
		}
		out[meta.ParentID] = append(out[meta.ParentID], SessionVersion{
			Path: full, Turns: meta.Turns, Preview: meta.Preview, SavedAt: meta.CreatedAt,
		})
	}
	for _, versions := range out {
		sort.Slice(versions, func(i, j int) bool { return versions[i].SavedAt.After(versions[j].SavedAt) })
	}
	return out, nil
}

// SessionVersionPaths are the versions cut from the session at parentPath,
// for a caller removing it: a version nobody lists must not outlive its parent.
func SessionVersionPaths(parentPath string) []string {
	byParent, _ := ListSessionVersions(filepath.Dir(parentPath))
	var out []string
	for _, v := range byParent[BranchID(parentPath)] {
		out = append(out, v.Path)
	}
	return out
}

// DropVersionMatching removes the newest version of the session at parentPath
// when it holds exactly msgs: undoing a rewind puts back the conversation that
// rewind kept, and leaving the copy would show one conversation twice.
func DropVersionMatching(parentPath string, msgs []provider.Message) error {
	versions := SessionVersionPaths(parentPath)
	if len(versions) == 0 {
		return nil
	}
	newest := versions[0]
	kept, err := LoadSession(newest)
	if err != nil || kept == nil || !sameMessages(kept.Messages, msgs) {
		return err
	}
	return store.RemoveSessionArtifacts(newest)
}

func sameMessages(a, b []provider.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Role != b[i].Role || a[i].Content != b[i].Content || len(a[i].ToolCalls) != len(b[i].ToolCalls) {
			return false
		}
	}
	return true
}
