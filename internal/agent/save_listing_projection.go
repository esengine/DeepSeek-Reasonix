package agent

import (
	"crypto/sha256"
	"log/slog"

	"reasonix/internal/provider"
)

func (s *Session) markPersistedWithListing(path string, digest [sha256.Size]byte, version uint64, revision int64, rewriteVersion int, msgs []provider.Message) {
	s.markPersisted(path, digest, version, revision, rewriteVersion)
	persistSessionListingProjection(path, msgs)
}

func persistSessionListingProjection(path string, msgs []provider.Message) {
	preview, turns := SessionPreviewFromMessages(msgs)
	if err := UpdateBranchMeta(path, false, func(meta *BranchMeta) error {
		meta.Preview = preview
		meta.Turns = turns
		meta.SchemaVersion = BranchMetaCountsVersion
		return nil
	}); err != nil {
		// JSONL/event log already committed. Listing metadata is a repairable
		// projection and must never turn a successful transcript save into an
		// application error.
		slog.Warn("session: listing metadata update deferred", "path", path, "err", err)
	}
}
