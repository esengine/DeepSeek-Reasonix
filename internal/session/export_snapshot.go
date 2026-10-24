package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ExportSnapshot names an immutable display-history cut, not a provider workset.
// It is safe to retain while the source runtime continues accepting events.
type ExportSnapshot struct {
	ReadIncomplete    bool       `json:"readIncomplete,omitempty"`
	Ref               SessionRef `json:"session"`
	StorageGeneration string     `json:"storageGeneration"`
	SnapshotSequence  uint64     `json:"snapshotSequence"`
	AcceptedThrough   uint64     `json:"acceptedThrough"`
	DurableThrough    uint64     `json:"durableThrough"`
	CapturedAt        time.Time  `json:"capturedAt"`
	Title             string     `json:"title"`
}

func (q *Query) CaptureExportSnapshot(ctx context.Context, ref SessionRef) (ExportSnapshot, error) {
	out := ExportSnapshot{Ref: ref, CapturedAt: time.Now().UTC()}
	if err := ref.validate(q.hostID); err != nil {
		return out, err
	}
	generation := q.storageGeneration(ref.SessionID)
	if generation == "" {
		return out, ErrSessionNotFound
	}
	live := false
	if q.service != nil {
		if runtime, ok := q.service.Runtime(ref); ok {
			live = true
			out.AcceptedThrough = runtime.Session().EventSequence()
			receipt, err := runtime.Session().FlushThrough(ctx, out.AcceptedThrough)
			if err != nil {
				return out, err
			}
			out.DurableThrough = receipt.DurableSequence
		}
	}
	_, path, err := q.prepareHistoryIndex(ctx, ref)
	if err != nil {
		return out, err
	}
	handle, err := openHistoryProjection(ctx, path)
	if err != nil {
		return out, err
	}
	defer handle.DB.Close()
	metadata, err := readHistoryIndexMetadata(ctx, handle.DB)
	if err != nil {
		return out, err
	}
	if q.storageGeneration(ref.SessionID) != generation {
		return out, ErrStaleGeneration
	}
	if !live {
		out.AcceptedThrough = metadata.durableSequence
		out.DurableThrough = metadata.durableSequence
	}
	out.SnapshotSequence = out.AcceptedThrough
	out.StorageGeneration = generation
	if out.SnapshotSequence > metadata.durableSequence {
		return out, fmt.Errorf("session: history does not cover export watermark")
	}
	if metadata.generation != fmt.Sprintf("%s:%d", generation, metadata.viewSequence) {
		return out, ErrStaleGeneration
	}
	info, err := q.Stat(ctx, ref)
	if err != nil {
		return out, err
	}
	out.Title = info.Title
	return out, nil
}

// VisitExportMessages traverses the existing versioned display projection in
// ascending order, without accumulating the transcript or growing a UI window.
func (q *Query) VisitExportMessages(ctx context.Context, snapshot ExportSnapshot, visit func(PersistentMessage) error) error {
	return q.VisitExportMessagesForRef(ctx, snapshot.Ref, snapshot, visit)
}

// VisitExportMessagesForRef keeps the trusted storage identity separate from
// snapshot metadata received over a transport boundary.
func (q *Query) VisitExportMessagesForRef(ctx context.Context, ref SessionRef, snapshot ExportSnapshot, visit func(PersistentMessage) error) error {
	if err := q.ValidateExportSourceForRef(ref, snapshot); err != nil {
		return err
	}
	generation := q.storageGeneration(ref.SessionID)
	filesystem, path, err := q.prepareHistoryIndex(ctx, ref)
	if err != nil {
		return err
	}
	handle, err := openHistoryProjection(ctx, path)
	if err != nil {
		return err
	}
	defer handle.DB.Close()
	metadata, err := readHistoryIndexMetadata(ctx, handle.DB)
	if err != nil {
		return err
	}
	if q.storageGeneration(ref.SessionID) != generation || metadata.durableSequence < snapshot.SnapshotSequence {
		return ErrStaleGeneration
	}
	if metadata.generation != fmt.Sprintf("%s:%d", generation, metadata.viewSequence) {
		return ErrStaleGeneration
	}
	boundary := int64(1)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if q.storageGeneration(ref.SessionID) != generation {
			return ErrStaleGeneration
		}
		page, err := q.readHistoryWindowPage(ctx, handle.DB, filesystem, ref, metadata, snapshot.SnapshotSequence, boundary, historyWindowDirNewer, 100)
		if err != nil {
			return err
		}
		if err = q.attachHistoryWindowTurnStats(ctx, handle.DB, &page); err != nil {
			return err
		}
		if err = q.attachToolObservations(ctx, handle.DB, ref, &page); err != nil {
			return err
		}
		for _, message := range page.Messages {
			if message.ContentRef != nil {
				contentRef := *message.ContentRef
				body := make([]byte, 0)
				for offset := int64(0); offset < contentRef.Bytes; {
					chunk, err := q.ReadContent(ctx, ref, contentRef, offset, min(int64(1<<20), contentRef.Bytes-offset))
					if err != nil {
						return err
					}
					if len(chunk) == 0 {
						return fmt.Errorf("session: empty export content chunk")
					}
					body = append(body, chunk...)
					offset += int64(len(chunk))
				}
				if !json.Valid(body) {
					return fmt.Errorf("session: invalid export message")
				}
				message.Inline = body
				message.ContentRef = nil
			}
			if err := visit(message); err != nil {
				return err
			}
		}
		if !page.HasNewer {
			if q.storageGeneration(ref.SessionID) != generation {
				return ErrStaleGeneration
			}
			return nil
		}
		if len(page.Messages) == 0 {
			return fmt.Errorf("session: export cursor did not advance")
		}
		boundary = page.Messages[len(page.Messages)-1].Position + 1
	}
}

// CaptureDiagnosticSnapshot records an accepted boundary without requiring a
// successful persistence checkpoint or a readable display projection.
func (q *Query) CaptureDiagnosticSnapshot(ctx context.Context, ref SessionRef) (ExportSnapshot, error) {
	out := ExportSnapshot{Ref: ref, CapturedAt: time.Now().UTC()}
	if err := ref.validate(q.hostID); err != nil {
		return out, err
	}
	out.StorageGeneration = q.storageGeneration(ref.SessionID)
	if out.StorageGeneration == "" {
		return out, ErrSessionNotFound
	}
	if q.service != nil {
		if runtime, ok := q.service.Runtime(ref); ok {
			state := runtime.Session().StateSnapshot()
			out.AcceptedThrough, out.DurableThrough = state.EventSequence, state.DurableSequence
			out.SnapshotSequence = out.AcceptedThrough
			return out, nil
		}
	}
	err := q.StreamSession(ctx, ref, func(commit Commit) error {
		out.DurableThrough = commit.LastSequence()
		return nil
	})
	out.AcceptedThrough, out.SnapshotSequence = out.DurableThrough, out.DurableThrough
	out.ReadIncomplete = err != nil
	// A damaged tail must not prevent exporting its readable prefix and errors.
	if err != nil && ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, nil
}

// ValidateExportSource never rebinds a handle to replacement storage.
func (q *Query) ValidateExportSource(snapshot ExportSnapshot) error {
	return q.ValidateExportSourceForRef(snapshot.Ref, snapshot)
}

// ValidateExportSourceForRef validates transport metadata against a trusted
// canonical identity without using request-owned fields as path components.
func (q *Query) ValidateExportSourceForRef(ref SessionRef, snapshot ExportSnapshot) error {
	if err := ref.validate(q.hostID); err != nil {
		return err
	}
	if snapshot.Ref != ref {
		return ErrStaleGeneration
	}
	generation := q.storageGeneration(ref.SessionID)
	if generation == "" {
		return ErrSessionNotFound
	}
	if snapshot.StorageGeneration != generation && !strings.HasPrefix(snapshot.StorageGeneration, generation+":") {
		return ErrStaleGeneration
	}
	return nil
}

// StreamExportCommits visits a fixed raw prefix for cold diagnostics. Reopening
// that session in another tab cannot make traversal chase new commits forever.
func (q *Query) StreamExportCommits(ctx context.Context, snapshot ExportSnapshot, visit func(Commit) error) error {
	if err := q.ValidateExportSource(snapshot); err != nil {
		return err
	}
	reached := errors.New("export boundary reached")
	err := q.StreamSession(ctx, snapshot.Ref, func(commit Commit) error {
		if commit.LastSequence() > snapshot.SnapshotSequence {
			return reached
		}
		if err := visit(commit); err != nil {
			return err
		}
		if commit.LastSequence() == snapshot.SnapshotSequence {
			return reached
		}
		return nil
	})
	if errors.Is(err, reached) {
		err = nil
	}
	if err != nil {
		return err
	}
	return q.ValidateExportSource(snapshot)
}
