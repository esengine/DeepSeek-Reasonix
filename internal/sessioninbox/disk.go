package sessioninbox

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/fileutil"
	"reasonix/internal/store"
)

func (s *Store) mutableLocked() error {
	if s.closed {
		return ErrClosed
	}
	if s.readonly {
		return ErrSchemaReadonly
	}
	return nil
}

func (s *Store) blobPath(blobName string) (string, error) {
	if !validBlobStem(blobName) {
		return "", fmt.Errorf("sessioninbox: invalid blob name")
	}
	base := filepath.Join(s.dir, blobsDirName)
	path := filepath.Join(base, blobName+blobSuffix)
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("sessioninbox: blob path escapes inbox")
	}
	return path, nil
}

// blobNameFor returns the on-disk blob stem for a meta entry.
func blobNameFor(meta InboxItemMeta) string {
	if name := strings.TrimSpace(meta.BlobName); name != "" {
		return name
	}
	return meta.ID
}

func (s *Store) writeBlobLocked(blobName string, data []byte) error {
	if err := ensurePrivateDir(s.dir); err != nil {
		return err
	}
	if err := ensurePrivateDir(filepath.Join(s.dir, blobsDirName)); err != nil {
		return fmt.Errorf("sessioninbox: blobs dir: %w", err)
	}
	path, err := s.blobPath(blobName)
	if err != nil {
		return err
	}
	fileutil.Crash("inbox-blob-write", path)
	if err := fileutil.AtomicWriteFileStrict(path, data, 0o600); err != nil {
		return fmt.Errorf("sessioninbox: write blob: %w", err)
	}
	fileutil.Crash("inbox-blob-rename", path)
	return nil
}

func (s *Store) readBlobLocked(blobName, wantChecksum string) (PromptEnvelope, error) {
	if err := validatePrivateDir(filepath.Join(s.dir, blobsDirName)); err != nil {
		return PromptEnvelope{}, fmt.Errorf("sessioninbox: blobs dir: %w", err)
	}
	path, err := s.blobPath(blobName)
	if err != nil {
		return PromptEnvelope{}, err
	}
	data, err := readRegularFile(path, s.limits.MaxItemBytes)
	if err != nil {
		return PromptEnvelope{}, fmt.Errorf("sessioninbox: read blob: %w", err)
	}
	got := sha256Hex(data)
	if wantChecksum != "" && got != wantChecksum {
		return PromptEnvelope{}, fmt.Errorf("sessioninbox: blob checksum mismatch")
	}
	var env PromptEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return PromptEnvelope{}, fmt.Errorf("sessioninbox: decode blob: %w", err)
	}
	return env, nil
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return validatePrivateDir(path)
}

func validatePrivateDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("refusing non-directory or symlink")
	}
	return nil
}

func readRegularFile(path string, maxBytes int64) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing non-regular file")
	}
	if maxBytes > 0 && before.Size() > maxBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", maxBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, fmt.Errorf("file changed while opening")
	}
	reader := io.Reader(f)
	if maxBytes > 0 {
		reader = io.LimitReader(f, maxBytes+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", maxBytes)
	}
	return data, nil
}

func (s *Store) commitManifestLocked(next *manifest) error {
	if next == nil {
		return fmt.Errorf("sessioninbox: nil manifest")
	}
	if err := validateManifest(next, false); err != nil {
		return fmt.Errorf("sessioninbox: invalid manifest: %w", err)
	}
	if err := ensurePrivateDir(s.dir); err != nil {
		return fmt.Errorf("sessioninbox: mkdir: %w", err)
	}
	next.SchemaVersion = SchemaVersion
	next.RunID = s.runID
	next.Revision++
	next.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(s.dir, manifestName)
	fileutil.Crash("inbox-manifest-write", path)
	if err := fileutil.AtomicWriteFileStrict(path, data, 0o600); err != nil {
		return fmt.Errorf("sessioninbox: write manifest: %w", err)
	}
	fileutil.Crash("inbox-manifest-commit", path)
	// Best-effort directory fsync for durability of the rename.
	if d, err := os.Open(s.dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	s.man = next
	return nil
}

func (s *Store) quarantineFileLocked(path, tag string) error {
	qdir := filepath.Join(s.dir, quarantineName)
	if err := ensurePrivateDir(qdir); err != nil {
		return err
	}
	base := filepath.Base(path) + "." + tag + "." + fmt.Sprintf("%d", time.Now().UnixNano())
	return os.Rename(path, filepath.Join(qdir, base))
}

func (s *Store) gcOrphansLocked() {
	bdir := filepath.Join(s.dir, blobsDirName)
	if err := validatePrivateDir(bdir); err != nil {
		return
	}
	entries, err := os.ReadDir(bdir)
	if err != nil {
		return
	}
	live := make(map[string]struct{}, len(s.man.Items))
	for _, it := range s.man.Items {
		live[blobNameFor(it)] = struct{}{}
	}
	qdir := filepath.Join(s.dir, quarantineName)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, blobSuffix) {
			_ = s.quarantineUnknownLocked(filepath.Join(bdir, name))
			continue
		}
		stem := strings.TrimSuffix(name, blobSuffix)
		if _, ok := live[stem]; ok {
			continue
		}
		// Orphan blob → quarantine (do not delete silently: crash recovery).
		_ = ensurePrivateDir(qdir)
		_ = os.Rename(filepath.Join(bdir, name), filepath.Join(qdir, name+"."+fmt.Sprintf("%d", time.Now().UnixNano())))
	}
}

// salvageOrphanBlobsLocked rebuilds uncertain meta rows from blob files after a
// corrupt-manifest quarantine. Bodies stay on disk; the user reviews before resume.
func (s *Store) salvageOrphanBlobsLocked() []InboxItemMeta {
	bdir := filepath.Join(s.dir, blobsDirName)
	if err := validatePrivateDir(bdir); err != nil {
		return nil
	}
	entries, err := os.ReadDir(bdir)
	if err != nil {
		return nil
	}
	now := time.Now().UTC()
	var out []InboxItemMeta
	for _, e := range entries {
		if e.IsDir() || e.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(e.Name(), blobSuffix) {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), blobSuffix)
		if !validBlobStem(stem) {
			continue
		}
		data, err := readRegularFile(filepath.Join(bdir, e.Name()), s.limits.MaxItemBytes)
		if err != nil {
			continue
		}
		var env PromptEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		preview := PreviewText(firstNonEmpty(env.DisplayText, env.SubmitText, env.RawText), DefaultPreviewRunes)
		if preview == "" {
			preview = "(salvaged body)"
		}
		// The corrupt manifest no longer provides a revision-to-item mapping.
		// Use the complete blob stem as a collision-free recovered item ID.
		itemID := stem
		out = append(out, InboxItemMeta{
			ID:          itemID,
			Intent:      IntentFollowup,
			State:       StateUncertain,
			BlobName:    stem,
			CreatedAt:   now,
			UpdatedAt:   now,
			Preview:     preview,
			ByteSize:    int64(len(data)),
			Checksum:    sha256Hex(data),
			RunID:       s.runID,
			BlockReason: "salvaged after corrupt manifest",
		})
	}
	return out
}

func (s *Store) quarantineUnknownLocked(path string) error {
	qdir := filepath.Join(s.dir, quarantineName)
	if err := ensurePrivateDir(qdir); err != nil {
		return err
	}
	return os.Rename(path, filepath.Join(qdir, filepath.Base(path)+"."+fmt.Sprintf("%d", time.Now().UnixNano())))
}

func (s *Store) notifyLocked(snap InboxSnapshot) {
	listeners := append([]func(InboxSnapshot){}, s.listeners...)
	// Unlock is held; notify asynchronously so listeners can re-enter.
	go func() {
		for _, fn := range listeners {
			fn(snap)
		}
	}()
}

func encodeEnvelope(env PromptEnvelope) (data []byte, checksum string, size int64, err error) {
	data, err = json.Marshal(env)
	if err != nil {
		return nil, "", 0, err
	}
	return data, sha256Hex(data), int64(len(data)), nil
}

// idempotencyRequestHash fingerprints stable client intent. Enqueue-time
// reference materialization is deliberately excluded so a network retry does
// not conflict merely because the referenced workspace changed meanwhile.
func idempotencyRequestHash(env PromptEnvelope) (string, error) {
	type stableInvocation struct {
		Name    string            `json:"name,omitempty"`
		Args    map[string]string `json:"args,omitempty"`
		Display string            `json:"display,omitempty"`
	}
	storedInvocations := append([]StructuredInvocation(nil), env.Invocations...)
	if len(storedInvocations) == 0 && env.Invocation != nil {
		storedInvocations = []StructuredInvocation{*env.Invocation}
	}
	sort.SliceStable(storedInvocations, func(i, j int) bool {
		return storedInvocations[i].Offset < storedInvocations[j].Offset
	})
	invocations := make([]stableInvocation, 0, len(storedInvocations))
	for _, invocation := range storedInvocations {
		invocations = append(invocations, stableInvocation{
			Name: invocation.Name, Args: invocation.Args, Display: invocation.Display,
		})
	}
	stable := struct {
		DisplayText  string             `json:"displayText"`
		RawText      string             `json:"rawText"`
		SubmitText   string             `json:"submitText"`
		Invocations  []stableInvocation `json:"invocations,omitempty"`
		Format       string             `json:"format,omitempty"`
		Attachments  []string           `json:"attachments,omitempty"`
		ExplicitRefs []string           `json:"explicitRefs,omitempty"`
		Source       string             `json:"source,omitempty"`
		Extra        map[string]string  `json:"extra,omitempty"`
	}{
		DisplayText:  env.DisplayText,
		RawText:      env.RawText,
		SubmitText:   env.SubmitText,
		Invocations:  invocations,
		Format:       env.Format,
		Attachments:  env.Attachments,
		ExplicitRefs: env.ExplicitRefs,
		Source:       env.Source,
		Extra:        env.Extra,
	}
	data, err := json.Marshal(stable)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func normalizeEnvelope(env PromptEnvelope) PromptEnvelope {
	env.DisplayText = strings.TrimSpace(env.DisplayText)
	env.RawText = strings.TrimSpace(env.RawText)
	env.SubmitText = strings.TrimSpace(env.SubmitText)
	env.Format = strings.TrimSpace(env.Format)
	env.Idempotency = strings.TrimSpace(env.Idempotency)
	env.Source = strings.TrimSpace(env.Source)
	return env
}

func completeEnqueueEnvelope(env PromptEnvelope) PromptEnvelope {
	env = normalizeEnvelope(env)
	if env.Invocation != nil || len(env.Invocations) > 0 {
		return env
	}
	if env.SubmitText == "" {
		env.SubmitText = firstNonEmpty(env.RawText, env.DisplayText)
	}
	if env.DisplayText == "" {
		env.DisplayText = env.SubmitText
	}
	if env.RawText == "" {
		env.RawText = env.SubmitText
	}
	return env
}

func refSummaries(refs []RefSnapshot) []RefSummary {
	if len(refs) == 0 {
		return nil
	}
	out := make([]RefSummary, 0, len(refs))
	for _, r := range refs {
		out = append(out, RefSummary{
			Kind:    r.Kind,
			Path:    firstNonEmpty(r.DisplayPath, r.Path),
			Commit:  r.Commit,
			Bytes:   int64(len(r.Content)),
			Preview: PreviewText(string(r.Content), 40),
		})
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func agentBranchID(sessionPath string) string {
	base := filepath.Base(sessionPath)
	return strings.TrimSuffix(base, ".jsonl")
}

// RemoveDir deletes the entire inbox directory (clear/delete session).
func RemoveDir(sessionPath string) error {
	dir := store.SessionInboxDir(sessionPath)
	if dir == "" {
		return nil
	}
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// MigrateDir renames the inbox directory with a session path change.
func MigrateDir(oldPath, newPath string) error {
	oldDir := store.SessionInboxDir(oldPath)
	newDir := store.SessionInboxDir(newPath)
	if oldDir == "" || newDir == "" {
		return nil
	}
	if err := os.Rename(oldDir, newDir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
