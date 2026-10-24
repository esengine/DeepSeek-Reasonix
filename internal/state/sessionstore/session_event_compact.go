package sessionstore

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

// compactSessionEventLog rewrites the log as a single replace event via an
// atomic tmp+fsync+rename, so readers observe either the old log or the
// compacted one and never a partial state. It also heals a damaged log by
// construction.
func compactSessionEventLog(sessionPath string, msgs []provider.Message, digest [sha256.Size]byte, baseRevision int64, reason string) error {
	path := store.SessionEventLog(sessionPath)
	if path == "" {
		return fmt.Errorf("empty session event log path")
	}
	rec := sessionEventRecord{
		Type:          sessionEventTypeReplace,
		Revision:      baseRevision + 1,
		BaseRevision:  baseRevision,
		Messages:      append([]provider.Message(nil), msgs...),
		ContentDigest: digestString(digest),
		WriterID:      SessionWriterID(),
		Reason:        reason,
		CreatedAt:     time.Now().UTC(),
	}
	var err error
	if rec.Messages, rec.ImageBlobs, err = externalizeSessionImages(sessionPath, rec.Messages); err != nil {
		return err
	}
	rec.SchemaVersion = compositionEventSchema(sessionPath, rec.Messages, rec.ImageBlobs)
	buf, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encode session event: %w", err)
	}
	buf = append(buf, '\n')
	buf, err = prependEnvironmentFence(sessionPath, buf)
	if err != nil {
		return err
	}
	if err := fileutil.AtomicWriteFileStrict(path, buf, 0o600); err != nil {
		return err
	}
	// The published log names every blob still needed; nothing else is.
	sweepSessionImageBlobs(sessionPath, rec.ImageBlobs)
	return nil
}
