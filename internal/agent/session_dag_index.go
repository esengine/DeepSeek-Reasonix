package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"time"

	"reasonix/internal/provider"

	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/store"
)

// SessionHeadIndex is the schema-2 shape of <id>.event-index.json: a snapshot
// of the heads so listings and the catalog never replay a log. It is trusted
// only while LogSize and LogGeneration still match the log.
type SessionHeadIndex struct {
	SchemaVersion int           `json:"schema_version"`
	LogSize       int64         `json:"log_size"`
	LogGeneration int64         `json:"log_generation"`
	SelectedHead  string        `json:"selected_head"`
	MessageCount  int           `json:"message_count"`
	ContentDigest string        `json:"content_digest"`
	WriterID      string        `json:"writer_id"`
	UpdatedAt     time.Time     `json:"updated_at"`
	Heads         []SessionHead `json:"heads"`
}

// Current reports whether the index still describes the log on disk.
func (idx *SessionHeadIndex) Current(sessionPath string) bool {
	if idx == nil {
		return false
	}
	info, err := os.Stat(store.SessionEventLog(sessionPath))
	if err != nil || info.IsDir() || info.Size() != idx.LogSize {
		return false
	}
	header, ok, err := readSessionDAGHeader(sessionPath)
	return err == nil && ok && header.generation == idx.LogGeneration
}

// ReadSessionHeadIndex loads the schema-2 index. A missing file or a schema-1
// index yields nil, nil so callers fall back to replay.
func ReadSessionHeadIndex(sessionPath string) (*SessionHeadIndex, error) {
	path := store.SessionEventIndex(sessionPath)
	if path == "" {
		return nil, nil
	}
	b, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var idx SessionHeadIndex
	if err := json.Unmarshal(b, &idx); err != nil {
		return nil, err
	}
	if idx.SchemaVersion != sessionDAGSchemaVersion {
		return nil, nil
	}
	if idx.Heads == nil {
		idx.Heads = []SessionHead{}
	}
	return &idx, nil
}

func writeSessionDAGIndex(ctx context.Context, sessionPath string, st *sessionDAGState) error {
	indexPath := store.SessionEventIndex(sessionPath)
	if indexPath == "" || st == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	selected := st.selectedHead()
	msgs, _ := st.materialize(selected)
	digest, err := digestSessionMessages(msgs)
	if err != nil {
		return err
	}
	idx := SessionHeadIndex{
		SchemaVersion: sessionDAGSchemaVersion,
		LogSize:       st.size,
		LogGeneration: st.generation,
		SelectedHead:  selected,
		MessageCount:  len(msgs),
		ContentDigest: digestString(digest),
		WriterID:      SessionWriterID(),
		UpdatedAt:     time.Now().UTC(),
		Heads:         st.headList(),
	}
	b, err := marshalJSONIndentContext(ctx, idx)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return atomicWriteFileContext(ctx, indexPath, ".session-event-index.*.tmp", "event-index", b, 0o600, false)
}

// refreshSessionEventIndexContext rewrites the event index in the schema the
// log actually uses: a schema-2 log gets its head index replayed, a schema-1
// log the listing index. A listing repair must never downgrade a head index.
func refreshSessionEventIndexContext(ctx context.Context, sessionPath string, msgs []provider.Message, digest [sha256.Size]byte, revision int64) error {
	probe, err := probeSessionEventLog(sessionPath)
	if err != nil {
		return err
	}
	if !probe.dag {
		return writeSessionEventIndexContext(ctx, sessionPath, msgs, digest, revision)
	}
	st, err := replaySessionDAG(ctx, store.SessionEventLog(sessionPath), defaultSessionReplayLimits)
	if err != nil {
		return err
	}
	return writeSessionDAGIndex(ctx, sessionPath, st)
}
