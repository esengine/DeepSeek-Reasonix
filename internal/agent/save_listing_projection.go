package agent

import (
	"crypto/sha256"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"reasonix/internal/provider"
)

func (s *Session) classifySnapshotWriteForCommit(path string, msgs []provider.Message, digest [sha256.Size]byte, version uint64, ownedRewrite bool, mode sessionSaveMode) (snapshotWriteDecision, error) {
	decision, err := s.classifySnapshotWrite(path, msgs, digest, version, ownedRewrite)
	if err != nil || decision.upToDate && mode != sessionSaveRewriteCompact && !decision.ledgerStale {
		return decision, err
	}
	// Invalidate before any transcript mutation or stale-ledger repair so an
	// interrupted commit cannot leave the previous listing self-certified.
	reservedRevision, err := invalidateSessionListingProjection(path)
	if err != nil {
		return decision, fmt.Errorf("invalidate session listing projection: %w", err)
	}
	decision.reservedRevision = reservedRevision
	return decision, nil
}

func (s *Session) markPersistedWithListing(path string, digest [sha256.Size]byte, version uint64, revision int64, rewriteVersion int, msgs []provider.Message) {
	// Pair the persisted-message view with the baseline: writer-bound saves
	// use it to classify append shapes without reloading the transcript.
	s.setPersistedBaseline(path, digest, version, revision, true, true, rewriteVersion, msgs)
	persistSessionListingProjection(path, msgs, revision, digestString(digest))
}

// invalidateSessionListingProjection makes cached counts untrusted before a
// transcript commit begins. If the transcript lands but its revision ledger
// does not, readers must decode/repair instead of certifying the previous
// generation's preview from an internally consistent but stale sidecar.
func invalidateSessionListingProjection(path string) (int64, error) {
	if !sessionArtifactsHaveContent(path) {
		// A brand-new session has no prior projection to reuse. It also needs to
		// retain the historical first committed revision of one.
		return 0, nil
	}
	var reservedRevision int64
	err := UpdateBranchMeta(path, false, func(meta *BranchMeta) error {
		// Reserve the next transcript generation so stale whole-sidecar writers
		// preserve this invalidation. The transcript commit finalizes the same
		// revision, keeping the WAL and content ledger in one generation.
		if meta.Revision == math.MaxInt64 {
			return fmt.Errorf("session revision exhausted")
		}
		meta.Revision++
		reservedRevision = meta.Revision
		meta.WriterID = SessionWriterID()
		meta.SchemaVersion = 0
		return nil
	})
	return reservedRevision, err
}

func persistSessionListingProjection(path string, msgs []provider.Message, revision int64, contentDigest string) {
	preview, turns := SessionPreviewFromMessages(msgs)
	if err := UpdateBranchMeta(path, false, func(meta *BranchMeta) error {
		// The transcript commit and this repairable projection are separate
		// critical sections. A newer writer may already have advanced the
		// sidecar, so only publish counts for the generation this save committed.
		contentDigest = strings.TrimSpace(contentDigest)
		if revision <= 0 || meta.Revision != revision || strings.TrimSpace(meta.ContentDigest) != contentDigest {
			return nil
		}
		meta.Preview = preview
		meta.Turns = turns
		meta.SchemaVersion = BranchMetaCountsVersion
		meta.ListingRevision = revision
		meta.ListingContentDigest = contentDigest
		return nil
	}); err != nil {
		// JSONL/event log already committed. Listing metadata is a repairable
		// projection and must never turn a successful transcript save into an
		// application error.
		slog.Warn("session: listing metadata update deferred", "path", path, "err", err)
	}
}
