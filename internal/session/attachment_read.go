package session

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"

	"reasonix/internal/attachment"
	"reasonix/internal/projectiondb"
	"reasonix/internal/sessioncontent"
)

const sessionAttachmentReadLimit = 1 << 20

// ReadSessionAttachment returns one bounded range of an original attachment.
// The caller supplies only the digest; size and integrity fields are looked up
// from this session's content graph and are never taken from the client.
func (q *Query) ReadSessionAttachment(ctx context.Context, ref SessionRef, digest string, offset, length int64) ([]byte, int64, error) {
	if q == nil {
		return nil, 0, errors.New("session: nil query")
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if err := ref.validate(q.hostID); err != nil {
		return nil, 0, err
	}
	digest, ok := canonicalContentDigest(digest)
	if !ok {
		return nil, 0, errors.New("session: attachment digest is invalid")
	}
	filesystem, ok := q.persistence.(*FilesystemPersistence)
	if !ok {
		return nil, 0, errors.New("session: content reads require filesystem persistence")
	}
	authorized, err := q.lookupAuthorizedAttachment(ctx, filesystem, ref, digest)
	if err != nil {
		return nil, 0, err
	}
	if offset < 0 || length < 0 || length > sessionAttachmentReadLimit || offset > authorized.Bytes {
		return nil, 0, errors.New("session: invalid or oversized attachment range")
	}
	if offset == authorized.Bytes {
		return []byte{}, authorized.Bytes, nil
	}
	if length == 0 || length > authorized.Bytes-offset {
		length = min(int64(sessionAttachmentReadLimit), authorized.Bytes-offset)
	}
	data, err := contentStoreForSessionDir(filepath.Join(filesystem.Root, ref.SessionID)).ReadRange(ctx, authorized, offset, length)
	if err != nil {
		return nil, 0, err
	}
	return data, authorized.Bytes, nil
}

func (q *Query) lookupAuthorizedAttachment(ctx context.Context, filesystem *FilesystemPersistence, ref SessionRef, digest string) (sessioncontent.Ref, error) {
	if found, ok, err := q.lookupHistoryAttachment(ctx, filesystem, ref, digest); err != nil {
		return sessioncontent.Ref{}, err
	} else if ok {
		return found, nil
	}
	if found, ok := q.lookupLiveAttachment(ctx, ref, digest); ok {
		return found, nil
	}
	sessionDir := filepath.Join(filesystem.Root, ref.SessionID)
	for _, extra := range collectInboxContentRefs(sessionDir) {
		if extra.Digest == digest {
			return withIntegrityBlock(extra), nil
		}
	}
	return sessioncontent.Ref{}, errors.New("session: attachment is not authorized for this session")
}

func (q *Query) lookupHistoryAttachment(ctx context.Context, filesystem *FilesystemPersistence, ref SessionRef, digest string) (sessioncontent.Ref, bool, error) {
	path := historyIndexPath(filesystem.Root, ref.SessionID)
	if err := ensureHistoryIndex(ctx, filesystem, ref.SessionID, path); err != nil {
		return sessioncontent.Ref{}, false, err
	}
	handle, err := projectiondb.Open(ctx, projectiondb.OpenOptions{Path: path, Migrations: historyMigrations, RequireDisk: true, MaxOpenConns: 1})
	if err != nil {
		return sessioncontent.Ref{}, false, err
	}
	defer handle.DB.Close()
	var bytes int64
	var indexDigest string
	err = handle.DB.QueryRowContext(ctx, `SELECT bytes, index_digest FROM content_refs WHERE digest=? LIMIT 1`, digest).Scan(&bytes, &indexDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return sessioncontent.Ref{}, false, nil
	}
	if err != nil {
		return sessioncontent.Ref{}, false, err
	}
	found := sessioncontent.Ref{Digest: digest, Bytes: bytes, IndexDigest: indexDigest}
	return withIntegrityBlock(found), true, nil
}

func (q *Query) lookupLiveAttachment(ctx context.Context, ref SessionRef, digest string) (sessioncontent.Ref, bool) {
	snapshot, err := q.Snapshot(ctx, ref)
	if err != nil {
		return sessioncontent.Ref{}, false
	}
	for _, message := range snapshot.Projection.Messages {
		for _, extra := range attachment.CollectContentRefs(message.ImageInputs) {
			if extra.Digest == digest {
				return withIntegrityBlock(extra), true
			}
		}
	}
	return sessioncontent.Ref{}, false
}

func canonicalContentDigest(digest string) (string, bool) {
	digest = strings.ToLower(strings.TrimSpace(digest))
	if len(digest) != 64 {
		return "", false
	}
	for _, c := range digest {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", false
		}
	}
	return digest, true
}

func withIntegrityBlock(ref sessioncontent.Ref) sessioncontent.Ref {
	if ref.IndexDigest != "" && ref.IntegrityBlock == 0 {
		ref.IntegrityBlock = sessioncontent.IntegrityBlockBytes
	}
	return ref
}
