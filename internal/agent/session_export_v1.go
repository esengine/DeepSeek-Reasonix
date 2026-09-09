package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/store"
)

// ExportSessionSchemaOne writes the selected head of a schema-2 session as a
// self-contained schema-1 session at dst (a .jsonl path): the checkpoint, a
// one-record event log, and a meta sidecar older binaries can open. It is
// the rollback exit for users who must run a release that predates schema 2.
func ExportSessionSchemaOne(src, dst string) error {
	src, dst = strings.TrimSpace(src), strings.TrimSpace(dst)
	if src == "" || dst == "" {
		return fmt.Errorf("export session: source and destination are required")
	}
	if !strings.HasSuffix(dst, ".jsonl") {
		return fmt.Errorf("export session: destination %s must end in .jsonl", dst)
	}
	if sessionArtifactExists(dst) {
		return fmt.Errorf("export session: destination %s already exists", dst)
	}
	res, err := loadSessionTranscript(context.Background(), src, defaultSessionReplayLimits, nil)
	if err != nil {
		return err
	}
	if !res.dag {
		return fmt.Errorf("export session: %s is not a schema-2 session", src)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	digest, err := digestSessionMessages(res.msgs)
	if err != nil {
		return err
	}
	if err := appendSessionReplaceEvent(dst, res.msgs, digest, 0, "export"); err != nil {
		return err
	}
	if err := writeSessionMessages(dst, res.msgs); err != nil {
		return err
	}
	meta := BranchMeta{ID: BranchID(dst), CreatedAt: time.Now().UTC(), Revision: 1, ContentDigest: digestString(digest), WriterID: SessionWriterID()}
	if srcMeta, ok, err := LoadBranchMeta(src); err == nil && ok {
		meta.Name, meta.Scope, meta.WorkspaceRoot = srcMeta.Name, srcMeta.Scope, srcMeta.WorkspaceRoot
		meta.TopicID, meta.TopicTitle, meta.CustomTitle, meta.Model = srcMeta.TopicID, srcMeta.TopicTitle, srcMeta.CustomTitle, srcMeta.Model
		meta.CreatedAt = srcMeta.CreatedAt
	}
	if err := saveBranchMeta(dst, meta, false); err != nil {
		return err
	}
	if err := writeSessionEventIndex(dst, res.msgs, digest, 1); err != nil {
		return err
	}
	_ = store.SessionEventLog(dst)
	return nil
}
